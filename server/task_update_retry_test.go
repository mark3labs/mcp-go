package server

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestUpdateTask_AcksWhenNotAwaitingInput retries an accepted answer while the
// task is working and completed, without resuming the handler again.
func TestUpdateTask_AcksWhenNotAwaitingInput(t *testing.T) {
	srv := NewMCPServer("ack-test", "1.0.0", WithTaskCapabilities(true, true, true))
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
