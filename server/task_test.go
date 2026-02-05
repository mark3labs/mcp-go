package server

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPServer_TaskCapabilities(t *testing.T) {
	tests := []struct {
		name                  string
		serverOptions         []ServerOption
		expectedCapabilities  bool
		expectedList          bool
		expectedCancel        bool
		expectedToolCallTasks bool
	}{
		{
			name:                  "server with full task capabilities",
			serverOptions:         []ServerOption{WithTaskCapabilities(true, true, true)},
			expectedCapabilities:  true,
			expectedList:          true,
			expectedCancel:        true,
			expectedToolCallTasks: true,
		},
		{
			name:                  "server with partial task capabilities",
			serverOptions:         []ServerOption{WithTaskCapabilities(true, false, true)},
			expectedCapabilities:  true,
			expectedList:          true,
			expectedCancel:        false,
			expectedToolCallTasks: true,
		},
		{
			name:                  "server without task capabilities",
			serverOptions:         []ServerOption{},
			expectedCapabilities:  false,
			expectedList:          false,
			expectedCancel:        false,
			expectedToolCallTasks: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewMCPServer("test-server", "1.0.0", tt.serverOptions...)

			// Initialize to get capabilities
			response := server.HandleMessage(context.Background(), []byte(`{
				"jsonrpc": "2.0",
				"id": 1,
				"method": "initialize",
				"params": {
					"protocolVersion": "2025-06-18",
					"capabilities": {},
					"clientInfo": {
						"name": "test-client",
						"version": "1.0.0"
					}
				}
			}`))

			resp, ok := response.(mcp.JSONRPCResponse)
			require.True(t, ok, "Expected JSONRPCResponse, got %T", response)

			result, ok := resp.Result.(mcp.InitializeResult)
			require.True(t, ok, "Expected InitializeResult, got %T", resp.Result)

			if tt.expectedCapabilities {
				require.NotNil(t, result.Capabilities.Tasks, "Expected tasks capability to be present")
				if tt.expectedList {
					assert.NotNil(t, result.Capabilities.Tasks.List)
				} else {
					assert.Nil(t, result.Capabilities.Tasks.List)
				}
				if tt.expectedCancel {
					assert.NotNil(t, result.Capabilities.Tasks.Cancel)
				} else {
					assert.Nil(t, result.Capabilities.Tasks.Cancel)
				}
				if tt.expectedToolCallTasks {
					require.NotNil(t, result.Capabilities.Tasks.Requests)
					require.NotNil(t, result.Capabilities.Tasks.Requests.Tools)
					assert.NotNil(t, result.Capabilities.Tasks.Requests.Tools.Call)
				}
			} else {
				assert.Nil(t, result.Capabilities.Tasks)
			}
		})
	}
}

func TestMCPServer_TaskLifecycle(t *testing.T) {
	server := NewMCPServer(
		"test-server",
		"1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	ctx := context.Background()

	// Create a task
	ttl := int64(60000)
	pollInterval := int64(1000)
	entry, err := server.createTask(ctx, "task-123", "test-tool", &ttl, &pollInterval)
	require.NoError(t, err)

	require.NotNil(t, entry)
	assert.Equal(t, "task-123", entry.task.TaskId)
	assert.Equal(t, mcp.TaskStatusWorking, entry.task.Status)
	assert.NotNil(t, entry.task.TTL)
	assert.Equal(t, int64(60000), *entry.task.TTL)

	// Get task
	retrievedTask, _, err := server.getTask(ctx, "task-123")
	require.NoError(t, err)
	assert.Equal(t, "task-123", retrievedTask.TaskId)

	// Complete task
	result := map[string]string{"result": "success"}
	server.completeTask(entry, result, nil)

	assert.Equal(t, mcp.TaskStatusCompleted, entry.task.Status)
	assert.Equal(t, result, entry.result)
	assert.Nil(t, entry.resultErr)

	// Verify channel is closed
	select {
	case <-entry.done:
		// Channel is closed as expected
	default:
		t.Fatal("Expected done channel to be closed")
	}
}

func TestMCPServer_HandleGetTask(t *testing.T) {
	server := NewMCPServer(
		"test-server",
		"1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	ctx := context.Background()

	// Create a task
	ttl := int64(60000)
	pollInterval := int64(1000)
	_, err := server.createTask(ctx, "task-456", "test-tool", &ttl, &pollInterval)
	require.NoError(t, err)

	// Get task via handler
	response := server.HandleMessage(ctx, []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "tasks/get",
		"params": {
			"taskId": "task-456"
		}
	}`))

	resp, ok := response.(mcp.JSONRPCResponse)
	require.True(t, ok, "Expected JSONRPCResponse, got %T", response)

	result, ok := resp.Result.(mcp.GetTaskResult)
	require.True(t, ok, "Expected GetTaskResult, got %T", resp.Result)

	assert.Equal(t, "task-456", result.TaskId)
	assert.Equal(t, mcp.TaskStatusWorking, result.Status)
}

func TestMCPServer_HandleGetTaskNotFound(t *testing.T) {
	server := NewMCPServer(
		"test-server",
		"1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	response := server.HandleMessage(context.Background(), []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "tasks/get",
		"params": {
			"taskId": "nonexistent"
		}
	}`))

	errResp, ok := response.(mcp.JSONRPCError)
	require.True(t, ok, "Expected JSONRPCError, got %T", response)
	assert.Equal(t, mcp.INVALID_PARAMS, errResp.Error.Code)
}

func TestMCPServer_HandleListTasks(t *testing.T) {
	server := NewMCPServer(
		"test-server",
		"1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	ctx := context.Background()

	// Create multiple tasks
	ttl := int64(60000)
	pollInterval := int64(1000)
	_, err := server.createTask(ctx, "task-1", "test-tool", &ttl, &pollInterval)
	require.NoError(t, err)
	_, err = server.createTask(ctx, "task-2", "test-tool", &ttl, &pollInterval)
	require.NoError(t, err)
	_, err = server.createTask(ctx, "task-3", "test-tool", &ttl, &pollInterval)
	require.NoError(t, err)

	// List tasks
	response := server.HandleMessage(ctx, []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "tasks/list"
	}`))

	resp, ok := response.(mcp.JSONRPCResponse)
	require.True(t, ok, "Expected JSONRPCResponse, got %T", response)

	result, ok := resp.Result.(mcp.ListTasksResult)
	require.True(t, ok, "Expected ListTasksResult, got %T", resp.Result)

	assert.Len(t, result.Tasks, 3)
	taskIds := []string{result.Tasks[0].TaskId, result.Tasks[1].TaskId, result.Tasks[2].TaskId}
	assert.Contains(t, taskIds, "task-1")
	assert.Contains(t, taskIds, "task-2")
	assert.Contains(t, taskIds, "task-3")
}

func TestMCPServer_HandleCancelTask(t *testing.T) {
	server := NewMCPServer(
		"test-server",
		"1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	ctx := context.Background()

	// Create a task
	ttl := int64(60000)
	pollInterval := int64(1000)
	entry, err := server.createTask(ctx, "task-789", "test-tool", &ttl, &pollInterval)
	require.NoError(t, err)

	// Verify initial status
	assert.Equal(t, mcp.TaskStatusWorking, entry.task.Status)

	// Cancel task
	response := server.HandleMessage(ctx, []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "tasks/cancel",
		"params": {
			"taskId": "task-789"
		}
	}`))

	resp, ok := response.(mcp.JSONRPCResponse)
	require.True(t, ok, "Expected JSONRPCResponse, got %T", response)

	result, ok := resp.Result.(mcp.CancelTaskResult)
	require.True(t, ok, "Expected CancelTaskResult, got %T", resp.Result)

	assert.Equal(t, "task-789", result.TaskId)
	assert.Equal(t, mcp.TaskStatusCancelled, result.Status)
}

