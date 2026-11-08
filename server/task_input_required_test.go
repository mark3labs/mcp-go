package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTaskInputRequired_RegularToolRoundTrip verifies an optional tool run as a
// task (modern, extension declared) that returns input_required.
func TestTaskInputRequired_RegularToolRoundTrip(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))

	const confirmKey = "confirm"
	srv.AddTool(
		mcp.NewTool("deploy", mcp.WithTaskSupport(mcp.TaskSupportOptional)),
		func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// Resume, the answer is present on the retry.
			if _, ok := req.Params.InputResponses[confirmKey]; ok {
				return mcp.NewToolResultText("deployed"), nil
			}
			// First call, ask for input.
			return NewInputRequestBuilder("step=1").
				Elicit(confirmKey, mcp.ElicitationParams{
					Mode:    mcp.ElicitationModeForm,
					Message: "Proceed?",
					RequestedSchema: map[string]any{
						"type":       "object",
						"properties": map[string]any{"approve": map[string]any{"type": "boolean"}},
						"required":   []string{"approve"},
					},
				}).ToolResult(), nil
		})

	ctx := buildModernContext(t.Context(), srv)

	created, reqErr := srv.handleToolCall(ctx, 1, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "deploy"},
	})
	require.Nil(t, reqErr)
	task, ok := created.(*mcp.CreateTaskResult)
	require.True(t, ok, "expected *CreateTaskResult, got %T", created)
	taskID := task.Task.TaskId

	// The async executor should park the task in input_required with the
	// pending request surfaced via tasks/get.
	require.Eventually(t, func() bool {
		got, e := srv.handleGetTask(ctx, 2, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
		return e == nil && got.Status == mcp.TaskStatusInputRequired
	}, 5*time.Second, 10*time.Millisecond)

	got, e := srv.handleGetTask(ctx, 3, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
	require.Nil(t, e)
	require.Contains(t, got.InputRequests, confirmKey, "inputRequests must surface the pending request key")

	// Answer via tasks/update.
	_, e = srv.handleUpdateTask(ctx, 4, mcp.UpdateTaskRequest{
		Params: mcp.UpdateTaskParams{
			TaskId: taskID,
			InputResponses: mcp.InputResponses{
				confirmKey: mcp.NewElicitationInputResponse(mcp.ElicitationResult{
					ElicitationResponse: mcp.ElicitationResponse{
						Action:  mcp.ElicitationResponseActionAccept,
						Content: map[string]any{"approve": true},
					},
				}),
			},
		},
	})
	require.Nil(t, e)

	// The task should resume and complete with the real result.
	require.Eventually(t, func() bool {
		got, e := srv.handleGetTask(ctx, 5, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
		return e == nil && got.Status == mcp.TaskStatusCompleted
	}, 5*time.Second, 10*time.Millisecond)

	result, e := srv.handleTaskResult(ctx, 6, mcp.TaskResultRequest{Params: mcp.TaskResultParams{TaskId: taskID}})
	require.Nil(t, e)
	require.False(t, result.IsError)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	require.Equal(t, "deployed", text.Text)
}

// TestTaskInputRequired_CancelWhileParked verifies that cancelling a task that
// is parked in input_required transitions it to cancelled and lets the executor
// goroutine exit cleanly.
func TestTaskInputRequired_CancelWhileParked(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))

	var resumed atomic.Bool
	srv.AddTool(
		mcp.NewTool("waits", mcp.WithTaskSupport(mcp.TaskSupportOptional)),
		func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if len(req.Params.InputResponses) > 0 {
				resumed.Store(true)
				return mcp.NewToolResultText("done"), nil
			}
			return NewInputRequestBuilder("").
				Elicit("x", mcp.ElicitationParams{Mode: mcp.ElicitationModeForm, Message: "?"}).
				ToolResult(), nil
		})

	ctx := buildModernContext(t.Context(), srv)

	created, reqErr := srv.handleToolCall(ctx, 1, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "waits"},
	})
	require.Nil(t, reqErr)
	taskID := created.(*mcp.CreateTaskResult).Task.TaskId

	require.Eventually(t, func() bool {
		got, e := srv.handleGetTask(ctx, 2, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
		return e == nil && got.Status == mcp.TaskStatusInputRequired
	}, 5*time.Second, 10*time.Millisecond)

	// Cancel while parked.
	_, e := srv.handleCancelTask(ctx, 3, mcp.CancelTaskRequest{Params: mcp.CancelTaskParams{TaskId: taskID}})
	require.Nil(t, e)

	require.Eventually(t, func() bool {
		got, e := srv.handleGetTask(ctx, 4, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
		return e == nil && got.Status == mcp.TaskStatusCancelled
	}, 5*time.Second, 10*time.Millisecond)

	// Give the executor a moment, it must not have resumed the handler.
	time.Sleep(50 * time.Millisecond)
	assert.False(t, resumed.Load(), "handler must not resume after cancellation")
}

// TestTaskInputRequired_SlotFreedOnExpiry verifies that cancelling a task
// parked in input_required frees its concurrency slot.
func TestTaskInputRequired_SlotFreedOnExpiry(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
		WithMaxConcurrentTasks(1),
	)

	srv.AddTool(
		mcp.NewTool("ask", mcp.WithTaskSupport(mcp.TaskSupportOptional)),
		func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if len(req.Params.InputResponses) > 0 {
				return mcp.NewToolResultText("done"), nil
			}
			return NewInputRequestBuilder("").
				Elicit("x", mcp.ElicitationParams{Mode: mcp.ElicitationModeForm, Message: "?"}).
				ToolResult(), nil
		})

	ctx := buildModernContext(t.Context(), srv)

	created, reqErr := srv.handleToolCall(ctx, 1, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "ask"},
	})
	require.Nil(t, reqErr)
	taskID := created.(*mcp.CreateTaskResult).Task.TaskId

	// Wait until parked in input_required (slot is held).
	require.Eventually(t, func() bool {
		got, e := srv.handleGetTask(ctx, 2, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
		return e == nil && got.Status == mcp.TaskStatusInputRequired
	}, 5*time.Second, 10*time.Millisecond)

	// Cancel the parked task, the executor must free the slot.
	_, e := srv.handleCancelTask(ctx, 3, mcp.CancelTaskRequest{Params: mcp.CancelTaskParams{TaskId: taskID}})
	require.Nil(t, e)

	// A new task must now be creatable despite the 1-task limit.
	require.Eventually(t, func() bool {
		c2, re := srv.handleToolCall(ctx, 4, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "ask"}})
		if re != nil {
			return false
		}
		_, ok := c2.(*mcp.CreateTaskResult)
		return ok
	}, 5*time.Second, 10*time.Millisecond, "concurrency slot must be freed after the parked task is cancelled")
}

