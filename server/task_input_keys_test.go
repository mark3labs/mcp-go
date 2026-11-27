package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

// inputRoundTool elicits the given keys, one per round, using the MRTR
// requestState to track rounds: each retry echoes the state of the round it
// answered, exactly as the synchronous multi round-trip flow does.
func inputRoundTool(srv *MCPServer, name string, keys []string) {
	stateFor := func(round int) string { return fmt.Sprintf("round-%d", round) }
	srv.AddTool(
		mcp.NewTool(name, mcp.WithTaskSupport(mcp.TaskSupportOptional)),
		func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			round := 0
			for i := range keys {
				if req.Params.RequestState == stateFor(i) {
					round = i + 1
				}
			}
			if round >= len(keys) {
				return mcp.NewToolResultText("done"), nil
			}
			key := keys[round]
			return NewInputRequestBuilder(stateFor(round)).
				Elicit(key, mcp.ElicitationParams{
					Mode:            mcp.ElicitationModeForm,
					Message:         "Round " + key,
					RequestedSchema: map[string]any{"type": "object"},
				}).ToolResult(), nil
		})
}

func answerFor(key string) mcp.InputResponses {
	return mcp.InputResponses{key: mcp.NewElicitationInputResponse(mcp.ElicitationResult{
		ElicitationResponse: mcp.ElicitationResponse{Action: mcp.ElicitationResponseActionAccept},
	})}
}

// TestAwaitTaskInput_KeyReuseFailsTask verifies that a key re-issued in a
// later round of the same task fails the task instead of confusing clients
// that deduplicate by key.
func TestAwaitTaskInput_KeyReuseFailsTask(t *testing.T) {
	srv := NewMCPServer("key-test", "1.0.0", WithTaskCapabilities(true, true, true), WithTasksExtension())
	inputRoundTool(srv, "flaky", []string{"confirm", "confirm"})

	ctx := buildModernContext(t.Context(), srv)
	created, reqErr := srv.handleToolCall(ctx, 1, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "flaky"}})
	require.Nil(t, reqErr)
	task, ok := created.(*mcp.CreateTaskResult)
	require.True(t, ok, "got %T", created)
	taskID := task.Task.TaskId
	t.Cleanup(func() { _ = srv.cancelTask(ctx, taskID, false) })

	// Answer round one. the handler then re-elicits with the same key, which
	// must fail the task.
	require.Eventually(t, func() bool {
		got, e := srv.handleGetTask(ctx, 2, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
		return e == nil && got.Status == mcp.TaskStatusInputRequired
	}, 5*time.Second, 10*time.Millisecond)

	_, reqErr = srv.handleUpdateTask(ctx, 3, mcp.UpdateTaskRequest{
		Params: mcp.UpdateTaskParams{TaskId: taskID, InputResponses: answerFor("confirm")},
	})
	require.Nil(t, reqErr)

	require.Eventually(t, func() bool {
		got, e := srv.handleGetTask(ctx, 4, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
		return e == nil && got.Status == mcp.TaskStatusFailed
	}, 5*time.Second, 10*time.Millisecond)

	got, reqErr := srv.handleGetTask(ctx, 4, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
	require.Nil(t, reqErr)
	require.NotNil(t, got.TaskError)
	assert.Contains(t, got.TaskError.Message, `"confirm"`, "error names the re-issued key")
}

// TestAwaitTaskInput_DistinctKeysAcrossRoundsSucceed is the control: a second
// round with a fresh key completes normally.
func TestAwaitTaskInput_DistinctKeysAcrossRoundsSucceed(t *testing.T) {
	srv := NewMCPServer("key-test", "1.0.0", WithTaskCapabilities(true, true, true), WithTasksExtension())
	inputRoundTool(srv, "tworound", []string{"q1", "q2"})

	ctx := buildModernContext(t.Context(), srv)
	created, reqErr := srv.handleToolCall(ctx, 1, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "tworound"}})
	require.Nil(t, reqErr)
	task, ok := created.(*mcp.CreateTaskResult)
	require.True(t, ok, "got %T", created)
	taskID := task.Task.TaskId
	t.Cleanup(func() { _ = srv.cancelTask(ctx, taskID, false) })

	for i, key := range []string{"q1", "q2"} {
		require.Eventually(t, func() bool {
			got, e := srv.handleGetTask(ctx, 2, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
			if e != nil || got.Status != mcp.TaskStatusInputRequired {
				return false
			}
			_, pending := got.InputRequests[key]
			return pending
		}, 5*time.Second, 10*time.Millisecond, "waiting to answer %s", key)

		got, reqErr := srv.handleGetTask(ctx, 2, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
		require.Nil(t, reqErr)
		require.Contains(t, got.InputRequests, key)

		_, reqErr = srv.handleUpdateTask(ctx, 10+i, mcp.UpdateTaskRequest{
			Params: mcp.UpdateTaskParams{TaskId: taskID, InputResponses: answerFor(key)},
		})
		require.Nil(t, reqErr)
	}

	require.Eventually(t, func() bool {
		got, e := srv.handleGetTask(ctx, 5, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
		return e == nil && got.Status == mcp.TaskStatusCompleted
	}, 5*time.Second, 10*time.Millisecond)

	got, reqErr := srv.handleGetTask(ctx, 5, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
	require.Nil(t, reqErr)
	assert.NotNil(t, got.TaskResult)
}
