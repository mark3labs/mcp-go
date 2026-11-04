package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestTaskResultAfterCancellationReturnsError(t *testing.T) {
	srv := NewMCPServer("test", "1", WithTaskCapabilities(true, true, true))
	started := make(chan struct{})
	returned := make(chan struct{})
	srv.AddTool(mcp.NewTool("work", mcp.WithTaskSupport(mcp.TaskSupportOptional)),
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			close(started)
			defer close(returned)
			<-ctx.Done()
			return nil, ctx.Err()
		})
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	created, reqErr := srv.handleToolCall(ctx, 1, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "work", Task: &mcp.TaskParams{}},
	})
	require.Nil(t, reqErr)
	task, ok := created.(*mcp.CreateTaskResult)
	require.True(t, ok, "task creation result: %T", created)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("task handler did not start")
	}
	cancelRequest, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tasks/cancel", "params": map[string]any{"taskId": task.Task.TaskId},
	})
	require.NoError(t, err)
	cancelled := srv.HandleMessage(ctx, cancelRequest)
	cancelEncoded, err := json.Marshal(cancelled)
	require.NoError(t, err)
	t.Logf("tasks/cancel: %s", cancelEncoded)
	cancelResponse, ok := cancelled.(mcp.JSONRPCResponse)
	require.True(t, ok, "tasks/cancel response: %T", cancelled)
	cancelResult, ok := cancelResponse.Result.(mcp.CancelTaskResult)
	require.True(t, ok)
	require.Equal(t, mcp.TaskStatusCancelled, cancelResult.Status)
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("task handler did not observe cancellation")
	}
	resultRequest, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tasks/result", "params": map[string]any{"taskId": task.Task.TaskId},
	})
	require.NoError(t, err)
	result := srv.HandleMessage(ctx, resultRequest)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	t.Logf("tasks/result after successful cancellation: %s", encoded)
	rpcError, ok := result.(mcp.JSONRPCError)
	require.True(t, ok, "cancelled execution must not return a successful empty result")
	require.Equal(t, mcp.REQUEST_INTERRUPTED, rpcError.Error.Code)
}

func TestTaskCancellationKeepsResultError(t *testing.T) {
	for _, waiting := range []bool{false, true} {
		t.Run(fmt.Sprint("waiting=", waiting), func(t *testing.T) {
			srv := NewMCPServer("test", "1", WithTaskCapabilities(true, true, true), WithMaxConcurrentTasks(1))
			entry, err := srv.createTask(t.Context(), "cancelled", "work", nil, nil)
			require.NoError(t, err)
			ready := make(chan struct{})
			ctx := &taskResultWaitContext{Context: t.Context(), ready: ready}
			results := make(chan *requestError, 1)
			request := mcp.TaskResultRequest{Params: mcp.TaskResultParams{TaskId: "cancelled"}}
			if waiting {
				go func() { _, e := srv.handleTaskResult(ctx, 1, request); results <- e }()
				select {
				case <-ready:
				case <-time.After(5 * time.Second):
					t.Fatal("result request did not wait")
				}
			}
			require.NoError(t, srv.cancelTask(t.Context(), "cancelled"))
			if waiting {
				select {
				case e := <-results:
					require.NotNil(t, e)
					require.Equal(t, mcp.REQUEST_INTERRUPTED, e.code)
				case <-time.After(5 * time.Second):
					t.Fatal("result request did not wake after cancellation")
				}
			}
			// A late successful handler must not replace an accepted cancellation.
			srv.completeTask(entry, mcp.NewToolResultText("late success"), nil)
			for range 2 {
				result, e := srv.handleTaskResult(t.Context(), 2, request)
				require.Nil(t, result)
				require.NotNil(t, e)
				require.Equal(t, mcp.REQUEST_INTERRUPTED, e.code)
				require.ErrorIs(t, e.err, context.Canceled)
			}
			require.Equal(t, mcp.TaskStatusCancelled, entry.task.Status)
			require.Zero(t, srv.activeTasks)
			next, err := srv.createTask(t.Context(), "next", "work", nil, nil)
			require.NoError(t, err)
			srv.completeTask(next, mcp.NewToolResultText("normal success"), nil)
			result, e := srv.handleTaskResult(t.Context(), 3, mcp.TaskResultRequest{Params: mcp.TaskResultParams{TaskId: "next"}})
			require.Nil(t, e)
			require.Equal(t, "normal success", result.Content[0].(mcp.TextContent).Text)
		})
	}
}

type taskResultWaitContext struct {
	context.Context
	ready chan struct{}
	once  sync.Once
}

func (c *taskResultWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.ready) })
	return c.Context.Done()
}

func TestCancelledTaskResultHTTPEnvelope(t *testing.T) {
	srv := NewMCPServer("test", "1", WithTaskCapabilities(true, true, true))
	_, err := srv.createTask(t.Context(), "cancelled", "work", nil, nil)
	require.NoError(t, err)
	require.NoError(t, srv.cancelTask(t.Context(), "cancelled"))
	httpServer := httptest.NewServer(NewStreamableHTTPServer(srv, WithStateLess(true)))
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, httpServer.URL, bytes.NewBufferString(`{"jsonrpc":"2.0","id":7,"method":"tasks/result","params":{"taskId":"cancelled"}}`))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Mcp-Protocol-Version", "2025-11-25")
	response, err := httpServer.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var envelope map[string]json.RawMessage
	require.NoError(t, json.NewDecoder(response.Body).Decode(&envelope))
	require.NotContains(t, envelope, "result")
	var rpcError struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(envelope["error"], &rpcError))
	require.Equal(t, mcp.REQUEST_INTERRUPTED, rpcError.Code)
}

func TestTaskResultErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code int
	}{
		{"handler cancellation", context.Canceled, mcp.REQUEST_INTERRUPTED},
		{"handler deadline", context.DeadlineExceeded, mcp.REQUEST_INTERRUPTED},
		{"ordinary failure", errors.New("handler failed"), mcp.INTERNAL_ERROR},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := NewMCPServer("test", "1", WithTaskCapabilities(true, true, true))
			entry, err := srv.createTask(t.Context(), "task", "work", nil, nil)
			require.NoError(t, err)
			srv.executeTaskTool(t.Context(), entry, ServerTaskTool{Handler: func(context.Context, mcp.CallToolRequest) (*mcp.CreateTaskResult, error) { return nil, tc.err }}, mcp.CallToolRequest{})
			result, e := srv.handleTaskResult(t.Context(), 1, mcp.TaskResultRequest{Params: mcp.TaskResultParams{TaskId: "task"}})
			require.Nil(t, result)
			require.NotNil(t, e)
			require.Equal(t, tc.code, e.code)
			if tc.code == mcp.INTERNAL_ERROR {
				require.ErrorIs(t, e.err, tc.err)
			}
		})
	}
}