// TestTaskInputRequired_LegacyFailsCleanly verifies a legacy client on the task
// path whose handler returns input_required fails with a clear error rather
// than hanging (tasks/update is modern-only, so it could never answer).
func TestTaskInputRequired_LegacyFailsCleanly(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))

	srv.AddTool(
		mcp.NewTool("ask", mcp.WithTaskSupport(mcp.TaskSupportOptional)),
		func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return NewInputRequestBuilder("").
				Elicit("x", mcp.ElicitationParams{Mode: mcp.ElicitationModeForm, Message: "?"}).
				ToolResult(), nil
		})

	// Legacy context (no modern protocol info) with an explicit task param.
	ctx := t.Context()
	created, reqErr := srv.handleToolCall(ctx, 1, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "ask", Task: &mcp.TaskParams{}},
	})
	require.Nil(t, reqErr)
	taskID := created.(*mcp.CreateTaskResult).Task.TaskId

	// The task must reach failed (not input_required).
	require.Eventually(t, func() bool {
		task, _, err := srv.getTask(ctx, taskID)
		return err == nil && task.Status == mcp.TaskStatusFailed
	}, 5*time.Second, 10*time.Millisecond)

	task, _, err := srv.getTask(ctx, taskID)
	require.NoError(t, err)
	assert.Equal(t, mcp.TaskStatusFailed, task.Status)
	assert.Contains(t, task.StatusMessage, "legacy task execution")
}

