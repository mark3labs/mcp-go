package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseSSEOrJSON extracts the JSON payload from either a plain JSON response
// or an SSE stream with a single "data: {...}" line.
func parseSSEOrJSON(body []byte) []byte {
	for _, line := range strings.Split(string(body), "\n") {
		if after, ok := strings.CutPrefix(line, "data: "); ok {
			after = strings.TrimSpace(after)
			if len(after) > 0 {
				return []byte(after)
			}
		}
	}
	return body
}

// Streamable HTTP tests can obtain a session ID via the initialize handshake.
var legacyInitRequest = map[string]any{
	"jsonrpc": "2.0",
	"id":      1,
	"method":  "initialize",
	"params": map[string]any{
		"protocolVersion": mcp.LATEST_LEGACY_PROTOCOL_VERSION,
		"clientInfo":      map[string]any{"name": "test-client", "version": "1.0.0"},
	},
}

// initStatefulSession performs the initialize handshake and returns the session ID.
func initStatefulSession(t *testing.T, serverURL string) string {
	t.Helper()
	resp, err := postJSON(serverURL, legacyInitRequest)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
	sessionID := resp.Header.Get(HeaderKeySessionID)
	require.NotEmpty(t, sessionID, "expected session ID from initialize")
	return sessionID
}

// TestTaskHTTPLifecycle_RequestContextCancelledAfterResponse verifies that the
// task goroutine's context is cancelled by the time the HTTP response is
// delivered on Streamable HTTP, demonstrating the known limitation.
func TestTaskHTTPLifecycle_RequestContextCancelledAfterResponse(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	ctxCancelledCh := make(chan struct{})
	handlerStarted := make(chan struct{})

	tool := mcp.NewTool("blocking_task",
		mcp.WithDescription("A task that waits for context cancellation"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)
	srv.AddTaskTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
		close(handlerStarted)
		<-ctx.Done()
		close(ctxCancelledCh)
		return nil, ctx.Err()
	})

	ts := NewTestStreamableHTTPServer(srv, WithStateful(true))
	defer ts.Close()

	sessionID := initStatefulSession(t, ts.URL)

	callReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params":  map[string]any{"name": "blocking_task", "task": map[string]any{}},
	}
	callResp, err := postSessionJSON(ts.URL, sessionID, callReq)
	require.NoError(t, err)
	defer callResp.Body.Close()
	require.Equal(t, http.StatusOK, callResp.StatusCode)

	var result struct {
		Result struct {
			Task struct {
				TaskId string `json:"taskId"`
				Status string `json:"status"`
			} `json:"task"`
		} `json:"result"`
	}
	require.NoError(t, json.NewDecoder(callResp.Body).Decode(&result))
	taskID := result.Result.Task.TaskId
	require.NotEmpty(t, taskID, "expected a task ID in the response")
	assert.Equal(t, "working", result.Result.Task.Status)

	select {
	case <-ctxCancelledCh:
		// Expected on HTTP: request context cancelled after response.
	case <-time.After(2 * time.Second):
		t.Fatal("task handler context was not cancelled after HTTP response — " +
			"this may indicate the context lifecycle behaviour changed")
	}
}

// TestTaskHTTPLifecycle_CancelViaTasksCancel verifies that tasks/cancel works
// correctly over Streamable HTTP regardless of the request-context limitation.
// tasks/cancel operates via entry.cancelFunc which is set by the task goroutine
// and is independent of the HTTP request context.
func TestTaskHTTPLifecycle_CancelViaTasksCancel(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	handlerRunning := make(chan struct{})

	tool := mcp.NewTool("cancellable_task",
		mcp.WithDescription("A task that can be cancelled via tasks/cancel"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)
	srv.AddTaskTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
		close(handlerRunning)
		<-ctx.Done()
		return nil, ctx.Err()
	})

	ts := NewTestStreamableHTTPServer(srv, WithStateful(true))
	defer ts.Close()

	sessionID := initStatefulSession(t, ts.URL)

	callReq := map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": "cancellable_task", "task": map[string]any{}},
	}
	callResp, err := postSessionJSON(ts.URL, sessionID, callReq)
	require.NoError(t, err)
	var callResult struct {
		Result struct {
			Task struct {
				TaskId string `json:"taskId"`
			} `json:"task"`
		} `json:"result"`
	}
	require.NoError(t, json.NewDecoder(callResp.Body).Decode(&callResult))
	callResp.Body.Close()
	taskID := callResult.Result.Task.TaskId
	require.NotEmpty(t, taskID)

	select {
	case <-handlerRunning:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start")
	}

	// Cancel via tasks/cancel.
	cancelResp, err := postSessionJSON(ts.URL, sessionID, map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tasks/cancel",
		"params": map[string]any{"taskId": taskID},
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, cancelResp.StatusCode)
	cancelResp.Body.Close()

	// Poll tasks/get until the task reaches a terminal state.
	// Verify the task reaches a terminal state.
	var lastStatus string
	require.Eventually(t, func() bool {
		getResp, err := postSessionJSON(ts.URL, sessionID, map[string]any{
			"jsonrpc": "2.0", "id": 4, "method": "tasks/get",
			"params": map[string]any{"taskId": taskID},
		})
		if err != nil {
			return false
		}
		body, _ := io.ReadAll(getResp.Body)
		getResp.Body.Close()
		var gr struct {
			Result struct {
				Status string `json:"status"`
			} `json:"result"`
		}
		if err := json.Unmarshal(parseSSEOrJSON(body), &gr); err != nil {
			return false
		}
		lastStatus = gr.Result.Status
		s := gr.Result.Status
		return s == "cancelled" || s == "failed" || s == "completed"
	}, 3*time.Second, 100*time.Millisecond, "task status: %s", lastStatus)
}

// TestTaskHTTPLifecycle_ContextCancelledOnAllTransports verifies that the
// request-context cancellation limitation applies to ALL transports, not just
// Streamable HTTP.
func TestTaskHTTPLifecycle_ContextCancelledOnAllTransports(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	ctxCancelledCh := make(chan struct{})
	handlerStarted := make(chan struct{})

	tool := mcp.NewTool("blocking_task_inprocess",
		mcp.WithDescription("Blocks on ctx.Done to detect premature cancellation"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)
	srv.AddTaskTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
		close(handlerStarted)
		<-ctx.Done()
		close(ctxCancelledCh)
		return nil, ctx.Err()
	})

	// Drive via HandleMessage, the same code path used by every real transport.
	ctx := t.Context()
	notifyChan := make(chan mcp.JSONRPCNotification, 10)
	session := fakeSess{sessionID: "inprocess-session", notifyChan: notifyChan}
	require.NoError(t, srv.RegisterSession(ctx, session))
	sessionCtx := srv.WithContext(ctx, session)

	msg, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": "blocking_task_inprocess", "task": map[string]any{}},
	})
	srv.HandleMessage(sessionCtx, msg)

	// HandleMessage has returned, the defer cancel inside it has fired.
	// The task goroutine must observe ctx.Done() almost immediately.
	select {
	case <-ctxCancelledCh:
		// Correct: context is cancelled on in-process transport too.
	case <-time.After(2 * time.Second):
		t.Fatal("task handler context was not cancelled after HandleMessage returned — " +
			"the per-request context cancellation applies to all transports")
	}
}