func TestMCPServer_HandleCancelTerminalTask(t *testing.T) {
	server := NewMCPServer(
		"test-server",
		"1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	ctx := context.Background()

	// Create and complete a task
	ttl := int64(60000)
	pollInterval := int64(1000)
	entry, err := server.createTask(ctx, "task-completed", "test-tool", &ttl, &pollInterval)
	require.NoError(t, err)
	server.completeTask(entry, "result", nil)

	// Try to cancel completed task
	response := server.HandleMessage(ctx, []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "tasks/cancel",
		"params": {
			"taskId": "task-completed"
		}
	}`))

	errResp, ok := response.(mcp.JSONRPCError)
	require.True(t, ok, "Expected JSONRPCError, got %T", response)
	assert.Equal(t, mcp.INVALID_PARAMS, errResp.Error.Code)
}

func TestMCPServer_TaskWithoutCapabilities(t *testing.T) {
	// Server without task capabilities
	server := NewMCPServer("test-server", "1.0.0")

	tests := []struct {
		name   string
		method string
		params string
	}{
		{
			name:   "tasks/get without capability",
			method: "tasks/get",
			params: `"params": {"taskId": "task-1"}`,
		},
		{
			name:   "tasks/list without capability",
			method: "tasks/list",
			params: "",
		},
		{
			name:   "tasks/cancel without capability",
			method: "tasks/cancel",
			params: `"params": {"taskId": "task-1"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paramsStr := ""
			if tt.params != "" {
				paramsStr = "," + tt.params
			}
			requestJSON := `{
				"jsonrpc": "2.0",
				"id": 1,
				"method": "` + tt.method + `"` + paramsStr + `
			}`

			response := server.HandleMessage(context.Background(), []byte(requestJSON))

			errResp, ok := response.(mcp.JSONRPCError)
			require.True(t, ok, "Expected JSONRPCError, got %T", response)
			assert.Equal(t, mcp.METHOD_NOT_FOUND, errResp.Error.Code)
		})
	}
}

func TestMCPServer_TaskTTLCleanup(t *testing.T) {
	server := NewMCPServer(
		"test-server",
		"1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	ctx := context.Background()

	// Create a task with very short TTL
	ttl := int64(100) // 100ms
	pollInterval := int64(50)
	_, err := server.createTask(ctx, "task-ttl", "test-tool", &ttl, &pollInterval)
	require.NoError(t, err)

	// Task should exist initially
	_, _, getErr := server.getTask(ctx, "task-ttl")
	require.NoError(t, getErr)

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Task should be cleaned up and return "expired" error
	_, _, err = server.getTask(ctx, "task-ttl")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "task has expired")

	// A task that never existed should return "not found" error
	_, _, err = server.getTask(ctx, "never-existed")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "task not found")
}

func TestMCPServer_TaskExpiredVsNotFoundErrors(t *testing.T) {
	server := NewMCPServer(
		"test-server",
		"1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	ctx := context.Background()

	t.Run("expired task returns 'task has expired' error", func(t *testing.T) {
		ttl := int64(50) // 50ms
		_, err := server.createTask(ctx, "task-expired", "test-tool", &ttl, nil)
		require.NoError(t, err)

		// Wait for TTL to expire
		time.Sleep(100 * time.Millisecond)

		// All task operations should return "expired" error
		_, _, err = server.getTask(ctx, "task-expired")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "task has expired")

		_, err = server.getTaskEntry(ctx, "task-expired")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "task has expired")

		err = server.cancelTask(ctx, "task-expired")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "task has expired")
	})

	t.Run("non-existent task returns 'task not found' error", func(t *testing.T) {
		// Task that never existed
		_, _, err := server.getTask(ctx, "never-existed")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "task not found")
		assert.NotContains(t, err.Error(), "expired")

		_, err = server.getTaskEntry(ctx, "never-existed")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "task not found")
		assert.NotContains(t, err.Error(), "expired")

		err = server.cancelTask(ctx, "never-existed")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "task not found")
		assert.NotContains(t, err.Error(), "expired")
	})
}

func TestMCPServer_TaskStatusIsTerminal(t *testing.T) {
	tests := []struct {
		status     mcp.TaskStatus
		isTerminal bool
	}{
		{mcp.TaskStatusWorking, false},
		{mcp.TaskStatusInputRequired, false},
		{mcp.TaskStatusCompleted, true},
		{mcp.TaskStatusFailed, true},
		{mcp.TaskStatusCancelled, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			assert.Equal(t, tt.isTerminal, tt.status.IsTerminal())
		})
	}
}