// TestTaskInputRequired_ValidationBlocksTaskCreation verifies that with
// WithInputSchemaValidation(), a modern call missing a required argument is
// rejected synchronously and no task is created.
func TestTaskInputRequired_ValidationBlocksTaskCreation(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
		WithInputSchemaValidation(),
	)

	var handlerRan bool
	srv.AddTool(
		mcp.NewTool("needs_arg",
			mcp.WithTaskSupport(mcp.TaskSupportOptional),
			mcp.WithString("text", mcp.Required()),
		),
		func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			handlerRan = true
			return mcp.NewToolResultText("ok"), nil
		})

	ctx := buildModernContext(t.Context(), srv)

	// Missing the required "text" argument.
	result, reqErr := srv.handleToolCall(ctx, 1, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "needs_arg"},
	})

	// Validation returns a tool execution error result (not a task handle).
	require.Nil(t, reqErr)
	callResult, ok := result.(*mcp.CallToolResult)
	require.True(t, ok, "expected *CallToolResult (validation error), got %T", result)
	assert.True(t, callResult.IsError)

	// No task should have been created, and the handler must not have run.
	assert.False(t, handlerRan)
	srv.tasksMu.RLock()
	n := len(srv.tasks)
	srv.tasksMu.RUnlock()
	assert.Equal(t, 0, n, "no task should be created when input validation fails")
}

// TestTaskGetModern_FailedPreservesErrorDetails verifies a handler that fails
// with an exposed JSON-RPC error (NewHandlerError) keeps its code, message, and
// data through modern tasks/get instead of flattening to INTERNAL_ERROR.
// testHandlerJSONRPCError (request_error_test.go) supplies INVALID_PARAMS,
// "invalid customer ID", data {field: customer_id}.
func TestTaskGetModern_FailedPreservesErrorDetails(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))

	srv.AddTool(
		mcp.NewTool("fails", mcp.WithTaskSupport(mcp.TaskSupportOptional)),
		func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return nil, NewHandlerError(testHandlerJSONRPCError{})
		})

	ctx := buildModernContext(t.Context(), srv)

	created, reqErr := srv.handleToolCall(ctx, 1, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "fails"},
	})
	require.Nil(t, reqErr)
	taskID := created.(*mcp.CreateTaskResult).Task.TaskId

	require.Eventually(t, func() bool {
		got, e := srv.handleGetTask(ctx, 2, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
		return e == nil && got.Status == mcp.TaskStatusFailed
	}, 5*time.Second, 10*time.Millisecond)

	got, e := srv.handleGetTask(ctx, 3, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
	require.Nil(t, e)
	require.NotNil(t, got.TaskError, "failed task must carry error details")
	assert.Equal(t, mcp.INVALID_PARAMS, got.TaskError.Code, "original JSON-RPC code must be preserved")
	assert.Equal(t, "invalid customer ID", got.TaskError.Message, "client-facing message must be preserved")
	assert.Equal(t, map[string]any{"field": "customer_id"}, got.TaskError.Data, "error data must be preserved")
}

// TestTaskModern_IgnoresLegacyTaskParam verifies a modern request's legacy
// params.task.ttl is ignored; the TTL comes from the server default
// (WithDefaultTaskTTL), not the removed-in-modern param.
func TestTaskModern_IgnoresLegacyTaskParam(t *testing.T) {
	const serverDefault = int64(60000)
	srv := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
		WithDefaultTaskTTL(serverDefault),
	)
	srv.AddTool(
		mcp.NewTool("work", mcp.WithTaskSupport(mcp.TaskSupportOptional)),
		func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("ok"), nil
		})

	ctx := buildModernContext(t.Context(), srv)
	stray := int64(1000)
	created, reqErr := srv.handleToolCall(ctx, 1, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "work", Task: &mcp.TaskParams{TTL: &stray}},
	})
	require.Nil(t, reqErr)
	taskID := created.(*mcp.CreateTaskResult).Task.TaskId

	task, _, err := srv.getTask(ctx, taskID)
	require.NoError(t, err)
	require.NotNil(t, task.TTL)
	assert.Equal(t, serverDefault, *task.TTL,
		"modern request must ignore legacy task.ttl and use the server default")
}

