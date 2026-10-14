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

// TestTaskHTTPLifecycle_RequestContextDoesNotCancelTask verifies that
// task goroutines are NOT cancelled when the HTTP response is written.
func TestTaskHTTPLifecycle_RequestContextDoesNotCancelTask(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	handlerCompleted := make(chan struct{})
	handlerStarted := make(chan struct{})

	tool := mcp.NewTool("blocking_task",
		mcp.WithDescription("A task that completes after a short delay"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)
	srv.AddTaskTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
		close(handlerStarted)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
			close(handlerCompleted)
			r := mcp.NewCreateTaskResult(mcp.Task{})
			return &r, nil
		}
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

	// Wait until the goroutine has started before asserting it completes.
	// Without this barrier the 200ms timer may not have begun yet.
	<-handlerStarted

	// HTTP response has been sent; handler must still complete.
	select {
	case <-handlerCompleted:
		// Correct: task outlived the HTTP request.
	case <-time.After(2 * time.Second):
		t.Fatal("task handler was cancelled after HTTP response — " +
			"executeTaskTool should use s.serverCtx, not the per-request context")
	}
}

// TestTaskHTTPLifecycle_CancelViaTasksCancel verifies that tasks/cancel works
// correctly over Streamable HTTP regardless of the request-context limitation.
// tasks/cancel operates which is set by the task goroutine and is independent
// of the HTTP request context.
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

// TestTaskHTTPLifecycle_ContextNotCancelledOnAllTransports verifies that task
// goroutines are not cancelled when the request context is cancelled.
// Cancelling the caller's context after HandleMessage returns must not affect
// the running goroutine.
func TestTaskHTTPLifecycle_ContextNotCancelledOnAllTransports(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	handlerCompleted := make(chan struct{})

	tool := mcp.NewTool("blocking_task_inprocess",
		mcp.WithDescription("Completes after a short delay; must not be cancelled by request ctx"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)
	srv.AddTaskTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
			close(handlerCompleted)
			r := mcp.NewCreateTaskResult(mcp.Task{})
			return &r, nil
		}
	})

	// Use a cancellable context. We will cancel it right after HandleMessage
	// returns to simulate the per-request context being torn down.
	reqCtx, cancelReq := context.WithCancel(t.Context())
	notifyChan := make(chan mcp.JSONRPCNotification, 10)
	session := fakeSess{sessionID: "inprocess-session", notifyChan: notifyChan}
	require.NoError(t, srv.RegisterSession(reqCtx, session))
	sessionCtx := srv.WithContext(reqCtx, session)

	msg, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": "blocking_task_inprocess", "task": map[string]any{}},
	})
	srv.HandleMessage(sessionCtx, msg)

	// Cancel the request context immediately.
	cancelReq()

	select {
	case <-handlerCompleted:
		// Correct: goroutine is rooted on s.serverCtx and survives request cancellation.
	case <-time.After(2 * time.Second):
		t.Fatal("task handler was cancelled after request ctx was cancelled — " +
			"executeTaskTool must use s.serverCtx, not the per-request context")
	}
}
