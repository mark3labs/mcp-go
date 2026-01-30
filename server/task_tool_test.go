package server

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTaskToolTracerBullet is a comprehensive end-to-end integration test
// that demonstrates the complete flow of task-augmented tools:
//
// 1. Server configuration with task capabilities
// 2. Task tool registration (both Required and Optional modes)
// 3. Tool call with task augmentation (creates task)
// 4. Async task execution with context cancellation support
// 5. Task status polling and notifications
// 6. Result retrieval via tasks/result
// 7. Related task metadata propagation
//
// This test serves as a "tracer bullet" - a complete implementation that
// validates the entire task tool feature from end to end.
func TestTaskToolTracerBullet(t *testing.T) {
	t.Run("complete task tool flow - TaskSupportRequired", func(t *testing.T) {
		// Step 1: Create server with full task capabilities
		server := NewMCPServer(
			"test-task-tool-server",
			"1.0.0",
			WithTaskCapabilities(true, true, true), // list, cancel, toolCallTasks
			WithToolCapabilities(true),             // listChanged
		)

		// Register a test session to receive notifications
		ctx := context.Background()
		notifyChan := make(chan mcp.JSONRPCNotification, 10)
		session := fakeSess{
			sessionID:  "test-session-tracer",
			notifyChan: notifyChan,
		}
		err := server.RegisterSession(ctx, session)
		require.NoError(t, err)

		sessionCtx := server.WithContext(ctx, session)

		// Step 2: Register a task-required tool
		// This tool MUST be called with task augmentation
		longRunningTool := mcp.NewTool("long_operation",
			mcp.WithDescription("A long running operation that processes data"),
			mcp.WithTaskSupport(mcp.TaskSupportRequired),
			mcp.WithString("data", mcp.Description("Data to process"), mcp.Required()),
			mcp.WithNumber("delay_ms", mcp.Description("Processing delay in milliseconds"), mcp.DefaultNumber(100)),
		)

		// Handler that simulates long-running work
		var mu sync.Mutex
		handlerCalled := false
		var receivedData string
		server.AddTaskTool(longRunningTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
			mu.Lock()
			handlerCalled = true
			receivedData = request.GetString("data", "")
			mu.Unlock()
			delayMs := request.GetFloat("delay_ms", 100)

			// Simulate processing time
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(delayMs) * time.Millisecond):
			}

			// Return successful result
			return &mcp.CreateTaskResult{
				Task: mcp.Task{
					// Task fields managed by server
				},
			}, nil
		})

		// Step 3: Verify tool appears in tools/list with proper task support
		toolsList, listErr := server.handleListTools(sessionCtx, 1, mcp.ListToolsRequest{})
		require.Nil(t, listErr)
		require.NotNil(t, toolsList)
		found := false
		for _, tool := range toolsList.Tools {
			if tool.Name == "long_operation" {
				found = true
				require.NotNil(t, tool.Execution, "Tool should have Execution metadata")
				assert.Equal(t, mcp.TaskSupportRequired, tool.Execution.TaskSupport)
				break
			}
		}
		assert.True(t, found, "Task tool should appear in tools/list")

		// Step 4: Call tool without task param - should fail for TaskSupportRequired
		callRequestNoTask := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "long_operation",
				Arguments: map[string]any{
					"data":     "test-data",
					"delay_ms": 50,
				},
			},
		}

		resultNoTask, errNoTask := server.handleToolCall(sessionCtx, 1, callRequestNoTask)
		assert.Nil(t, resultNoTask, "Should fail without task param")
		assert.NotNil(t, errNoTask, "Should return error")
		assert.Equal(t, mcp.METHOD_NOT_FOUND, errNoTask.code)
		assert.Contains(t, errNoTask.err.Error(), "requires task augmentation")
		mu.Lock()
		assert.False(t, handlerCalled, "Handler should not be called without task param")
		mu.Unlock()

		// Step 5: Call tool WITH task augmentation - should succeed
		callRequest := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "long_operation",
				Arguments: map[string]any{
					"data":     "test-data-123",
					"delay_ms": 100,
				},
				Task: &mcp.TaskParams{
					TTL: ptrInt64(60000),
				},
			},
		}

		// Clear any initial notifications
		for len(notifyChan) > 0 {
			<-notifyChan
		}

		callResult, callErr := server.handleToolCall(sessionCtx, 2, callRequest)
		if callErr != nil {
			t.Fatalf("Task-augmented call should succeed, got error: %v (code: %d)", callErr.err, callErr.code)
		}
		require.NotNil(t, callResult, "Call result should not be nil")

		// Step 6: Verify CreateTaskResult is returned with task metadata
		createTaskResult, ok := callResult.(*mcp.CreateTaskResult)
		require.True(t, ok, "Result should be CreateTaskResult for task-augmented call")
		require.NotNil(t, createTaskResult.Task, "Task field should not be nil")

		taskID := createTaskResult.Task.TaskId
		require.NotEmpty(t, taskID, "Task ID should not be empty")
		assert.Equal(t, mcp.TaskStatusWorking, createTaskResult.Task.Status)
		assert.NotEmpty(t, createTaskResult.Task.CreatedAt)
		assert.NotEmpty(t, createTaskResult.Task.LastUpdatedAt)

		// Verify TTL is set
		require.NotNil(t, createTaskResult.Task.TTL)
		assert.Equal(t, int64(60000), *createTaskResult.Task.TTL)

		// Step 7: Verify task is in working state and can be retrieved
		task, _, getErr := server.getTask(sessionCtx, taskID)
		require.NoError(t, getErr, "Should be able to get task")
		assert.Equal(t, taskID, task.TaskId)
		assert.Equal(t, mcp.TaskStatusWorking, task.Status)

		// Step 8: Wait for task to complete (poll tasks/get)
		var taskStatus mcp.TaskStatus
		for range 30 {
			task, _, err := server.getTask(sessionCtx, taskID)
			require.NoError(t, err, "getTask should succeed")

			taskStatus = task.Status
			if taskStatus.IsTerminal() {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}

		assert.Equal(t, mcp.TaskStatusCompleted, taskStatus, "Task should complete successfully")
		mu.Lock()
		assert.True(t, handlerCalled, "Handler should have been called")
		assert.Equal(t, "test-data-123", receivedData, "Handler should receive correct data")
		mu.Unlock()

		// Step 9: Verify task status notification was sent
		var statusNotification mcp.JSONRPCNotification
		timeout := time.After(2 * time.Second)
		for {
			select {
			case statusNotification = <-notifyChan:
				params := statusNotification.Params.AdditionalFields
				if params == nil {
					continue
				}
				if params["taskId"] == taskID && params["status"] == mcp.TaskStatusCompleted {
					assert.Equal(t, mcp.MethodNotificationTasksStatus, statusNotification.Method)
					goto notificationReceived
				}
				// Ignore non-matching or working notifications
			case <-timeout:
				t.Fatal("Did not receive task status completed notification")
			}
		}
	notificationReceived:

		// Step 10: Retrieve result via tasks/result
		resultRequest := mcp.TaskResultRequest{
			Params: mcp.TaskResultParams{
				TaskId: taskID,
			},
		}

		taskResult, resultErr := server.handleTaskResult(sessionCtx, 3, resultRequest)
		require.Nil(t, resultErr, "Task result request should succeed")
		require.NotNil(t, taskResult, "Task result should not be nil")

		// Step 11: Verify result contains related task metadata
		require.NotNil(t, taskResult.Meta, "Result meta should not be nil")
		require.NotNil(t, taskResult.Meta.AdditionalFields, "Result meta fields should not be nil")

		relatedTask, ok := taskResult.Meta.AdditionalFields[mcp.RelatedTaskMetaKey]
		require.True(t, ok, "Related task should be in meta")

		relatedTaskMap, ok := relatedTask.(map[string]any)
		require.True(t, ok, "Related task should be a map")
		assert.Equal(t, taskID, relatedTaskMap["taskId"])
	})

	t.Run("task tool with TaskSupportOptional - synchronous execution", func(t *testing.T) {
		// Step 1: Create server
		server := NewMCPServer(
			"test-optional-sync",
			"1.0.0",
			WithTaskCapabilities(true, true, true),
		)

		ctx := context.Background()

		// Step 2: Register a task-optional tool
		optionalTool := mcp.NewTool("flexible_operation",
			mcp.WithDescription("Can run sync or async"),
			mcp.WithTaskSupport(mcp.TaskSupportOptional),
			mcp.WithString("input", mcp.Required()),
		)

		server.AddTool(optionalTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			input := request.GetString("input", "")
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Processed: %s", input)),
				},
			}, nil
		})

		// Step 3: Call WITHOUT task param - should execute synchronously
		syncRequest := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "flexible_operation",
				Arguments: map[string]any{
					"input": "sync-test",
				},
				// No Task param
			},
		}

		syncResult, syncErr := server.handleToolCall(ctx, 1, syncRequest)
		require.Nil(t, syncErr, "Sync call should succeed")
		require.NotNil(t, syncResult, "Sync result should not be nil")

		// Verify result is CallToolResult (not CreateTaskResult)
		callToolResult, ok := syncResult.(*mcp.CallToolResult)
		require.True(t, ok, "Result should be CallToolResult for sync execution")

		// Verify result content is returned directly (not a task)
		require.Len(t, callToolResult.Content, 1)
		textContent, ok := callToolResult.Content[0].(mcp.TextContent)
		require.True(t, ok)
		assert.Equal(t, "Processed: sync-test", textContent.Text)

		// Should NOT have task metadata
		if callToolResult.Meta != nil && callToolResult.Meta.AdditionalFields != nil {
			_, hasTask := callToolResult.Meta.AdditionalFields["task"]
			assert.False(t, hasTask, "Sync execution should not have task metadata")
		}
	})

	t.Run("task tool with TaskSupportOptional - asynchronous execution", func(t *testing.T) {
		// Step 1: Create server
		server := NewMCPServer(
			"test-optional-async",
			"1.0.0",
			WithTaskCapabilities(true, true, true),
		)

		ctx := context.Background()

		// Step 2: Register a task-optional tool
		optionalTool := mcp.NewTool("flexible_operation",
			mcp.WithDescription("Can run sync or async"),
			mcp.WithTaskSupport(mcp.TaskSupportOptional),
			mcp.WithString("input", mcp.Required()),
		)

		server.AddTool(optionalTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			input := request.GetString("input", "")
			// Simulate some work
			time.Sleep(50 * time.Millisecond)
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Processed: %s", input)),
				},
			}, nil
		})

		// Step 3: Call WITH task param - should execute asynchronously
		asyncRequest := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "flexible_operation",
				Arguments: map[string]any{
					"input": "async-test",
				},
				Task: &mcp.TaskParams{}, // Task param present
			},
		}

		asyncResult, asyncErr := server.handleToolCall(ctx, 1, asyncRequest)
		require.Nil(t, asyncErr, "Async call should succeed")
		require.NotNil(t, asyncResult, "Async result should not be nil")

		// Verify task result is returned
		createTaskResult, ok := asyncResult.(*mcp.CreateTaskResult)
		require.True(t, ok, "Result should be CreateTaskResult for task-augmented call")
		require.NotNil(t, createTaskResult.Task, "Task field should not be nil")

		taskID := createTaskResult.Task.TaskId
		require.NotEmpty(t, taskID)

		// Wait for completion
		for range 20 {
			task, _, err := server.getTask(ctx, taskID)
			require.NoError(t, err)
			if task.Status.IsTerminal() {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}

		// Retrieve result
		resultRequest := mcp.TaskResultRequest{
			Params: mcp.TaskResultParams{
				TaskId: taskID,
			},
		}

		taskResult, resultErr := server.handleTaskResult(ctx, 2, resultRequest)
		require.Nil(t, resultErr)
		require.NotNil(t, taskResult)

		// Verify result content
		require.Len(t, taskResult.Content, 1)
		textContent, ok := taskResult.Content[0].(mcp.TextContent)
		require.True(t, ok)
		assert.Equal(t, "Processed: async-test", textContent.Text)
	})

	t.Run("task tool cancellation via context", func(t *testing.T) {
		// Step 1: Create server
		server := NewMCPServer(
			"test-cancellation",
			"1.0.0",
			WithTaskCapabilities(true, true, true),
		)

		ctx := context.Background()

		// Step 2: Register a long-running task tool
		started := make(chan struct{})
		cancelled := make(chan struct{})

		cancellableTool := mcp.NewTool("cancellable_operation",
			mcp.WithTaskSupport(mcp.TaskSupportRequired),
		)

		server.AddTaskTool(cancellableTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
			close(started)
			<-ctx.Done()
			close(cancelled)
			return nil, ctx.Err()
		})

		// Step 3: Call tool with task augmentation
		callRequest := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "cancellable_operation",
				Task: &mcp.TaskParams{},
			},
		}

		callResult, callErr := server.handleToolCall(ctx, 1, callRequest)
		require.Nil(t, callErr)
		require.NotNil(t, callResult)

		createTaskResult := callResult.(*mcp.CreateTaskResult)
		taskID := createTaskResult.Task.TaskId

		// Wait for task to start
		<-started

		// Step 4: Cancel the task via tasks/cancel
		cancelErr := server.cancelTask(ctx, taskID)
		require.NoError(t, cancelErr)

		// Wait for cancellation to be detected
		select {
		case <-cancelled:
			// Success
		case <-time.After(1 * time.Second):
			t.Fatal("Task did not detect cancellation")
		}

		// Step 5: Verify task status is cancelled
		task, _, err := server.getTask(ctx, taskID)
		require.NoError(t, err)
		assert.Equal(t, mcp.TaskStatusCancelled, task.Status)
	})

	t.Run("task tool error handling", func(t *testing.T) {
		// Step 1: Create server
		server := NewMCPServer(
			"test-error-handling",
			"1.0.0",
			WithTaskCapabilities(true, true, true),
		)

		ctx := context.Background()

		// Step 2: Register a tool that returns an error
		expectedErr := fmt.Errorf("processing failed: invalid input")

		failingTool := mcp.NewTool("failing_operation",
			mcp.WithTaskSupport(mcp.TaskSupportRequired),
		)

		server.AddTaskTool(failingTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
			return nil, expectedErr
		})

		// Step 3: Call tool with task augmentation
		callRequest := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "failing_operation",
				Task: &mcp.TaskParams{},
			},
		}

		callResult, callErr := server.handleToolCall(ctx, 1, callRequest)
		require.Nil(t, callErr)
		require.NotNil(t, callResult)

		createTaskResult := callResult.(*mcp.CreateTaskResult)
		taskID := createTaskResult.Task.TaskId

		// Wait for task to complete (should fail)
		var taskObj mcp.Task
		for range 20 {
			taskResult, _, err := server.getTask(ctx, taskID)
			require.NoError(t, err)
			taskObj = taskResult
			if taskObj.Status.IsTerminal() {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}

		// Step 4: Verify task failed with proper error
		assert.Equal(t, mcp.TaskStatusFailed, taskObj.Status)
		assert.Equal(t, expectedErr.Error(), taskObj.StatusMessage)

		// Step 5: Verify tasks/result returns error
		resultRequest := mcp.TaskResultRequest{
			Params: mcp.TaskResultParams{
				TaskId: taskID,
			},
		}

		taskResult, resultErr := server.handleTaskResult(ctx, 2, resultRequest)
		assert.Nil(t, taskResult, "Result should be nil on error")
		assert.NotNil(t, resultErr, "Error should be returned")
		assert.Equal(t, mcp.INTERNAL_ERROR, resultErr.code)
		assert.Equal(t, expectedErr, resultErr.err)
	})

	t.Run("task tool handler returns context.Canceled before tasks/cancel called", func(t *testing.T) {
		// This test verifies that if a handler detects context cancellation
		// (e.g., from parent context timeout) and returns ctx.Err() before
		// tasks/cancel is explicitly called, the task is still marked as cancelled
		// rather than failed.

		// Step 1: Create server
		server := NewMCPServer(
			"test-handler-cancellation",
			"1.0.0",
			WithTaskCapabilities(true, true, true),
		)

		// Step 2: Create a parent context that we'll cancel
		parentCtx, cancelParent := context.WithCancel(context.Background())
		defer cancelParent()

		// Step 3: Register a task tool that respects context cancellation
		handlerStarted := make(chan struct{})

		selfCancelTool := mcp.NewTool("self_cancel_operation",
			mcp.WithTaskSupport(mcp.TaskSupportRequired),
		)

		server.AddTaskTool(selfCancelTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
			close(handlerStarted)
			// Wait for context cancellation
			<-ctx.Done()
			// Return the context error
			return nil, ctx.Err()
		})

		// Step 4: Call tool with task augmentation
		callRequest := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "self_cancel_operation",
				Task: &mcp.TaskParams{},
			},
		}

		callResult, callErr := server.handleToolCall(parentCtx, 1, callRequest)
		require.Nil(t, callErr)
		require.NotNil(t, callResult)

		createTaskResult := callResult.(*mcp.CreateTaskResult)
		taskID := createTaskResult.Task.TaskId

		// Wait for handler to start
		<-handlerStarted

		// Step 5: Cancel the parent context (simulating external cancellation)
		cancelParent()

		// Step 6: Wait for task to complete
		var finalTask mcp.Task
		for range 20 {
			task, _, err := server.getTask(context.Background(), taskID)
			require.NoError(t, err)
			finalTask = task
			if task.Status.IsTerminal() {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}

		// Step 7: Verify task status is cancelled (not failed)
		assert.Equal(t, mcp.TaskStatusCancelled, finalTask.Status)
		assert.Contains(t, finalTask.StatusMessage, "context canceled")
	})

	t.Run("multiple concurrent task tools", func(t *testing.T) {
		// Step 1: Create server
		server := NewMCPServer(
			"test-concurrent",
			"1.0.0",
			WithTaskCapabilities(true, true, true),
		)

		ctx := context.Background()

		// Step 2: Register a task tool
		concurrentTool := mcp.NewTool("concurrent_operation",
			mcp.WithTaskSupport(mcp.TaskSupportRequired),
			mcp.WithNumber("task_num", mcp.Required()),
		)

		executionOrder := make(chan int, 5)
		server.AddTaskTool(concurrentTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
			taskNum := int(request.GetFloat("task_num", 0))
			time.Sleep(50 * time.Millisecond)
			executionOrder <- taskNum
			return &mcp.CreateTaskResult{
				Task: mcp.Task{},
			}, nil
		})

		// Step 3: Launch 5 concurrent task tool calls
		taskIDs := make([]string, 5)
		for i := range 5 {
			callRequest := mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: "concurrent_operation",
					Arguments: map[string]any{
						"task_num": float64(i),
					},
					Task: &mcp.TaskParams{},
				},
			}

			callResult, callErr := server.handleToolCall(ctx, i+1, callRequest)
			require.Nil(t, callErr)
			require.NotNil(t, callResult)

			createTaskResult := callResult.(*mcp.CreateTaskResult)
			taskIDs[i] = createTaskResult.Task.TaskId
		}

		// Step 4: Wait for all tasks to complete
		for _, taskID := range taskIDs {
			for range 20 {
				task, _, err := server.getTask(ctx, taskID)
				require.NoError(t, err)
				if task.Status.IsTerminal() {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
		}

		// Step 5: Verify all tasks executed (order may vary due to concurrency)
		close(executionOrder)
		executed := make(map[int]bool)
		for taskNum := range executionOrder {
			executed[taskNum] = true
		}

		assert.Len(t, executed, 5, "All 5 tasks should have executed")
		for i := range 5 {
			assert.True(t, executed[i], "Task %d should have executed", i)
		}
	})
}

