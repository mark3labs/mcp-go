package server

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestUpdateTask_RequiresInputResponsesObject(t *testing.T) {
	tests := []struct {
		name      string
		include   bool
		responses mcp.InputResponses
		wantError bool
	}{
		{name: "omitted", wantError: true},
		{name: "null", include: true, wantError: true},
		{name: "empty object", include: true, responses: mcp.InputResponses{}},
	}
	for _, status := range []mcp.TaskStatus{mcp.TaskStatusWorking, mcp.TaskStatusInputRequired} {
		for _, tt := range tests {
			t.Run(string(status)+"/"+tt.name, func(t *testing.T) {
				var beforeCalls, afterCalls int
				hooks := &Hooks{}
				hooks.AddBeforeUpdateTask(func(context.Context, any, *mcp.UpdateTaskRequest) { beforeCalls++ })
				hooks.AddAfterUpdateTask(func(context.Context, any, *mcp.UpdateTaskRequest, *mcp.UpdateTaskResult) { afterCalls++ })
				srv := NewMCPServer("test", "1.0.0", WithTasksExtension(), WithHooks(hooks))
				ctx := buildModernContext(t.Context(), srv)
				entry, err := srv.createTask(ctx, "task", "tool", nil, nil)
				require.NoError(t, err)
				t.Cleanup(func() { _ = srv.cancelTask(ctx, "task", false) })
				srv.tasksMu.Lock()
				entry.task.Status = status
				if status == mcp.TaskStatusInputRequired {
					entry.inputRequests = mcp.InputRequests{"confirm": mcp.NewRootsInputRequest()}
				}
				srv.tasksMu.Unlock()
				params := map[string]any{"taskId": "task", "_meta": modernTaskMeta()}
				if tt.include {
					params["inputResponses"] = tt.responses
				}
				wire, err := json.Marshal(map[string]any{
					"jsonrpc": "2.0", "id": 1, "method": mcp.MethodTasksUpdate, "params": params,
				})
				require.NoError(t, err)
				response := srv.HandleMessage(ctx, wire)
				if tt.wantError {
					got, ok := response.(mcp.JSONRPCError)
					require.True(t, ok, "got %T", response)
					assert.Equal(t, mcp.INVALID_PARAMS, got.Error.Code)
					assert.Contains(t, got.Error.Message, "inputResponses")
					assert.Zero(t, beforeCalls, "invalid parameters must not reach dispatch hooks")
					assert.Zero(t, afterCalls)
				} else {
					got, ok := response.(mcp.JSONRPCResponse)
					require.True(t, ok, "got %T", response)
					result, ok := got.Result.(mcp.UpdateTaskResult)
					require.True(t, ok, "got %T", got.Result)
					assert.Equal(t, mcp.ResultTypeComplete, result.ResultType)
					assert.Equal(t, 1, beforeCalls)
					assert.Equal(t, 1, afterCalls)
				}
				srv.tasksMu.RLock()
				currentStatus := entry.task.Status
				pending := len(entry.inputRequests)
				accepted := len(entry.inputResponses)
				srv.tasksMu.RUnlock()
				assert.Equal(t, status, currentStatus)
				assert.Zero(t, accepted)
				if status == mcp.TaskStatusInputRequired {
					assert.Equal(t, 1, pending, "an empty update must leave the input outstanding")
				}
			})
		}
	}
}

// TestUpdateTask_AcksWhenNotAwaitingInput retries an accepted answer while the
// task is working and completed, without resuming the handler again.
func TestUpdateTask_AcksWhenNotAwaitingInput(t *testing.T) {
	srv := NewMCPServer("ack-test", "1.0.0", WithTaskCapabilities(true, true, true), WithTasksExtension())
	block := make(chan struct{})
	var calls atomic.Int32
	srv.AddTool(
		mcp.NewTool("slow", mcp.WithTaskSupport(mcp.TaskSupportOptional)),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			calls.Add(1)
			if _, answered := req.Params.InputResponses["confirm"]; !answered {
				return NewInputRequestBuilder("approval").Elicit("confirm", mcp.ElicitationParams{
					Mode:            mcp.ElicitationModeForm,
					Message:         "Proceed?",
					RequestedSchema: map[string]any{"type": "object"},
				}).ToolResult(), nil
			}
			select {
			case <-block:
				return mcp.NewToolResultText("done"), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})

	ctx := buildModernContext(t.Context(), srv)
	created, reqErr := srv.handleToolCall(ctx, 1, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "slow"}})
	require.Nil(t, reqErr)
	task, ok := created.(*mcp.CreateTaskResult)
	require.True(t, ok, "got %T", created)
	taskID := task.Task.TaskId
	t.Cleanup(func() { _ = srv.cancelTask(ctx, taskID, false) })

	require.Eventually(t, func() bool {
		got, e := srv.handleGetTask(ctx, 2, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
		return e == nil && got.Status == mcp.TaskStatusInputRequired
	}, 5*time.Second, 10*time.Millisecond)

	update := mcp.UpdateTaskRequest{Params: mcp.UpdateTaskParams{TaskId: taskID, InputResponses: answerFor("confirm")}}
	// Discard the first acknowledgement to model a lost response.
	_, reqErr = srv.handleUpdateTask(ctx, 3, update)
	require.Nil(t, reqErr)
	require.Eventually(t, func() bool {
		got, e := srv.handleGetTask(ctx, 4, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
		return e == nil && got.Status == mcp.TaskStatusWorking && calls.Load() == 2
	}, 5*time.Second, 10*time.Millisecond)

	result, reqErr := srv.handleUpdateTask(ctx, 5, update)
	require.Nil(t, reqErr, "retry on a working task must ack")
	require.NotNil(t, result)
	assert.Equal(t, mcp.ResultTypeComplete, result.GetResultType())
	got, reqErr := srv.handleGetTask(ctx, 6, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
	require.Nil(t, reqErr)
	assert.Equal(t, mcp.TaskStatusWorking, got.Status)

	close(block)
	require.Eventually(t, func() bool {
		got, e := srv.handleGetTask(ctx, 7, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
		return e == nil && got.Status == mcp.TaskStatusCompleted
	}, 5*time.Second, 10*time.Millisecond)
	result, reqErr = srv.handleUpdateTask(ctx, 8, update)
	require.Nil(t, reqErr, "retry on a completed task must ack")
	require.NotNil(t, result)
	assert.Equal(t, mcp.ResultTypeComplete, result.GetResultType())
	got, reqErr = srv.handleGetTask(ctx, 9, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
	require.Nil(t, reqErr)
	assert.Equal(t, mcp.TaskStatusCompleted, got.Status)
	assert.NotNil(t, got.TaskResult)
	assert.Equal(t, int32(2), calls.Load(), "retries must not re-invoke the handler")

	_, reqErr = srv.handleUpdateTask(ctx, 10, mcp.UpdateTaskRequest{Params: mcp.UpdateTaskParams{TaskId: "does-not-exist"}})
	require.NotNil(t, reqErr)
	assert.Equal(t, mcp.INVALID_PARAMS, reqErr.code)
}
