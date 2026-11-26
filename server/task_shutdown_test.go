package server

import (
	"bufio"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

const workTaskCall = `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"work","task":{}}}`

type taskCancelledCall struct {
	ctxErr  error
	metrics TaskMetrics
}

// taskShutdownProbe reports on a "work" task whose handler blocks until its
// context ends.
type taskShutdownProbe struct {
	started   chan struct{}
	stopped   chan error
	cancelled chan taskCancelledCall
}

func newTaskShutdownServer(taskTool bool) (*MCPServer, *taskShutdownProbe) {
	probe := &taskShutdownProbe{
		started:   make(chan struct{}, 1),
		stopped:   make(chan error, 1),
		cancelled: make(chan taskCancelledCall, 2),
	}
	hooks := &TaskHooks{}
	hooks.AddOnTaskCancelled(func(ctx context.Context, metrics TaskMetrics) {
		probe.cancelled <- taskCancelledCall{ctxErr: ctx.Err(), metrics: metrics}
	})
	s := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true), WithTaskHooks(hooks))
	addTaskWorkTool(s, taskTool, func(ctx context.Context) (string, error) {
		probe.started <- struct{}{}
		<-ctx.Done()
		probe.stopped <- ctx.Err()
		return "", ctx.Err()
	})
	return s, probe
}

// postModernWorkTask sends a modern tools/call of "work" to run as a task. A
// modern client declares the tasks extension for that.
func postModernWorkTask(t *testing.T, url string) *http.Response {
	t.Helper()
	meta := modernMeta()
	meta[mcp.MetaKeyClientCapabilities] = map[string]any{
		"extensions": map[string]any{"io.modelcontextprotocol/tasks": map[string]any{}},
	}
	return postModernWithMeta(t, url, mcp.MethodToolsCall, map[string]any{"name": "work", "arguments": map[string]any{}, "task": map[string]any{}}, meta)
}

func (p *taskShutdownProbe) waitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-p.started:
	case <-time.After(2 * time.Second):
		t.Fatal("task handler did not start")
	}
}

// requireCancelledByShutdown checks that the task's context was cancelled,
// that the task ended cancelled, and that the cancel hook ran once with a
// context that was still live.
func (p *taskShutdownProbe) requireCancelledByShutdown(t *testing.T, s *MCPServer) {
	t.Helper()
	select {
	case err := <-p.stopped:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not cancel the task's context")
	}
	select {
	case call := <-p.cancelled:
		assert.NoError(t, call.ctxErr, "cancel hook must not get a cancelled context")
		assert.Equal(t, mcp.TaskStatusCancelled, call.metrics.Status)
		assert.Equal(t, "Task cancelled by server shutdown", call.metrics.StatusMessage)
	case <-time.After(2 * time.Second):
		t.Fatal("cancel hook did not run")
	}

	s.tasksMu.RLock()
	defer s.tasksMu.RUnlock()
	require.Len(t, s.tasks, 1)
	for _, entry := range s.tasks {
		assert.Equal(t, mcp.TaskStatusCancelled, entry.task.Status)
	}
	assert.Zero(t, s.activeTasks)
	assert.Empty(t, p.cancelled, "cancel hook must run once")
}

func TestStreamableHTTPShutdownCancelsTasks(t *testing.T) {
	tests := []struct {
		name     string
		taskTool bool
		modern   bool
	}{
		{name: "task tool", taskTool: true},
		{name: "regular tool", taskTool: false},
		{name: "task tool, sessionless modern request", taskTool: true, modern: true},
		{name: "regular tool, sessionless modern request", taskTool: false, modern: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, probe := newTaskShutdownServer(tt.taskTool)
			ts := httptest.NewUnstartedServer(nil)
			httpServer := NewStreamableHTTPServer(s, WithStreamableHTTPServer(ts.Config))
			ts.Config.Handler = httpServer
			ts.Start()
			t.Cleanup(ts.Close)

			var resp *http.Response
			if tt.modern {
				resp = postModernWorkTask(t, ts.URL)
			} else {
				var err error
				resp, err = postSessionJSON(ts.URL, initializeSession(t, ts.URL), map[string]any{
					"jsonrpc": "2.0", "id": 2, "method": "tools/call",
					"params": map[string]any{"name": "work", "task": map[string]any{}},
				})
				require.NoError(t, err)
			}
			_ = resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)
			probe.waitStarted(t)

			require.NoError(t, httpServer.Shutdown(t.Context()))
			probe.requireCancelledByShutdown(t, s)
		})
	}
}

