package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
)

func newTestServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	dispatched := &atomic.Int32{}
	hooks := &server.Hooks{}
	hooks.AddBeforeGetTask(func(context.Context, any, *mcp.GetTaskRequest) { dispatched.Add(1) })
	hooks.AddBeforeUpdateTask(func(context.Context, any, *mcp.UpdateTaskRequest) { dispatched.Add(1) })
	hooks.AddBeforeCancelTask(func(context.Context, any, *mcp.CancelTaskRequest) { dispatched.Add(1) })
	hooks.AddBeforeListTasks(func(context.Context, any, *mcp.ListTasksRequest) { dispatched.Add(1) })
	hooks.AddBeforeTaskResult(func(context.Context, any, *mcp.TaskResultRequest) { dispatched.Add(1) })
	srv := httptest.NewServer(newTaskServer(map[string]principal{
		"alice-token":    {user: "alice"},
		"bob-token":      {user: "bob"},
		"operator-token": {user: "operator", canList: true},
	}, server.WithHooks(hooks)))
	t.Cleanup(srv.Close)
	return srv, dispatched
}

func postTaskRequest(t *testing.T, srv *httptest.Server, token string, modern bool, method mcp.MCPMethod, params map[string]any) (int, []byte) {
	t.Helper()
	if modern {
		params["_meta"] = map[string]any{
			mcp.MetaKeyProtocolVersion: mcp.ProtocolVersion20260728,
			mcp.MetaKeyClientCapabilities: map[string]any{
				"extensions": map[string]any{mcp.ExtensionTasks: map[string]any{}},
			},
		}
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	require.NoError(t, err)
	// Cleanup cancels owned tasks after the test context has ended.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if modern {
		req.Header.Set(mcp.HeaderProtocolVersion, mcp.ProtocolVersion20260728)
		req.Header.Set(mcp.HeaderMethod, string(method))
		rawParams, err := json.Marshal(params)
		require.NoError(t, err)
		if name, ok := mcp.ExtractHeaderName(method, rawParams); ok {
			encoded, valid := mcp.EncodeHeaderValue(name)
			require.True(t, valid)
			req.Header.Set(mcp.HeaderName, encoded)
		}
	}
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, responseBody
}