func TestMCPServer_TaskResultWaitForCompletion(t *testing.T) {
	server := NewMCPServer(
		"test-server",
		"1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	ctx := context.Background()

	// Create a task
	ttl := int64(60000)
	pollInterval := int64(1000)
	entry, err := server.createTask(ctx, "task-wait", "test-tool", &ttl, &pollInterval)
	require.NoError(t, err)

	// Start goroutine to complete task after delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		server.completeTask(entry, "delayed result", nil)
	}()

	// Request task result - should block until completion
	start := time.Now()

	// Use a channel to capture the response
	responseChan := make(chan mcp.JSONRPCMessage, 1)
	go func() {
		response := server.HandleMessage(ctx, []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "tasks/result",
			"params": {
				"taskId": "task-wait"
			}
		}`))
		responseChan <- response
	}()

	// Wait for response
	select {
	case response := <-responseChan:
		elapsed := time.Since(start)

		// Should have waited for completion
		assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(90))

		resp, ok := response.(mcp.JSONRPCResponse)
		require.True(t, ok, "Expected JSONRPCResponse, got %T", response)

		_, ok = resp.Result.(mcp.TaskResultResult)
		require.True(t, ok, "Expected TaskResultResult, got %T", resp.Result)

	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for task result")
	}
}

func TestMCPServer_CompleteTaskWithError(t *testing.T) {
	server := NewMCPServer(
		"test-server",
		"1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	ctx := context.Background()

	// Create a task
	ttl := int64(60000)
	pollInterval := int64(1000)
	entry, err := server.createTask(ctx, "task-error", "test-tool", &ttl, &pollInterval)
	require.NoError(t, err)

	// Complete with error
	testErr := assert.AnError
	server.completeTask(entry, nil, testErr)

	assert.Equal(t, mcp.TaskStatusFailed, entry.task.Status)
	assert.NotEmpty(t, entry.task.StatusMessage)
	assert.Equal(t, testErr, entry.resultErr)
}

func TestTask_HelperFunctions(t *testing.T) {
	t.Run("NewTask creates task with default values", func(t *testing.T) {
		task := mcp.NewTask("test-id")
		assert.Equal(t, "test-id", task.TaskId)
		assert.Equal(t, mcp.TaskStatusWorking, task.Status)
		assert.NotEmpty(t, task.CreatedAt)
	})

	t.Run("NewTask with options", func(t *testing.T) {
		ttl := int64(30000)
		pollInterval := int64(2000)
		task := mcp.NewTask("test-id",
			mcp.WithTaskStatus(mcp.TaskStatusCompleted),
			mcp.WithTaskStatusMessage("Done"),
			mcp.WithTaskTTL(ttl),
			mcp.WithTaskPollInterval(pollInterval),
		)

		assert.Equal(t, "test-id", task.TaskId)
		assert.Equal(t, mcp.TaskStatusCompleted, task.Status)
		assert.Equal(t, "Done", task.StatusMessage)
		require.NotNil(t, task.TTL)
		assert.Equal(t, int64(30000), *task.TTL)
		require.NotNil(t, task.PollInterval)
		assert.Equal(t, int64(2000), *task.PollInterval)
	})

	t.Run("NewTaskParams", func(t *testing.T) {
		ttl := int64(45000)
		params := mcp.NewTaskParams(&ttl)
		require.NotNil(t, params.TTL)
		assert.Equal(t, int64(45000), *params.TTL)
	})

	t.Run("NewTasksCapability", func(t *testing.T) {
		cap := mcp.NewTasksCapability()
		assert.NotNil(t, cap.List)
		assert.NotNil(t, cap.Cancel)
		assert.NotNil(t, cap.Requests)
		assert.NotNil(t, cap.Requests.Tools)
		assert.NotNil(t, cap.Requests.Tools.Call)
	})

	t.Run("NewTasksCapabilityWithToolsOnly", func(t *testing.T) {
		cap := mcp.NewTasksCapabilityWithToolsOnly()
		// List and Cancel should NOT be set with tools-only capability
		assert.Nil(t, cap.List)
		assert.Nil(t, cap.Cancel)
		// But tool call support should be enabled
		assert.NotNil(t, cap.Requests)
		assert.NotNil(t, cap.Requests.Tools)
		assert.NotNil(t, cap.Requests.Tools.Call)
	})
}

func TestMCPServer_TaskJSONMarshaling(t *testing.T) {
	task := mcp.NewTask("test-marshal",
		mcp.WithTaskStatus(mcp.TaskStatusCompleted),
		mcp.WithTaskStatusMessage("Test complete"),
	)

	// Marshal to JSON
	data, err := json.Marshal(task)
	require.NoError(t, err)

	// Verify JSON contains lastUpdatedAt field
	var jsonMap map[string]any
	err = json.Unmarshal(data, &jsonMap)
	require.NoError(t, err)
	assert.Contains(t, jsonMap, "lastUpdatedAt", "JSON should contain lastUpdatedAt field")
	assert.Contains(t, jsonMap, "createdAt", "JSON should contain createdAt field")

	// Unmarshal back
	var unmarshaled mcp.Task
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, task.TaskId, unmarshaled.TaskId)
	assert.Equal(t, task.Status, unmarshaled.Status)
	assert.Equal(t, task.StatusMessage, unmarshaled.StatusMessage)
	assert.Equal(t, task.CreatedAt, unmarshaled.CreatedAt)
	assert.Equal(t, task.LastUpdatedAt, unmarshaled.LastUpdatedAt)
}

func TestMCPServer_TaskListPagination(t *testing.T) {
	server := NewMCPServer(
		"test-server",
		"1.0.0",
		WithTaskCapabilities(true, true, true),
		WithPaginationLimit(2), // Limit to 2 tasks per page
	)

	ctx := context.Background()

	// Create multiple tasks with predictable IDs
	ttl := int64(60000)
	pollInterval := int64(1000)
	_, err := server.createTask(ctx, "task-alpha", "test-tool", &ttl, &pollInterval)
	require.NoError(t, err)
	_, err = server.createTask(ctx, "task-beta", "test-tool", &ttl, &pollInterval)
	require.NoError(t, err)
	_, err = server.createTask(ctx, "task-gamma", "test-tool", &ttl, &pollInterval)
	require.NoError(t, err)
	_, err = server.createTask(ctx, "task-delta", "test-tool", &ttl, &pollInterval)
	require.NoError(t, err)

	t.Run("first page", func(t *testing.T) {
		// List first page (no cursor)
		response := server.HandleMessage(ctx, []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "tasks/list"
		}`))

		resp, ok := response.(mcp.JSONRPCResponse)
		require.True(t, ok, "Expected JSONRPCResponse, got %T", response)

		result, ok := resp.Result.(mcp.ListTasksResult)
		require.True(t, ok, "Expected ListTasksResult, got %T", resp.Result)

		// Should get first 2 tasks (sorted by TaskId)
		assert.Len(t, result.Tasks, 2)
		assert.Equal(t, "task-alpha", result.Tasks[0].TaskId)
		assert.Equal(t, "task-beta", result.Tasks[1].TaskId)

		// Should have nextCursor since there are more tasks
		assert.NotEmpty(t, result.NextCursor)
	})

	t.Run("second page", func(t *testing.T) {
		// Get first page to get cursor
		response := server.HandleMessage(ctx, []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "tasks/list"
		}`))

		resp := response.(mcp.JSONRPCResponse)
		firstPage := resp.Result.(mcp.ListTasksResult)
		cursor := firstPage.NextCursor

		// List second page with cursor
		requestJSON := `{
			"jsonrpc": "2.0",
			"id": 2,
			"method": "tasks/list",
			"params": {
				"cursor": "` + string(cursor) + `"
			}
		}`

		response = server.HandleMessage(ctx, []byte(requestJSON))
		resp, ok := response.(mcp.JSONRPCResponse)
		require.True(t, ok, "Expected JSONRPCResponse, got %T", response)

		result, ok := resp.Result.(mcp.ListTasksResult)
		require.True(t, ok, "Expected ListTasksResult, got %T", resp.Result)

		// Should get next 2 tasks
		assert.Len(t, result.Tasks, 2)
		assert.Equal(t, "task-delta", result.Tasks[0].TaskId)
		assert.Equal(t, "task-gamma", result.Tasks[1].TaskId)

		// Should have nextCursor even though this is the last page (client will fetch empty third page)
		assert.NotEmpty(t, result.NextCursor)

		// Fetch third page to confirm it's empty
		requestJSON3 := `{
			"jsonrpc": "2.0",
			"id": 3,
			"method": "tasks/list",
			"params": {
				"cursor": "` + string(result.NextCursor) + `"
			}
		}`

		response = server.HandleMessage(ctx, []byte(requestJSON3))
		resp, ok = response.(mcp.JSONRPCResponse)
		require.True(t, ok, "Expected JSONRPCResponse, got %T", response)

		result, ok = resp.Result.(mcp.ListTasksResult)
		require.True(t, ok, "Expected ListTasksResult, got %T", resp.Result)

		// Third page should be empty with no cursor
		assert.Empty(t, result.Tasks)
		assert.Empty(t, result.NextCursor)
	})

	t.Run("without pagination limit", func(t *testing.T) {
		// Server without pagination limit
		serverNoPagination := NewMCPServer(
			"test-server-no-pagination",
			"1.0.0",
			WithTaskCapabilities(true, true, true),
		)

		// Create tasks
		_, err := serverNoPagination.createTask(ctx, "task-1", "test-tool", &ttl, &pollInterval)
		require.NoError(t, err)
		_, err = serverNoPagination.createTask(ctx, "task-2", "test-tool", &ttl, &pollInterval)
		require.NoError(t, err)
		_, err = serverNoPagination.createTask(ctx, "task-3", "test-tool", &ttl, &pollInterval)
		require.NoError(t, err)

		// List all tasks
		response := serverNoPagination.HandleMessage(ctx, []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "tasks/list"
		}`))

		resp, ok := response.(mcp.JSONRPCResponse)
		require.True(t, ok, "Expected JSONRPCResponse, got %T", response)

		result, ok := resp.Result.(mcp.ListTasksResult)
		require.True(t, ok, "Expected ListTasksResult, got %T", resp.Result)

		// Should get all tasks
		assert.Len(t, result.Tasks, 3)
		// Should not have nextCursor
		assert.Empty(t, result.NextCursor)
	})

	t.Run("invalid cursor", func(t *testing.T) {
		// Try to list with invalid cursor
		response := server.HandleMessage(ctx, []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "tasks/list",
			"params": {
				"cursor": "invalid-cursor"
			}
		}`))

		errResp, ok := response.(mcp.JSONRPCError)
		require.True(t, ok, "Expected JSONRPCError, got %T", response)
		assert.Equal(t, mcp.INVALID_PARAMS, errResp.Error.Code)
	})
}

func TestTask_GetName(t *testing.T) {
	task := mcp.NewTask("test-task-id")
	assert.Equal(t, "test-task-id", task.GetName())
}

func TestMCPServer_TaskLastUpdatedAt(t *testing.T) {
	server := NewMCPServer(
		"test-server",
		"1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	ctx := context.Background()

	t.Run("task creation sets initial lastUpdatedAt", func(t *testing.T) {
		ttl := int64(60000)
		pollInterval := int64(1000)
		entry, err := server.createTask(ctx, "task-initial", "test-tool", &ttl, &pollInterval)
		require.NoError(t, err)

		require.NotNil(t, entry)
		assert.NotEmpty(t, entry.task.CreatedAt, "CreatedAt should be set")
		assert.NotEmpty(t, entry.task.LastUpdatedAt, "LastUpdatedAt should be set")
		assert.Equal(t, entry.task.CreatedAt, entry.task.LastUpdatedAt, "Initial lastUpdatedAt should equal createdAt")
	})

	t.Run("completeTask updates lastUpdatedAt", func(t *testing.T) {
		ttl := int64(60000)
		pollInterval := int64(1000)
		entry, err := server.createTask(ctx, "task-complete", "test-tool", &ttl, &pollInterval)
		require.NoError(t, err)

		initialLastUpdatedAt := entry.task.LastUpdatedAt
		createdAt := entry.task.CreatedAt

		// Sleep to ensure timestamp will be different (RFC3339 has second precision)
		time.Sleep(1100 * time.Millisecond)

		// Complete the task
		result := map[string]string{"result": "success"}
		server.completeTask(entry, result, nil)

		assert.Equal(t, mcp.TaskStatusCompleted, entry.task.Status)
		assert.NotEmpty(t, entry.task.LastUpdatedAt, "LastUpdatedAt should still be set")
		assert.NotEqual(t, initialLastUpdatedAt, entry.task.LastUpdatedAt, "LastUpdatedAt should be updated")
		assert.Equal(t, createdAt, entry.task.CreatedAt, "CreatedAt should not change")
	})

	t.Run("completeTask with error updates lastUpdatedAt", func(t *testing.T) {
		ttl := int64(60000)
		pollInterval := int64(1000)
		entry, err := server.createTask(ctx, "task-error", "test-tool", &ttl, &pollInterval)
		require.NoError(t, err)

		initialLastUpdatedAt := entry.task.LastUpdatedAt
		createdAt := entry.task.CreatedAt

		// Sleep to ensure timestamp will be different (RFC3339 has second precision)
		time.Sleep(1100 * time.Millisecond)

		// Complete the task with error
		testErr := fmt.Errorf("test error")
		server.completeTask(entry, nil, testErr)

		assert.Equal(t, mcp.TaskStatusFailed, entry.task.Status)
		assert.NotEmpty(t, entry.task.LastUpdatedAt, "LastUpdatedAt should still be set")
		assert.NotEqual(t, initialLastUpdatedAt, entry.task.LastUpdatedAt, "LastUpdatedAt should be updated")
		assert.Equal(t, createdAt, entry.task.CreatedAt, "CreatedAt should not change")
	})

	t.Run("cancelTask updates lastUpdatedAt", func(t *testing.T) {
		ttl := int64(60000)
		pollInterval := int64(1000)
		entry, err := server.createTask(ctx, "task-cancel", "test-tool", &ttl, &pollInterval)
		require.NoError(t, err)

		initialLastUpdatedAt := entry.task.LastUpdatedAt
		createdAt := entry.task.CreatedAt

		// Sleep to ensure timestamp will be different (RFC3339 has second precision)
		time.Sleep(1100 * time.Millisecond)

		// Cancel the task
		cancelErr := server.cancelTask(ctx, "task-cancel")
		require.NoError(t, cancelErr)

		assert.Equal(t, mcp.TaskStatusCancelled, entry.task.Status)
		assert.NotEmpty(t, entry.task.LastUpdatedAt, "LastUpdatedAt should still be set")
		assert.NotEqual(t, initialLastUpdatedAt, entry.task.LastUpdatedAt, "LastUpdatedAt should be updated")
		assert.Equal(t, createdAt, entry.task.CreatedAt, "CreatedAt should not change")
	})

	t.Run("lastUpdatedAt is included in task responses", func(t *testing.T) {
		ttl := int64(60000)
		pollInterval := int64(1000)
		_, err := server.createTask(ctx, "task-response", "test-tool", &ttl, &pollInterval)
		require.NoError(t, err)

		// Get task via handler
		response := server.HandleMessage(ctx, []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "tasks/get",
			"params": {
				"taskId": "task-response"
			}
		}`))

		resp, ok := response.(mcp.JSONRPCResponse)
		require.True(t, ok, "Expected JSONRPCResponse, got %T", response)

		result, ok := resp.Result.(mcp.GetTaskResult)
		require.True(t, ok, "Expected GetTaskResult, got %T", resp.Result)

		assert.NotEmpty(t, result.LastUpdatedAt, "LastUpdatedAt should be in response")
		assert.NotEmpty(t, result.CreatedAt, "CreatedAt should be in response")
	})
}