// TestDiscoverModern_OmitsLegacyTaskCapability verifies that with both
// WithTaskCapabilities and WithTasksExtension, modern discover omits the legacy
// Tasks block and advertises the extension, while legacy initialize keeps it.
func TestDiscoverModern_OmitsLegacyTaskCapability(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
		WithTasksExtension(),
	)

	// Modern discover: legacy Tasks block must be absent, extension present.
	modern := srv.serverCapabilitiesSnapshot(true)
	assert.Nil(t, modern.Tasks, "modern discovery must not advertise the legacy tasks capability")
	require.NotNil(t, modern.Extensions)
	_, hasExt := modern.Extensions[mcp.ExtensionTasks]
	assert.True(t, hasExt, "modern discovery must advertise the tasks extension")

	// Legacy initialize: legacy Tasks block must still be advertised.
	legacy := srv.serverCapabilitiesSnapshot(false)
	assert.NotNil(t, legacy.Tasks, "legacy initialize must still advertise the legacy tasks capability")
}

// TestTaskGetModern_URLElicitationMapsToInternal verifies a handler returning a
// URL-elicitation error surfaces INTERNAL_ERROR via modern tasks/get, not the
// legacy URL_ELICITATION_REQUIRED code (modern clients drive elicitation via
// multi round-trips).
func TestTaskGetModern_URLElicitationMapsToInternal(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))

	srv.AddTool(
		mcp.NewTool("needsURL", mcp.WithTaskSupport(mcp.TaskSupportOptional)),
		func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return nil, NewHandlerError(mcp.URLElicitationRequiredError{
				Elicitations: []mcp.ElicitationParams{{Message: "authorize at https://example.com/auth"}},
			})
		})

	ctx := buildModernContext(t.Context(), srv)
	created, reqErr := srv.handleToolCall(ctx, 1, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "needsURL"},
	})
	require.Nil(t, reqErr)
	taskID := created.(*mcp.CreateTaskResult).Task.TaskId

	require.Eventually(t, func() bool {
		got, e := srv.handleGetTask(ctx, 2, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
		return e == nil && got.Status == mcp.TaskStatusFailed
	}, 5*time.Second, 10*time.Millisecond)

	got, e := srv.handleGetTask(ctx, 3, mcp.GetTaskRequest{Params: mcp.GetTaskParams{TaskId: taskID}})
	require.Nil(t, e)
	require.NotNil(t, got.TaskError)
	assert.Equal(t, mcp.INTERNAL_ERROR, got.TaskError.Code,
		"modern tasks/get must not leak the legacy URL-elicitation code")
	assert.NotEqual(t, mcp.URL_ELICITATION_REQUIRED, got.TaskError.Code)
}

// TestDiscoverModern_OmitsLegacyTaskCapabilityWithoutExtension verifies the
// legacy tasks.* block is suppressed from modern discovery even with
// WithTaskCapabilities ONLY (no extension), it's not modern vocabulary while
// legacy initialize still carries it.
func TestDiscoverModern_OmitsLegacyTaskCapabilityWithoutExtension(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
		// No WithTasksExtension().
	)

	modern := srv.serverCapabilitiesSnapshot(true)
	assert.Nil(t, modern.Tasks,
		"modern discovery must not advertise legacy tasks.* even without the extension")

	legacy := srv.serverCapabilitiesSnapshot(false)
	assert.NotNil(t, legacy.Tasks, "legacy initialize must still advertise the legacy tasks capability")
}