// TestStreamableHTTPTaskStartedAfterShutdownIsCancelled covers a request that
// starts its task only after Shutdown has run, as one still in flight can.
func TestStreamableHTTPTaskStartedAfterShutdownIsCancelled(t *testing.T) {
	s, probe := newTaskShutdownServer(true)
	httpServer := NewStreamableHTTPServer(s)
	ts := httptest.NewServer(httpServer)
	t.Cleanup(ts.Close)
	require.NoError(t, httpServer.Shutdown(t.Context()))

	resp := postModernWorkTask(t, ts.URL)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	probe.waitStarted(t)
	probe.requireCancelledByShutdown(t, s)
}

func TestSSEShutdownCancelsTasks(t *testing.T) {
	for _, taskTool := range []bool{true, false} {
		name := "regular tool"
		if taskTool {
			name = "task tool"
		}
		t.Run(name, func(t *testing.T) {
			s, probe := newTaskShutdownServer(taskTool)
			ts := httptest.NewUnstartedServer(nil)
			sseServer := NewSSEServer(s, WithHTTPServer(ts.Config))
			ts.Config.Handler = sseServer
			ts.Start()
			t.Cleanup(ts.Close)
			WithBaseURL(ts.URL)(sseServer)

			get, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+"/sse", nil)
			require.NoError(t, err)
			stream, err := ts.Client().Do(get)
			require.NoError(t, err)
			defer stream.Body.Close()
			endpoint := readSSEEndpoint(t, bufio.NewReader(stream.Body))

			post, err := http.NewRequestWithContext(t.Context(), http.MethodPost, endpoint, strings.NewReader(workTaskCall))
			require.NoError(t, err)
			post.Header.Set("Content-Type", "application/json")
			resp, err := ts.Client().Do(post)
			require.NoError(t, err)
			_ = resp.Body.Close()
			require.Equal(t, http.StatusAccepted, resp.StatusCode)
			probe.waitStarted(t)

			require.NoError(t, sseServer.Shutdown(t.Context()))
			probe.requireCancelledByShutdown(t, s)
		})
	}
}

func TestStdioListenEndCancelsTasks(t *testing.T) {
	for _, taskTool := range []bool{true, false} {
		name := "regular tool"
		if taskTool {
			name = "task tool"
		}
		t.Run(name, func(t *testing.T) {
			s, probe := newTaskShutdownServer(taskTool)
			stdioServer := NewStdioServer(s)
			stdioServer.SetErrorLogger(log.New(io.Discard, "", 0))

			reader, input := io.Pipe()
			t.Cleanup(func() { _ = reader.Close() })
			listenDone := make(chan error, 1)
			go func() { listenDone <- stdioServer.Listen(t.Context(), reader, io.Discard) }()

			_, err := io.WriteString(input, workTaskCall+"\n")
			require.NoError(t, err)
			probe.waitStarted(t)

			// The client going away ends Listen.
			require.NoError(t, input.Close())
			select {
			case err := <-listenDone:
				require.NoError(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("Listen did not return after EOF")
			}
			probe.requireCancelledByShutdown(t, s)
		})
	}
}

// TestTransportShutdownLeavesOtherTransportsTasks serves one MCPServer over
// Streamable HTTP and stdio, as in the multi-transport example in the docs,
// and checks that the stdio listener ending does not cancel a task started
// over HTTP.
func TestTransportShutdownLeavesOtherTransportsTasks(t *testing.T) {
	s, probe := newTaskShutdownServer(true)
	httpServer := NewStreamableHTTPServer(s)
	ts := httptest.NewServer(httpServer)
	t.Cleanup(ts.Close)

	resp := postModernWorkTask(t, ts.URL)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	probe.waitStarted(t)

	stdioServer := NewStdioServer(s)
	stdioServer.SetErrorLogger(log.New(io.Discard, "", 0))
	require.NoError(t, stdioServer.Listen(t.Context(), strings.NewReader(""), io.Discard))

	select {
	case err := <-probe.stopped:
		t.Fatalf("stdio listener ending cancelled a task started over HTTP: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	require.NoError(t, httpServer.Shutdown(t.Context()))
	probe.requireCancelledByShutdown(t, s)
}