// fakeSess is a test helper for simulating client sessions
type fakeSess struct {
	sessionID  string
	notifyChan chan mcp.JSONRPCNotification
}

func (f fakeSess) SessionID() string {
	return f.sessionID
}

func (f fakeSess) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return f.notifyChan
}

func (f fakeSess) Initialize() {
}

func (f fakeSess) Initialized() bool {
	return true
}

var _ ClientSession = fakeSess{}

func TestMCPServer_TaskStatusNotifications(t *testing.T) {
	server := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	// Create a session to receive notifications
	ctx := context.Background()
	notifyChan := make(chan mcp.JSONRPCNotification, 10)
	session := fakeSess{
		sessionID:  "test-session",
		notifyChan: notifyChan,
	}
	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	t.Run("task completion sends notification", func(t *testing.T) {
		// Create a task
		ttl := int64(60000)
		pollInterval := int64(5000)
		entry, err := server.createTask(ctx, "task-notify-complete", "test-tool", &ttl, &pollInterval)
		require.NoError(t, err)

		// Clear any initial notifications
		for len(notifyChan) > 0 {
			<-notifyChan
		}

		// Complete the task
		result := &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent("test result"),
			},
		}
		server.completeTask(entry, result, nil)

		// Check for notification
		select {
		case notification := <-notifyChan:
			assert.Equal(t, mcp.MethodNotificationTasksStatus, notification.Method)

			// Verify notification params contain task data
			params := notification.Params.AdditionalFields
			require.NotNil(t, params, "Expected params to be set")

			assert.Equal(t, "task-notify-complete", params["taskId"])
			assert.Equal(t, mcp.TaskStatusCompleted, params["status"])
			assert.NotEmpty(t, params["createdAt"])
			assert.NotEmpty(t, params["lastUpdatedAt"])
			assert.Equal(t, int64(60000), params["ttl"])
			assert.Equal(t, int64(5000), params["pollInterval"])

		case <-time.After(1 * time.Second):
			t.Fatal("Expected task status notification but none received")
		}
	})

	t.Run("task failure sends notification with error message", func(t *testing.T) {
		// Create a task
		entry, err := server.createTask(ctx, "task-notify-fail", "test-tool", nil, nil)
		require.NoError(t, err)

		// Clear any initial notifications
		for len(notifyChan) > 0 {
			<-notifyChan
		}

		// Fail the task
		testErr := fmt.Errorf("test error")
		server.completeTask(entry, nil, testErr)

		// Check for notification
		select {
		case notification := <-notifyChan:
			assert.Equal(t, mcp.MethodNotificationTasksStatus, notification.Method)

			params := notification.Params.AdditionalFields
			require.NotNil(t, params, "Expected params to be set")

			assert.Equal(t, "task-notify-fail", params["taskId"])
			assert.Equal(t, mcp.TaskStatusFailed, params["status"])
			assert.Equal(t, "test error", params["statusMessage"])

		case <-time.After(1 * time.Second):
			t.Fatal("Expected task status notification but none received")
		}
	})

	t.Run("task cancellation sends notification", func(t *testing.T) {
		// Create a task
		_, err := server.createTask(ctx, "task-notify-cancel", "test-tool", nil, nil)
		require.NoError(t, err)

		// Clear any initial notifications
		for len(notifyChan) > 0 {
			<-notifyChan
		}

		// Cancel the task
		cancelErr := server.cancelTask(ctx, "task-notify-cancel")
		require.NoError(t, cancelErr)

		// Check for notification
		select {
		case notification := <-notifyChan:
			assert.Equal(t, mcp.MethodNotificationTasksStatus, notification.Method)

			params := notification.Params.AdditionalFields
			require.NotNil(t, params, "Expected params to be set")

			assert.Equal(t, "task-notify-cancel", params["taskId"])
			assert.Equal(t, mcp.TaskStatusCancelled, params["status"])
			assert.Equal(t, "Task cancelled by request", params["statusMessage"])

		case <-time.After(1 * time.Second):
			t.Fatal("Expected task status notification but none received")
		}
	})

	t.Run("notification includes optional fields when present", func(t *testing.T) {
		// Create a task with TTL and pollInterval
		ttl := int64(30000)
		pollInterval := int64(2000)
		entry, err := server.createTask(ctx, "task-notify-fields", "test-tool", &ttl, &pollInterval)
		require.NoError(t, err)

		// Clear any initial notifications
		for len(notifyChan) > 0 {
			<-notifyChan
		}

		// Complete the task
		server.completeTask(entry, "result", nil)

		// Check for notification
		select {
		case notification := <-notifyChan:
			params := notification.Params.AdditionalFields
			require.NotNil(t, params)

			assert.Equal(t, int64(30000), params["ttl"])
			assert.Equal(t, int64(2000), params["pollInterval"])

		case <-time.After(1 * time.Second):
			t.Fatal("Expected task status notification but none received")
		}
	})

	t.Run("notification omits optional fields when nil", func(t *testing.T) {
		// Create a task without TTL and pollInterval
		entry, err := server.createTask(ctx, "task-notify-no-fields", "test-tool", nil, nil)
		require.NoError(t, err)

		// Clear any initial notifications
		for len(notifyChan) > 0 {
			<-notifyChan
		}

		// Complete the task
		server.completeTask(entry, "result", nil)

		// Check for notification
		select {
		case notification := <-notifyChan:
			params := notification.Params.AdditionalFields
			require.NotNil(t, params)

			_, hasTTL := params["ttl"]
			assert.False(t, hasTTL, "ttl should not be present when nil")

			_, hasPollInterval := params["pollInterval"]
			assert.False(t, hasPollInterval, "pollInterval should not be present when nil")

		case <-time.After(1 * time.Second):
			t.Fatal("Expected task status notification but none received")
		}
	})

	t.Run("multiple clients receive notifications", func(t *testing.T) {
		// Create two additional sessions
		notifyChan1 := make(chan mcp.JSONRPCNotification, 10)
		session1 := fakeSess{
			sessionID:  "test-session-1",
			notifyChan: notifyChan1,
		}
		err := server.RegisterSession(ctx, session1)
		require.NoError(t, err)

		notifyChan2 := make(chan mcp.JSONRPCNotification, 10)
		session2 := fakeSess{
			sessionID:  "test-session-2",
			notifyChan: notifyChan2,
		}
		err = server.RegisterSession(ctx, session2)
		require.NoError(t, err)

		// Create a task
		entry, err := server.createTask(ctx, "task-notify-multi", "test-tool", nil, nil)
		require.NoError(t, err)

		// Clear any initial notifications
		for len(notifyChan1) > 0 {
			<-notifyChan1
		}
		for len(notifyChan2) > 0 {
			<-notifyChan2
		}

		// Complete the task
		server.completeTask(entry, "result", nil)

		// Both sessions should receive notification
		select {
		case notification := <-notifyChan1:
			assert.Equal(t, mcp.MethodNotificationTasksStatus, notification.Method)
		case <-time.After(1 * time.Second):
			t.Fatal("Expected notification on session1")
		}

		select {
		case notification := <-notifyChan2:
			assert.Equal(t, mcp.MethodNotificationTasksStatus, notification.Method)
		case <-time.After(1 * time.Second):
			t.Fatal("Expected notification on session2")
		}
	})

	t.Run("notification not sent on double completion", func(t *testing.T) {
		// Create a task
		entry, err := server.createTask(ctx, "task-notify-double", "test-tool", nil, nil)
		require.NoError(t, err)

		// Clear any initial notifications
		for len(notifyChan) > 0 {
			<-notifyChan
		}

		// Complete the task once
		server.completeTask(entry, "result", nil)

		// Wait for and consume first notification
		select {
		case <-notifyChan:
			// First notification received as expected
		case <-time.After(1 * time.Second):
			t.Fatal("Expected first notification")
		}

		// Try to complete again (should be ignored due to guard)
		server.completeTask(entry, "result2", nil)

		// Should not receive a second notification
		select {
		case <-notifyChan:
			t.Fatal("Should not receive notification on double completion")
		case <-time.After(100 * time.Millisecond):
			// Expected - no notification
		}
	})
}