// ptrInt64 is a helper to get a pointer to an int64
func ptrInt64(i int64) *int64 {
	return &i
}

func TestTaskTool_ModelImmediateResponse(t *testing.T) {
	// Test that the SDK provides helper functions for model immediate response
	// Note: The current SDK architecture calls task handlers asynchronously,
	// so immediate response metadata would need to be set server-side before
	// the handler is called. This test verifies the helper functions work correctly.

	t.Run("helper function creates correct metadata structure", func(t *testing.T) {
		message := "Processing your request. This may take a few minutes."
		meta := mcp.WithModelImmediateResponse(message)

		require.NotNil(t, meta)
		require.NotNil(t, meta.AdditionalFields)

		immediateResponse, ok := meta.AdditionalFields[mcp.ModelImmediateResponseMetaKey]
		assert.True(t, ok, "Metadata should contain model immediate response key")

		responseMsg, ok := immediateResponse.(string)
		assert.True(t, ok, "Immediate response should be a string")
		assert.Equal(t, message, responseMsg)
	})

	t.Run("CreateTaskResult can include immediate response", func(t *testing.T) {
		task := mcp.NewTask("task-123")
		message := "Your request is being processed."

		result := mcp.CreateTaskResult{
			Task: task,
			Result: mcp.Result{
				Meta: mcp.WithModelImmediateResponse(message),
			},
		}

		assert.NotNil(t, result.Meta)
		assert.NotNil(t, result.Meta.AdditionalFields)

		immediateResponse := result.Meta.AdditionalFields[mcp.ModelImmediateResponseMetaKey]
		assert.Equal(t, message, immediateResponse)
	})
}