// TestTaskInputRequired_EmptyRequestsFailsFast verifies that an input_required
// result with no input requests (the "retry later" signal) fails the task
// rather than parking it forever for a tasks/update that can't answer.
func TestTaskInputRequired_EmptyRequestsFailsFast(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))

	srv.AddTool(
		mcp.NewTool("shed", mcp.WithTaskSupport(mcp.TaskSupportOptional)),
		func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// input_required with no InputRequests.
			return &mcp.CallToolResult{
				Result: mcp.Result{ResultType: mcp.ResultTypeInputRequired},
			}, nil
		})

	ctx := buildModernContext(t.Context(), srv)
	created, reqErr := srv.handleToolCall(ctx, 1, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "shed"},
	})
	require.Nil(t, reqErr)
	taskID := created.(*mcp.CreateTaskResult).Task.TaskId

	// Must reach failed, not park in input_required.
	require.Eventually(t, func() bool {
		task, _, err := srv.getTask(ctx, taskID)
		return err == nil && task.Status == mcp.TaskStatusFailed
	}, 5*time.Second, 10*time.Millisecond)

	task, _, err := srv.getTask(ctx, taskID)
	require.NoError(t, err)
	assert.Equal(t, mcp.TaskStatusFailed, task.Status)
	assert.Contains(t, task.StatusMessage, "no input requests")
}

// TestTaskCrossOriginIsolation verifies a task created under one protocol
// surface is invisible to the other: a legacy caller cannot enumerate
// (tasks/list) or access (tasks/get) a modern-origin task, and vice versa.
func TestTaskCrossOriginIsolation(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))

	legacyCtx := t.Context()
	modernCtx := buildModernContext(t.Context(), srv)

	// A modern-origin task and a legacy-origin task.
	_, err := srv.createTask(modernCtx, "modern-task", "tool", nil, nil)
	require.NoError(t, err)
	_, err = srv.createTask(legacyCtx, "legacy-task", "tool", nil, nil)
	require.NoError(t, err)

	// Legacy caller: getTask must not reach the modern task, but reaches its own.
	_, _, err = srv.getTask(legacyCtx, "modern-task")
	assert.Error(t, err, "legacy caller must not access a modern-origin task")
	_, _, err = srv.getTask(legacyCtx, "legacy-task")
	assert.NoError(t, err, "legacy caller must access its own task")

	// Modern caller: the reverse.
	_, _, err = srv.getTask(modernCtx, "legacy-task")
	assert.Error(t, err, "modern caller must not access a legacy-origin task")
	_, _, err = srv.getTask(modernCtx, "modern-task")
	assert.NoError(t, err, "modern caller must access its own task")

	// Legacy tasks/list must enumerate only legacy-origin tasks.
	listed := srv.listTasks(legacyCtx)
	var ids []string
	for _, tk := range listed {
		ids = append(ids, tk.TaskId)
	}
	assert.Contains(t, ids, "legacy-task")
	assert.NotContains(t, ids, "modern-task", "legacy tasks/list must not enumerate modern tasks")
}