func TestMCPServer_ExecuteTaskTool(t *testing.T) {
	t.Run("successful task execution stores result", func(t *testing.T) {
		server := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))
		ctx := context.Background()

		// Create a task tool that returns a successful result
		taskTool := ServerTaskTool{
			Tool: mcp.NewTool("test_tool",
				mcp.WithTaskSupport(mcp.TaskSupportRequired),
			),
			Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
				return &mcp.CreateTaskResult{
					Task: mcp.NewTask("test-task-1"),
				}, nil
			},
		}

		// Create a task entry
		entry, err := server.createTask(ctx, "test-task-1", "test-tool", nil, nil)
		require.NoError(t, err)

		// Execute the task tool
		request := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "test_tool",
			},
		}

		// Execute in a goroutine
		go server.executeTaskTool(ctx, entry, taskTool, request)

		// Wait for completion
		select {
		case <-entry.done:
			// Task completed
		case <-time.After(1 * time.Second):
			t.Fatal("Task did not complete in time")
		}

		// Verify task is completed successfully
		server.tasksMu.RLock()
		assert.Equal(t, mcp.TaskStatusCompleted, entry.task.Status)
		assert.NotNil(t, entry.result)
		assert.Nil(t, entry.resultErr)
		assert.True(t, entry.completed)
		server.tasksMu.RUnlock()
	})

	t.Run("failed task execution stores error", func(t *testing.T) {
		server := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))
		ctx := context.Background()

		expectedErr := fmt.Errorf("task execution failed")

		// Create a task tool that returns an error
		taskTool := ServerTaskTool{
			Tool: mcp.NewTool("failing_tool",
				mcp.WithTaskSupport(mcp.TaskSupportRequired),
			),
			Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
				return nil, expectedErr
			},
		}

		// Create a task entry
		entry, err := server.createTask(ctx, "test-task-2", "test-tool", nil, nil)
		require.NoError(t, err)

		// Execute the task tool
		request := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "test_tool",
			},
		}

		// Execute in a goroutine
		go server.executeTaskTool(ctx, entry, taskTool, request)

		// Wait for completion
		select {
		case <-entry.done:
			// Task completed (with error)
		case <-time.After(1 * time.Second):
			t.Fatal("Task did not complete in time")
		}

		// Verify task is completed with error
		server.tasksMu.RLock()
		assert.Equal(t, mcp.TaskStatusFailed, entry.task.Status)
		assert.Equal(t, expectedErr.Error(), entry.task.StatusMessage)
		assert.Nil(t, entry.result)
		assert.Equal(t, expectedErr, entry.resultErr)
		assert.True(t, entry.completed)
		server.tasksMu.RUnlock()
	})

	t.Run("task can be cancelled via context", func(t *testing.T) {
		server := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))
		ctx := context.Background()

		// Channel to synchronize the test
		started := make(chan struct{})
		cancelled := make(chan struct{})

		// Create a task tool that waits for cancellation
		taskTool := ServerTaskTool{
			Tool: mcp.NewTool("cancellable_tool",
				mcp.WithTaskSupport(mcp.TaskSupportRequired),
			),
			Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
				close(started)
				<-ctx.Done()
				close(cancelled)
				return nil, ctx.Err()
			},
		}

		// Create a task entry
		entry, err := server.createTask(ctx, "test-task-3", "test-tool", nil, nil)
		require.NoError(t, err)

		// Execute the task tool
		request := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "cancellable_tool",
			},
		}

		// Execute in a goroutine
		go server.executeTaskTool(ctx, entry, taskTool, request)

		// Wait for task to start
		<-started

		// Verify cancel function is stored
		server.tasksMu.RLock()
		assert.NotNil(t, entry.cancelFunc)
		cancelFunc := entry.cancelFunc
		server.tasksMu.RUnlock()

		// Cancel the task
		cancelFunc()

		// Wait for cancellation to be processed
		select {
		case <-cancelled:
			// Cancellation detected
		case <-time.After(1 * time.Second):
			t.Fatal("Task did not detect cancellation in time")
		}

		// Wait for task to complete
		select {
		case <-entry.done:
			// Task completed
		case <-time.After(1 * time.Second):
			t.Fatal("Task did not complete after cancellation")
		}

		// Verify task was cancelled (not failed) when context error is returned
		server.tasksMu.RLock()
		assert.Equal(t, mcp.TaskStatusCancelled, entry.task.Status)
		assert.Contains(t, entry.task.StatusMessage, "context canceled")
		assert.True(t, entry.completed)
		server.tasksMu.RUnlock()
	})

	t.Run("cancel function is stored before handler execution", func(t *testing.T) {
		server := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))
		ctx := context.Background()

		// Channel to check when handler starts
		handlerStarted := make(chan struct{})

		// Create a task tool
		taskTool := ServerTaskTool{
			Tool: mcp.NewTool("test_tool",
				mcp.WithTaskSupport(mcp.TaskSupportRequired),
			),
			Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
				close(handlerStarted)
				// Give test time to check cancelFunc
				time.Sleep(100 * time.Millisecond)
				return &mcp.CreateTaskResult{
					Task: mcp.NewTask("test-task-4"),
				}, nil
			},
		}

		// Create a task entry
		entry, err := server.createTask(ctx, "test-task-4", "test-tool", nil, nil)
		require.NoError(t, err)

		// Execute the task tool
		request := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "failing_tool",
			},
		}

		// Execute in a goroutine
		go server.executeTaskTool(ctx, entry, taskTool, request)

		// Wait for handler to start
		<-handlerStarted

		// Verify cancel function is already stored
		server.tasksMu.RLock()
		assert.NotNil(t, entry.cancelFunc, "Cancel function should be stored before handler completes")
		server.tasksMu.RUnlock()

		// Wait for completion
		<-entry.done
	})

	t.Run("sends task status notification on completion", func(t *testing.T) {
		server := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))
		ctx := context.Background()

		// Register a test session to receive notifications
		notifyChan := make(chan mcp.JSONRPCNotification, 10)
		session := fakeSess{
			sessionID:  "test-session",
			notifyChan: notifyChan,
		}

		err := server.RegisterSession(ctx, session)
		require.NoError(t, err)

		// Get a context with the session
		sessionCtx := server.WithContext(ctx, session)

		// Create a task tool
		taskTool := ServerTaskTool{
			Tool: mcp.NewTool("test_tool",
				mcp.WithTaskSupport(mcp.TaskSupportRequired),
			),
			Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
				return &mcp.CreateTaskResult{
					Task: mcp.NewTask("test-task-5"),
				}, nil
			},
		}

		// Create a task entry
		entry, err := server.createTask(sessionCtx, "test-task-5", "test-tool", nil, nil)
		require.NoError(t, err)

		// Execute the task tool
		request := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "test_tool",
			},
		}

		// Execute in a goroutine
		go server.executeTaskTool(sessionCtx, entry, taskTool, request)

		// Wait for notification
		var notification mcp.JSONRPCNotification
		select {
		case notification = <-notifyChan:
			// Got notification
		case <-time.After(1 * time.Second):
			t.Fatal("Did not receive task status notification")
		}

		// Verify notification
		assert.Equal(t, mcp.MethodNotificationTasksStatus, notification.Method)
		params := notification.Params.AdditionalFields
		require.NotNil(t, params)
		assert.Equal(t, "test-task-5", params["taskId"])
		assert.Equal(t, mcp.TaskStatusCompleted, params["status"])
	})

	t.Run("multiple tasks can execute concurrently", func(t *testing.T) {
		server := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))
		ctx := context.Background()

		// Create multiple task tools
		numTasks := 5
		entries := make([]*taskEntry, numTasks)

		for i := range numTasks {
			taskID := fmt.Sprintf("concurrent-task-%d", i)
			taskNum := i // Capture for closure

			taskTool := ServerTaskTool{
				Tool: mcp.NewTool(fmt.Sprintf("tool_%d", i),
					mcp.WithTaskSupport(mcp.TaskSupportRequired),
				),
				Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
					// Simulate some work
					time.Sleep(50 * time.Millisecond)
					return &mcp.CreateTaskResult{
						Task: mcp.NewTask(fmt.Sprintf("concurrent-task-%d", taskNum)),
					}, nil
				},
			}

			// Create a task entry
			var err error
			entries[i], err = server.createTask(ctx, taskID, "test-tool", nil, nil)
			require.NoError(t, err)

			// Execute the task tool
			request := mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: fmt.Sprintf("tool_%d", i),
				},
			}

			// Execute in a goroutine
			go server.executeTaskTool(ctx, entries[i], taskTool, request)
		}

		// Wait for all tasks to complete
		for i, entry := range entries {
			select {
			case <-entry.done:
				// Task completed
			case <-time.After(2 * time.Second):
				t.Fatalf("Task %d did not complete in time", i)
			}
		}

		// Verify all tasks completed successfully
		server.tasksMu.RLock()
		for i, entry := range entries {
			assert.Equal(t, mcp.TaskStatusCompleted, entry.task.Status, "Task %d should be completed", i)
			assert.True(t, entry.completed, "Task %d should be marked completed", i)
		}
		server.tasksMu.RUnlock()
	})
}