func taskResult(t *testing.T, body []byte) json.RawMessage {
	t.Helper()
	var response struct {
		Result json.RawMessage          `json:"result"`
		Error  *mcp.JSONRPCErrorDetails `json:"error"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	require.Nil(t, response.Error, string(body))
	require.NotEmpty(t, response.Result)
	return response.Result
}

func createOwnedTask(t *testing.T, srv *httptest.Server, token string, modern bool, tool string) string {
	t.Helper()
	params := map[string]any{"name": tool}
	if !modern {
		params["task"] = map[string]any{}
	}
	status, body := postTaskRequest(t, srv, token, modern, mcp.MethodToolsCall, params)
	require.Equal(t, http.StatusOK, status, string(body))
	var created mcp.CreateTaskResult
	require.NoError(t, json.Unmarshal(taskResult(t, body), &created))
	require.NotEmpty(t, created.Task.TaskId)
	t.Cleanup(func() {
		postTaskRequest(t, srv, token, modern, mcp.MethodTasksCancel, map[string]any{"taskId": created.Task.TaskId})
	})
	return created.Task.TaskId
}

func waitForTaskStatus(t *testing.T, srv *httptest.Server, token string, modern bool, taskID string, expected mcp.TaskStatus) {
	t.Helper()
	require.Eventually(t, func() bool {
		status, body := postTaskRequest(t, srv, token, modern, mcp.MethodTasksGet, map[string]any{"taskId": taskID})
		require.Equal(t, http.StatusOK, status, string(body))
		var task mcp.GetTaskResult
		require.NoError(t, json.Unmarshal(taskResult(t, body), &task))
		return task.Status == expected
	}, 5*time.Second, 10*time.Millisecond)
}

// TestTaskOwnership rejects foreign access before SDK hooks or handlers execute.
func TestTaskOwnership(t *testing.T) {
	tests := []struct {
		name   string
		method mcp.MCPMethod
		modern bool
		tool   string
	}{
		{name: "modern get", method: mcp.MethodTasksGet, modern: true, tool: "approve"},
		{name: "modern update", method: mcp.MethodTasksUpdate, modern: true, tool: "approve"},
		{name: "modern cancel", method: mcp.MethodTasksCancel, modern: true, tool: "wait"},
		{name: "legacy get", method: mcp.MethodTasksGet, tool: "approve"},
		{name: "legacy result", method: mcp.MethodTasksResult, tool: "approve"},
		{name: "legacy cancel", method: mcp.MethodTasksCancel, tool: "wait"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, dispatched := newTestServer(t)
			taskID := createOwnedTask(t, srv, "alice-token", tt.modern, tt.tool)
			expected := mcp.TaskStatusCompleted
			if tt.tool == "wait" {
				expected = mcp.TaskStatusWorking
			} else if tt.modern {
				expected = mcp.TaskStatusInputRequired
			}
			waitForTaskStatus(t, srv, "alice-token", tt.modern, taskID, expected)
			params := map[string]any{"taskId": taskID}
			if tt.method == mcp.MethodTasksUpdate {
				params["inputResponses"] = mcp.InputResponses{"confirm": mcp.NewElicitationInputResponse(mcp.ElicitationResult{
					ElicitationResponse: mcp.ElicitationResponse{Action: mcp.ElicitationResponseActionAccept},
				})}
			}
			for _, denial := range []struct {
				token  string
				status int
			}{
				{token: "", status: http.StatusUnauthorized},
				{token: "invalid", status: http.StatusUnauthorized},
				{token: "bob-token", status: http.StatusForbidden},
				{token: "operator-token", status: http.StatusForbidden},
			} {
				dispatched.Store(0)
				status, body := postTaskRequest(t, srv, denial.token, tt.modern, tt.method, params)
				require.Equal(t, denial.status, status, string(body))
				require.NotContains(t, string(body), taskID)
				require.Zero(t, dispatched.Load(), "denial must precede SDK dispatch")
			}
			waitForTaskStatus(t, srv, "alice-token", tt.modern, taskID, expected)
			dispatched.Store(0)
			status, body := postTaskRequest(t, srv, "alice-token", tt.modern, tt.method, params)
			require.Equal(t, http.StatusOK, status, string(body))
			result := taskResult(t, body)
			require.Equal(t, int32(1), dispatched.Load())
			switch tt.method {
			case mcp.MethodTasksUpdate:
				waitForTaskStatus(t, srv, "alice-token", tt.modern, taskID, mcp.TaskStatusCompleted)
			case mcp.MethodTasksCancel:
				waitForTaskStatus(t, srv, "alice-token", tt.modern, taskID, mcp.TaskStatusCancelled)
			case mcp.MethodTasksResult:
				var toolResult mcp.CallToolResult
				require.NoError(t, json.Unmarshal(result, &toolResult))
				require.Len(t, toolResult.Content, 1)
			}
		})
	}
}

// TestLegacyTaskListing requires explicit listing permission on each request.
func TestLegacyTaskListing(t *testing.T) {
	srv, dispatched := newTestServer(t)
	aliceTask := createOwnedTask(t, srv, "alice-token", false, "approve")
	bobTask := createOwnedTask(t, srv, "bob-token", false, "approve")
	for _, denial := range []struct {
		token  string
		status int
	}{
		{token: "", status: http.StatusUnauthorized},
		{token: "invalid", status: http.StatusUnauthorized},
		{token: "alice-token", status: http.StatusForbidden},
		{token: "bob-token", status: http.StatusForbidden},
	} {
		dispatched.Store(0)
		status, body := postTaskRequest(t, srv, denial.token, false, mcp.MethodTasksList, map[string]any{})
		require.Equal(t, denial.status, status, string(body))
		require.Zero(t, dispatched.Load())
		require.NotContains(t, string(body), aliceTask)
		require.NotContains(t, string(body), bobTask)
	}
	dispatched.Store(0)
	status, body := postTaskRequest(t, srv, "operator-token", false, mcp.MethodTasksList, map[string]any{})
	require.Equal(t, http.StatusOK, status, string(body))
	var listed mcp.ListTasksResult
	require.NoError(t, json.Unmarshal(taskResult(t, body), &listed))
	require.Len(t, listed.Tasks, 2)
	require.ElementsMatch(t, []string{aliceTask, bobTask}, []string{listed.Tasks[0].TaskId, listed.Tasks[1].TaskId})
	require.Equal(t, int32(1), dispatched.Load())
}