// TestTaskMissingExtensionErrorData verifies the
// MISSING_REQUIRED_CLIENT_CAPABILITY error for a modern request lacking the
// tasks extension carries the normative requiredCapabilities.extensions data
// identifying io.modelcontextprotocol/tasks.
func TestTaskMissingExtensionErrorData(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
		WithTasksExtension(),
	)

	// Modern request (declares protocol version + capabilities) but WITHOUT
	// the tasks extension, so the dispatcher's capability guard fires.
	meta, _ := json.Marshal(map[string]any{
		"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	})
	msg := `{"jsonrpc":"2.0","id":1,"method":"tasks/get","params":{"taskId":"whatever","_meta":` + string(meta) + `}}`

	response := srv.HandleMessage(t.Context(), []byte(msg))
	errResp, ok := response.(mcp.JSONRPCError)
	require.True(t, ok, "expected JSONRPCError, got %T", response)
	assert.Equal(t, mcp.MISSING_REQUIRED_CLIENT_CAPABILITY, errResp.Error.Code)

	// The error data must carry requiredCapabilities.extensions[tasks].
	data, ok := errResp.Error.Data.(map[string]any)
	require.True(t, ok, "error data must be a structured object, got %T", errResp.Error.Data)
	reqCaps, ok := data["requiredCapabilities"].(map[string]any)
	require.True(t, ok, "data must contain requiredCapabilities")
	exts, ok := reqCaps["extensions"].(map[string]any)
	require.True(t, ok, "requiredCapabilities must contain extensions")
	_, ok = exts[mcp.ExtensionTasks]
	assert.True(t, ok, "extensions must identify %s", mcp.ExtensionTasks)
}

// TestModernTaskStatusNotSentToOtherSessions verifies a modern task does not
// broadcast its id via notifications/tasks/status (which fans out to every
// session, leaking the bearer handle). Legacy tasks still broadcast.
func TestModernTaskStatusNotSentToOtherSessions(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))

	notifyChan := make(chan mcp.JSONRPCNotification, 10)
	require.NoError(t, srv.RegisterSession(t.Context(),
		fakeSess{sessionID: "observer", notifyChan: notifyChan}))

	modernCtx := buildModernContext(t.Context(), srv)
	ttl := int64(60000)
	entry, err := srv.createTask(modernCtx, "modern-task", "tool", &ttl, nil)
	require.NoError(t, err)

	srv.completeTask(entry, &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent("secret")},
	}, nil)

	select {
	case n := <-notifyChan:
		t.Fatalf("modern task leaked status broadcast: %s %v", n.Method, n.Params.AdditionalFields)
	case <-time.After(100 * time.Millisecond):
		// No broadcast — correct.
	}

	// A legacy task still broadcasts.
	legacyEntry, err := srv.createTask(t.Context(), "legacy-task", "tool", &ttl, nil)
	require.NoError(t, err)
	srv.completeTask(legacyEntry, &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent("ok")},
	}, nil)

	select {
	case n := <-notifyChan:
		assert.Equal(t, mcp.MethodNotificationTasksStatus, n.Method)
		assert.Equal(t, "legacy-task", n.Params.AdditionalFields["taskId"])
	case <-time.After(time.Second):
		t.Fatal("expected legacy task status broadcast")
	}
}

// TestLegacyTerminalCancelRejectsAfterCompletion verifies a legacy cancel of an
// already-terminal task errors (INVALID_PARAMS upstream) even when completion
// raced it, while the modern path acks. Decided under the task lock.
func TestLegacyTerminalCancelRejectsAfterCompletion(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))

	ttl := int64(60000)
	entry, err := srv.createTask(t.Context(), "legacy-terminal", "tool", &ttl, nil)
	require.NoError(t, err)
	srv.completeTask(entry, &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent("done")},
	}, nil)

	// Legacy cancel (rejectTerminal=true) of a terminal task must error.
	err = srv.cancelTask(t.Context(), "legacy-terminal", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "terminal status")

	// Modern cancel (rejectTerminal=false) of the same terminal task acks.
	err = srv.cancelTask(t.Context(), "legacy-terminal", false)
	assert.NoError(t, err)
}

// TestConcurrentUpdateAndComplete checks under the race detector that
// tasks/update reading the task status does not race a concurrent completion.
func TestConcurrentUpdateAndComplete(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))
	modernCtx := buildModernContext(t.Context(), srv)

	for i := range 50 {
		entry, err := srv.createTask(modernCtx, fmt.Sprintf("race-%d", i), "tool", nil, nil)
		require.NoError(t, err)
		taskID := entry.task.TaskId

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			// Rejection branch formats the status; must be race-free.
			_, _ = srv.handleUpdateTask(modernCtx, 1, mcp.UpdateTaskRequest{
				Params: mcp.UpdateTaskParams{
					TaskId:         taskID,
					InputResponses: mcp.InputResponses{},
				},
			})
		}()
		go func() {
			defer wg.Done()
			srv.completeTask(entry, &mcp.CallToolResult{
				Content: []mcp.Content{mcp.NewTextContent("x")},
			}, nil)
		}()
		wg.Wait()
	}
}