func TestMCPServer_HandleTaskResult(t *testing.T) {
	t.Run("returns tool result with related task metadata", func(t *testing.T) {
		server := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))
		ctx := context.Background()

		// Create a task and complete it with a CallToolResult
		taskID := "test-task-result"
		entry, err := server.createTask(ctx, taskID, "test-tool", nil, nil)
		require.NoError(t, err)

		expectedContent := []mcp.Content{
			mcp.NewTextContent("Tool execution completed successfully"),
		}
		expectedStructuredContent := map[string]any{
			"status": "success",
			"count":  42,
		}

		toolResult := &mcp.CallToolResult{
			Content:           expectedContent,
			StructuredContent: expectedStructuredContent,
			IsError:           false,
		}

		// Complete the task with the tool result
		server.completeTask(entry, toolResult, nil)

		// Call handleTaskResult
		request := mcp.TaskResultRequest{
			Params: mcp.TaskResultParams{
				TaskId: taskID,
			},
		}

		result, err := server.handleTaskResult(ctx, 1, request)
		require.Nil(t, err, "handleTaskResult should not return error")
		require.NotNil(t, result, "Result should not be nil")

		// Verify the result contains the tool result fields
		assert.Equal(t, expectedContent, result.Content)
		assert.Equal(t, expectedStructuredContent, result.StructuredContent)
		assert.False(t, result.IsError)

		// Verify related task metadata is present
		require.NotNil(t, result.Meta, "Meta should not be nil")
		require.NotNil(t, result.Meta.AdditionalFields, "AdditionalFields should not be nil")

		relatedTask, ok := result.Meta.AdditionalFields[mcp.RelatedTaskMetaKey]
		require.True(t, ok, "RelatedTaskMetaKey should exist in meta")

		relatedTaskMap, ok := relatedTask.(map[string]any)
		require.True(t, ok, "Related task should be a map")
		assert.Equal(t, taskID, relatedTaskMap["taskId"])
	})

	t.Run("returns error when task failed", func(t *testing.T) {
		server := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))
		ctx := context.Background()

		// Create a task and complete it with an error
		taskID := "test-task-error"
		entry, err := server.createTask(ctx, taskID, "test-tool", nil, nil)
		require.NoError(t, err)

		expectedErr := fmt.Errorf("tool execution failed")
		server.completeTask(entry, nil, expectedErr)

		// Call handleTaskResult
		request := mcp.TaskResultRequest{
			Params: mcp.TaskResultParams{
				TaskId: taskID,
			},
		}

		result, reqErr := server.handleTaskResult(ctx, 1, request)
		require.Nil(t, result, "Result should be nil on error")
		require.NotNil(t, reqErr, "Error should not be nil")
		assert.Equal(t, mcp.INTERNAL_ERROR, reqErr.code)
		assert.Equal(t, expectedErr, reqErr.err)
	})

	t.Run("waits for task completion before returning result", func(t *testing.T) {
		server := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))
		ctx := context.Background()

		// Create a task but don't complete it yet
		taskID := "test-task-wait"
		entry, err := server.createTask(ctx, taskID, "test-tool", nil, nil)
		require.NoError(t, err)

		// Start a goroutine to complete the task after a delay
		go func() {
			time.Sleep(50 * time.Millisecond)
			toolResult := &mcp.CallToolResult{
				Content: []mcp.Content{mcp.NewTextContent("Delayed result")},
			}
			server.completeTask(entry, toolResult, nil)
		}()

		// Call handleTaskResult - should wait for completion
		request := mcp.TaskResultRequest{
			Params: mcp.TaskResultParams{
				TaskId: taskID,
			},
		}

		start := time.Now()
		result, err := server.handleTaskResult(ctx, 1, request)
		elapsed := time.Since(start)

		require.Nil(t, err, "handleTaskResult should not return error")
		require.NotNil(t, result, "Result should not be nil")

		// Verify it waited for completion
		assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(40))
		assert.Equal(t, "Delayed result", result.Content[0].(mcp.TextContent).Text)
	})

	t.Run("merges original result meta with related task meta", func(t *testing.T) {
		server := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))
		ctx := context.Background()

		// Create a task and complete it with a result that has meta
		taskID := "test-task-meta-merge"
		entry, err := server.createTask(ctx, taskID, "test-tool", nil, nil)
		require.NoError(t, err)

		toolResult := &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent("Result with meta")},
			Result: mcp.Result{
				Meta: &mcp.Meta{
					AdditionalFields: map[string]any{
						"custom-field":  "custom-value",
						"another-field": 123,
					},
				},
			},
		}

		server.completeTask(entry, toolResult, nil)

		// Call handleTaskResult
		request := mcp.TaskResultRequest{
			Params: mcp.TaskResultParams{
				TaskId: taskID,
			},
		}

		result, err := server.handleTaskResult(ctx, 1, request)
		require.Nil(t, err, "handleTaskResult should not return error")
		require.NotNil(t, result, "Result should not be nil")

		// Verify both related task meta and custom fields are present
		require.NotNil(t, result.Meta, "Meta should not be nil")
		require.NotNil(t, result.Meta.AdditionalFields, "AdditionalFields should not be nil")

		// Check related task meta
		relatedTask, ok := result.Meta.AdditionalFields[mcp.RelatedTaskMetaKey]
		require.True(t, ok, "RelatedTaskMetaKey should exist in meta")
		relatedTaskMap, ok := relatedTask.(map[string]any)
		require.True(t, ok, "Related task should be a map")
		assert.Equal(t, taskID, relatedTaskMap["taskId"])

		// Check custom fields were preserved
		assert.Equal(t, "custom-value", result.Meta.AdditionalFields["custom-field"])
		assert.Equal(t, 123, result.Meta.AdditionalFields["another-field"])
	})

	t.Run("returns error for non-existent task", func(t *testing.T) {
		server := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))
		ctx := context.Background()

		// Call handleTaskResult with non-existent task ID
		request := mcp.TaskResultRequest{
			Params: mcp.TaskResultParams{
				TaskId: "non-existent-task",
			},
		}

		result, err := server.handleTaskResult(ctx, 1, request)
		require.Nil(t, result, "Result should be nil on error")
		require.NotNil(t, err, "Error should not be nil")
		assert.Equal(t, mcp.INVALID_PARAMS, err.code)
	})
}

