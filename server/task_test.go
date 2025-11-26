package server

import (
	"context"
	"encoding/json"
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
	entry := server.createTask(ctx, "task-123", &ttl, &pollInterval)

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
	server.createTask(ctx, "task-456", &ttl, &pollInterval)

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
	server.createTask(ctx, "task-1", &ttl, &pollInterval)
	server.createTask(ctx, "task-2", &ttl, &pollInterval)
	server.createTask(ctx, "task-3", &ttl, &pollInterval)

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
	entry := server.createTask(ctx, "task-789", &ttl, &pollInterval)

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
	entry := server.createTask(ctx, "task-completed", &ttl, &pollInterval)
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
	server.createTask(ctx, "task-ttl", &ttl, &pollInterval)

	// Task should exist initially
	_, _, err := server.getTask(ctx, "task-ttl")
	require.NoError(t, err)

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Task should be cleaned up
	_, _, err = server.getTask(ctx, "task-ttl")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "task not found")
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
	entry := server.createTask(ctx, "task-wait", &ttl, &pollInterval)

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
	entry := server.createTask(ctx, "task-error", &ttl, &pollInterval)

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

	// Unmarshal back
	var unmarshaled mcp.Task
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, task.TaskId, unmarshaled.TaskId)
	assert.Equal(t, task.Status, unmarshaled.Status)
	assert.Equal(t, task.StatusMessage, unmarshaled.StatusMessage)
}