// TestTaskResultEndToEnd tests the complete flow of task-augmented tool call and result retrieval
func TestTaskResultEndToEnd(t *testing.T) {
	server := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))
	ctx := context.Background()

	// Register a tool with TaskSupportRequired
	tool := mcp.NewTool("long_operation",
		mcp.WithDescription("A long running operation"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)

	server.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Simulate a long operation
		time.Sleep(50 * time.Millisecond)

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent("Operation completed successfully"),
			},
			StructuredContent: map[string]any{
				"status": "success",
				"data":   "result data",
			},
		}, nil
	})

	// Step 1: Call the tool with task augmentation
	callRequest := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "long_operation",
			Task: &mcp.TaskParams{},
		},
	}

	callResult, callErr := server.handleToolCall(ctx, 1, callRequest)
	require.Nil(t, callErr, "Tool call should succeed")
	require.NotNil(t, callResult, "Call result should not be nil")

	// Extract task ID from CreateTaskResult
	createTaskResult, ok := callResult.(*mcp.CreateTaskResult)
	require.True(t, ok, "Result should be CreateTaskResult for task-augmented call")
	require.NotNil(t, createTaskResult.Task, "Task field should not be nil")

	taskID := createTaskResult.Task.TaskId
	require.NotEmpty(t, taskID, "Task ID should not be empty")

	// Step 2: Wait for task to complete (poll tasks/get)
	var taskStatus mcp.TaskStatus
	for range 20 {
		task, _, err := server.getTask(ctx, taskID)
		require.NoError(t, err, "getTask should succeed")

		taskStatus = task.Status
		if taskStatus.IsTerminal() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	assert.Equal(t, mcp.TaskStatusCompleted, taskStatus, "Task should be completed")

	// Step 3: Retrieve the result via tasks/result
	resultRequest := mcp.TaskResultRequest{
		Params: mcp.TaskResultParams{
			TaskId: taskID,
		},
	}

	result, resultErr := server.handleTaskResult(ctx, 2, resultRequest)
	require.Nil(t, resultErr, "Task result request should succeed")
	require.NotNil(t, result, "Result should not be nil")

	// Step 4: Verify the result matches the original tool result
	require.Len(t, result.Content, 1, "Should have one content item")
	textContent, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok, "Content should be TextContent")
	assert.Equal(t, "Operation completed successfully", textContent.Text)

	// Verify structured content
	require.NotNil(t, result.StructuredContent, "Structured content should not be nil")
	structuredMap, ok := result.StructuredContent.(map[string]any)
	require.True(t, ok, "Structured content should be a map")
	assert.Equal(t, "success", structuredMap["status"])
	assert.Equal(t, "result data", structuredMap["data"])

	// Step 5: Verify related task metadata
	require.NotNil(t, result.Meta, "Meta should not be nil")
	require.NotNil(t, result.Meta.AdditionalFields, "AdditionalFields should not be nil")

	relatedTask, ok := result.Meta.AdditionalFields[mcp.RelatedTaskMetaKey]
	require.True(t, ok, "RelatedTaskMetaKey should exist")

	relatedTaskMap, ok := relatedTask.(map[string]any)
	require.True(t, ok, "Related task should be a map")
	assert.Equal(t, taskID, relatedTaskMap["taskId"], "Related task ID should match")
}
