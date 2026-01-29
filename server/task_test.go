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
		result := &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent("delayed result"),
			},
		}
		server.completeTask(entry, result, nil)
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

func TestMCPServer_HandleTaskResult_ReturnsToolResult(t *testing.T) {
	t.Run("returns CallToolResult with related-task metadata", func(t *testing.T) {
		server := NewMCPServer(
			"test-server",
			"1.0.0",
			WithTaskCapabilities(true, true, true),
		)

		ctx := context.Background()

		// Create a task
		entry := server.createTask(ctx, "task-123", nil, nil)

		// Complete task with a CallToolResult
		expectedResult := &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent("Tool execution completed"),
			},
			IsError: false,
		}
		server.completeTask(entry, expectedResult, nil)

		// Call handleTaskResult
		result, err := server.handleTaskResult(ctx, 1, mcp.TaskResultRequest{
			Request: mcp.Request{Method: string(mcp.MethodTasksResult)},
			Params:  mcp.TaskResultParams{TaskId: "task-123"},
		})

		// Verify result
		require.Nil(t, err)
		require.NotNil(t, result)

		// Check that the result contains the tool content
		require.Len(t, result.Content, 1)
		assert.Equal(t, "Tool execution completed", result.Content[0].(mcp.TextContent).Text)
		assert.False(t, result.IsError)

		// Check that related-task metadata is present
		require.NotNil(t, result.Meta)
		require.NotNil(t, result.Meta.AdditionalFields)
		relatedTask, ok := result.Meta.AdditionalFields["io.modelcontextprotocol/related-task"].(map[string]any)
		require.True(t, ok, "Expected related-task metadata")
		assert.Equal(t, "task-123", relatedTask["taskId"])
	})

	t.Run("waits for task completion before returning", func(t *testing.T) {
		server := NewMCPServer(
			"test-server",
			"1.0.0",
			WithTaskCapabilities(true, true, true),
		)

		ctx := context.Background()

		// Create a task
		entry := server.createTask(ctx, "task-456", nil, nil)

		// Complete task after delay
		go func() {
			time.Sleep(100 * time.Millisecond)
			result := &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent("Async result"),
				},
			}
			server.completeTask(entry, result, nil)
		}()

		// Call handleTaskResult - should block
		start := time.Now()
		result, err := server.handleTaskResult(ctx, 1, mcp.TaskResultRequest{
			Request: mcp.Request{Method: string(mcp.MethodTasksResult)},
			Params:  mcp.TaskResultParams{TaskId: "task-456"},
		})
		elapsed := time.Since(start)

		// Verify it waited
		assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(90))

		// Verify result
		require.Nil(t, err)
		require.NotNil(t, result)
		require.Len(t, result.Content, 1)
		assert.Equal(t, "Async result", result.Content[0].(mcp.TextContent).Text)
	})

	t.Run("returns error if task failed", func(t *testing.T) {
		server := NewMCPServer(
			"test-server",
			"1.0.0",
			WithTaskCapabilities(true, true, true),
		)

		ctx := context.Background()

		// Create and fail a task
		entry := server.createTask(ctx, "task-789", nil, nil)
		testErr := fmt.Errorf("task execution failed")
		server.completeTask(entry, nil, testErr)

		// Call handleTaskResult
		result, err := server.handleTaskResult(ctx, 1, mcp.TaskResultRequest{
			Request: mcp.Request{Method: string(mcp.MethodTasksResult)},
			Params:  mcp.TaskResultParams{TaskId: "task-789"},
		})

		// Should return error
		require.NotNil(t, err)
		require.Nil(t, result)
		assert.Equal(t, mcp.INTERNAL_ERROR, err.code)
		assert.Contains(t, err.err.Error(), "task execution failed")
	})
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

// TestTaskToolHandlerFunc_TypeDefinition verifies that TaskToolHandlerFunc
// type is correctly defined and can be used.
func TestTaskToolHandlerFunc_TypeDefinition(t *testing.T) {
	// Define a simple task tool handler
	var handler TaskToolHandlerFunc = func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Return a simple tool result with content
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent("Task completed successfully"),
			},
		}, nil
	}

	// Verify the handler can be called
	ctx := context.Background()
	request := mcp.CallToolRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodToolsCall),
		},
		Params: mcp.CallToolParams{
			Name: "test-tool",
		},
	}

	result, err := handler(ctx, request)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Content, 1)
	assert.Equal(t, "Task completed successfully", result.Content[0].(mcp.TextContent).Text)
}

// TestMCPServer_HandleListToolsIncludesTaskTools verifies that tools/list
// includes both regular tools and task-augmented tools.
func TestMCPServer_HandleListToolsIncludesTaskTools(t *testing.T) {
	t.Run("Only regular tools", func(t *testing.T) {
		server := NewMCPServer(
			"test-server",
			"1.0.0",
			WithToolCapabilities(true),
			WithTaskCapabilities(true, true, true),
		)

		// Add a regular tool
		regularTool := mcp.NewTool("regular-tool",
			mcp.WithDescription("A regular tool"),
		)
		server.AddTool(regularTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})

		// List tools
		response := server.HandleMessage(context.Background(), []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "tools/list"
		}`))

		resp, ok := response.(mcp.JSONRPCResponse)
		require.True(t, ok, "Expected JSONRPCResponse, got %T", response)

		result, ok := resp.Result.(mcp.ListToolsResult)
		require.True(t, ok, "Expected ListToolsResult, got %T", resp.Result)

		assert.Len(t, result.Tools, 1)
		assert.Equal(t, "regular-tool", result.Tools[0].Name)
		assert.Nil(t, result.Tools[0].Execution, "Regular tool should not have execution field")
	})

	t.Run("Only task tools", func(t *testing.T) {
		server := NewMCPServer(
			"test-server",
			"1.0.0",
			WithToolCapabilities(true),
			WithTaskCapabilities(true, true, true),
		)

		// Add a task-augmented tool
		taskTool := mcp.NewTool("task-tool",
			mcp.WithDescription("A task tool"),
			mcp.WithTaskSupport(mcp.TaskSupportRequired),
		)
		err := server.AddTaskTool(taskTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})
		require.NoError(t, err)

		// List tools
		response := server.HandleMessage(context.Background(), []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "tools/list"
		}`))

		resp, ok := response.(mcp.JSONRPCResponse)
		require.True(t, ok, "Expected JSONRPCResponse, got %T", response)

		result, ok := resp.Result.(mcp.ListToolsResult)
		require.True(t, ok, "Expected ListToolsResult, got %T", resp.Result)

		assert.Len(t, result.Tools, 1)
		assert.Equal(t, "task-tool", result.Tools[0].Name)
		require.NotNil(t, result.Tools[0].Execution, "Task tool should have execution field")
		assert.Equal(t, mcp.TaskSupportRequired, result.Tools[0].Execution.TaskSupport)
	})

	t.Run("Mixed regular and task tools", func(t *testing.T) {
		server := NewMCPServer(
			"test-server",
			"1.0.0",
			WithToolCapabilities(true),
			WithTaskCapabilities(true, true, true),
		)

		// Add regular tools
		regularTool1 := mcp.NewTool("regular-tool-1",
			mcp.WithDescription("First regular tool"),
		)
		server.AddTool(regularTool1, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})

		regularTool2 := mcp.NewTool("regular-tool-2",
			mcp.WithDescription("Second regular tool"),
		)
		server.AddTool(regularTool2, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})

		// Add task-augmented tools
		taskTool1 := mcp.NewTool("task-tool-1",
			mcp.WithDescription("First task tool"),
			mcp.WithTaskSupport(mcp.TaskSupportRequired),
		)
		err := server.AddTaskTool(taskTool1, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})
		require.NoError(t, err)

		taskTool2 := mcp.NewTool("task-tool-2",
			mcp.WithDescription("Second task tool"),
			mcp.WithTaskSupport(mcp.TaskSupportOptional),
		)
		err = server.AddTaskTool(taskTool2, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})
		require.NoError(t, err)

		// List tools
		response := server.HandleMessage(context.Background(), []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "tools/list"
		}`))

		resp, ok := response.(mcp.JSONRPCResponse)
		require.True(t, ok, "Expected JSONRPCResponse, got %T", response)

		result, ok := resp.Result.(mcp.ListToolsResult)
		require.True(t, ok, "Expected ListToolsResult, got %T", resp.Result)

		// Should have all 4 tools
		assert.Len(t, result.Tools, 4)

		// Verify tools are sorted by name
		toolNames := make([]string, len(result.Tools))
		for i, tool := range result.Tools {
			toolNames[i] = tool.Name
		}
		assert.Equal(t, []string{"regular-tool-1", "regular-tool-2", "task-tool-1", "task-tool-2"}, toolNames)

		// Verify execution fields
		for _, tool := range result.Tools {
			switch tool.Name {
			case "regular-tool-1", "regular-tool-2":
				assert.Nil(t, tool.Execution, "Regular tool %s should not have execution field", tool.Name)
			case "task-tool-1":
				require.NotNil(t, tool.Execution, "Task tool %s should have execution field", tool.Name)
				assert.Equal(t, mcp.TaskSupportRequired, tool.Execution.TaskSupport)
			case "task-tool-2":
				require.NotNil(t, tool.Execution, "Task tool %s should have execution field", tool.Name)
				assert.Equal(t, mcp.TaskSupportOptional, tool.Execution.TaskSupport)
			}
		}
	})

	t.Run("Empty tools list", func(t *testing.T) {
		server := NewMCPServer(
			"test-server",
			"1.0.0",
			WithToolCapabilities(true),
			WithTaskCapabilities(true, true, true),
		)

		// List tools without adding any
		response := server.HandleMessage(context.Background(), []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "tools/list"
		}`))

		resp, ok := response.(mcp.JSONRPCResponse)
		require.True(t, ok, "Expected JSONRPCResponse, got %T", response)

		result, ok := resp.Result.(mcp.ListToolsResult)
		require.True(t, ok, "Expected ListToolsResult, got %T", resp.Result)

		assert.Len(t, result.Tools, 0)
	})
}

// TestTaskAugmentedToolCall_TracerBullet is a comprehensive end-to-end integration test
// that validates the complete flow of task-augmented tool calls. This "tracer bullet" test
// ensures that:
// 1. A task-augmented tool can be registered with the server
// 2. Calling the tool with task params returns CreateTaskResult immediately
// 3. The tool executes asynchronously in the background
// 4. Task status can be polled via tasks/get
// 5. Task result can be retrieved via tasks/result
// 6. The result includes proper CallToolResult content and related-task metadata
func TestTaskAugmentedToolCall_TracerBullet(t *testing.T) {
	// 1. Create server with full task capabilities
	server := NewMCPServer(
		"test-server",
		"1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	ctx := context.Background()

	// Initialize the server
	initResponse := server.HandleMessage(ctx, []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "initialize",
		"params": {
			"protocolVersion": "2025-11-05",
			"capabilities": {},
			"clientInfo": {
				"name": "test-client",
				"version": "1.0.0"
			}
		}
	}`))

	resp, ok := initResponse.(mcp.JSONRPCResponse)
	require.True(t, ok, "Expected JSONRPCResponse, got %T", initResponse)
	require.NotNil(t, resp.Result, "Initialize should succeed")

	// 2. Register a task-augmented tool that simulates a long-running operation
	tool := mcp.NewTool(
		"slow_operation",
		mcp.WithDescription("A slow operation that processes data asynchronously"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
		mcp.WithString("input", mcp.Required()),
	)

	// Track execution state
	executionStarted := make(chan struct{})
	executionCompleted := make(chan struct{})

	err := server.AddTaskTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		close(executionStarted)

		// Simulate slow processing
		input := req.GetString("input", "")

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
			// Processing complete
		}

		close(executionCompleted)

		// Return the actual tool result
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Processed: %s", input)),
			},
			IsError: false,
		}, nil
	})
	require.NoError(t, err, "AddTaskTool should succeed")

	// 3. Call the tool with task augmentation
	callResponse := server.HandleMessage(ctx, []byte(`{
		"jsonrpc": "2.0",
		"id": 2,
		"method": "tools/call",
		"params": {
			"name": "slow_operation",
			"arguments": {
				"input": "test data"
			},
			"task": {
				"ttl": 300
			}
		}
	}`))

	callResp, ok := callResponse.(mcp.JSONRPCResponse)
	require.True(t, ok, "Expected JSONRPCResponse, got %T", callResponse)

	// 4. Verify CreateTaskResult is returned immediately (before execution completes)
	select {
	case <-executionCompleted:
		t.Fatal("Tool should not have completed yet - CreateTaskResult should be returned immediately")
	default:
		// Good - execution hasn't finished yet
	}

	// Extract the task ID from the response
	callResult, ok := callResp.Result.(mcp.CallToolResult)
	require.True(t, ok, "Expected CallToolResult, got %T", callResp.Result)
	require.NotNil(t, callResult.Meta, "Meta should contain task info")
	require.NotNil(t, callResult.Meta.AdditionalFields, "AdditionalFields should contain task")

	// Task is stored as mcp.Task struct in AdditionalFields
	task, ok := callResult.Meta.AdditionalFields["task"].(mcp.Task)
	require.True(t, ok, "Task data should be present, got: %T", callResult.Meta.AdditionalFields["task"])

	taskID := task.TaskId
	require.NotEmpty(t, taskID, "Task ID should not be empty")

	// Verify task status is "working"
	assert.Equal(t, mcp.TaskStatusWorking, task.Status, "Initial task status should be 'working'")

	// 5. Poll task status via tasks/get
	// Wait for execution to start
	select {
	case <-executionStarted:
		// Good - execution has started
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Tool execution should have started")
	}

	// Get task while it's still working
	getResponse := server.HandleMessage(ctx, []byte(fmt.Sprintf(`{
		"jsonrpc": "2.0",
		"id": 3,
		"method": "tasks/get",
		"params": {
			"taskId": "%s"
		}
	}`, taskID)))

	getResp, ok := getResponse.(mcp.JSONRPCResponse)
	require.True(t, ok, "Expected JSONRPCResponse, got %T", getResponse)

	getResult, ok := getResp.Result.(mcp.GetTaskResult)
	require.True(t, ok, "Expected GetTaskResult, got %T", getResp.Result)
	assert.Equal(t, taskID, getResult.TaskId, "Task ID should match")
	// Status could be "working" or "completed" depending on timing

	// 6. Wait for execution to complete
	select {
	case <-executionCompleted:
		// Good - execution completed
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Tool execution should have completed")
	}

	// 7. Retrieve the task result via tasks/result
	resultResponse := server.HandleMessage(ctx, []byte(fmt.Sprintf(`{
		"jsonrpc": "2.0",
		"id": 4,
		"method": "tasks/result",
		"params": {
			"taskId": "%s"
		}
	}`, taskID)))

	resultResp, ok := resultResponse.(mcp.JSONRPCResponse)
	require.True(t, ok, "Expected JSONRPCResponse, got %T", resultResponse)

	taskResultResult, ok := resultResp.Result.(mcp.TaskResultResult)
	require.True(t, ok, "Expected TaskResultResult, got %T", resultResp.Result)

	// 8. Verify the result contains the actual CallToolResult content
	require.NotEmpty(t, taskResultResult.Content, "Result should contain content")

	textContent, ok := taskResultResult.Content[0].(mcp.TextContent)
	require.True(t, ok, "Expected TextContent, got %T", taskResultResult.Content[0])
	assert.Equal(t, "Processed: test data", textContent.Text, "Content should match expected output")
	assert.False(t, taskResultResult.IsError, "Result should not be an error")

	// 9. Verify related-task metadata is present
	require.NotNil(t, taskResultResult.Meta, "Meta should be present")
	require.NotNil(t, taskResultResult.Meta.AdditionalFields, "AdditionalFields should be present")

	relatedTask, ok := taskResultResult.Meta.AdditionalFields["io.modelcontextprotocol/related-task"].(map[string]any)
	require.True(t, ok, "Related task metadata should be present")

	relatedTaskID, ok := relatedTask["taskId"].(string)
	require.True(t, ok, "Related task ID should be present")
	assert.Equal(t, taskID, relatedTaskID, "Related task ID should match original task ID")

	// 10. Verify final task status via tasks/get
	finalGetResponse := server.HandleMessage(ctx, []byte(fmt.Sprintf(`{
		"jsonrpc": "2.0",
		"id": 5,
		"method": "tasks/get",
		"params": {
			"taskId": "%s"
		}
	}`, taskID)))

	finalGetResp, ok := finalGetResponse.(mcp.JSONRPCResponse)
	require.True(t, ok, "Expected JSONRPCResponse, got %T", finalGetResponse)

	finalGetResult, ok := finalGetResp.Result.(mcp.GetTaskResult)
	require.True(t, ok, "Expected GetTaskResult, got %T", finalGetResp.Result)
	assert.Equal(t, mcp.TaskStatusCompleted, finalGetResult.Status, "Final task status should be 'completed'")
}

// TestTaskAugmentedToolCall_ErrorHandling tests error scenarios in the tracer bullet flow
func TestTaskAugmentedToolCall_ErrorHandling(t *testing.T) {
	t.Run("tool handler error marks task as failed", func(t *testing.T) {
		server := NewMCPServer(
			"test-server",
			"1.0.0",
			WithTaskCapabilities(true, true, true),
		)

		ctx := context.Background()

		// Initialize
		initResponse := server.HandleMessage(ctx, []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "initialize",
			"params": {
				"protocolVersion": "2025-11-05",
				"capabilities": {},
				"clientInfo": {"name": "test", "version": "1.0.0"}
			}
		}`))
		require.IsType(t, mcp.JSONRPCResponse{}, initResponse)

		// Register a task tool that fails
		tool := mcp.NewTool(
			"failing_operation",
			mcp.WithTaskSupport(mcp.TaskSupportRequired),
		)

		err := server.AddTaskTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return nil, fmt.Errorf("simulated failure")
		})
		require.NoError(t, err)

		// Call the tool
		callResponse := server.HandleMessage(ctx, []byte(`{
			"jsonrpc": "2.0",
			"id": 2,
			"method": "tools/call",
			"params": {
				"name": "failing_operation",
				"task": {}
			}
		}`))

		callResp := callResponse.(mcp.JSONRPCResponse)

		// Extract task ID
		callResult := callResp.Result.(mcp.CallToolResult)
		task := callResult.Meta.AdditionalFields["task"].(mcp.Task)
		taskID := task.TaskId

		// Wait a bit for async execution
		time.Sleep(50 * time.Millisecond)

		// Check task status - should be failed
		getResponse := server.HandleMessage(ctx, []byte(fmt.Sprintf(`{
			"jsonrpc": "2.0",
			"id": 3,
			"method": "tasks/get",
			"params": {"taskId": "%s"}
		}`, taskID)))

		getResp := getResponse.(mcp.JSONRPCResponse)
		getResult := getResp.Result.(mcp.GetTaskResult)
		assert.Equal(t, mcp.TaskStatusFailed, getResult.Status)
		assert.NotEmpty(t, getResult.StatusMessage)
		assert.Contains(t, getResult.StatusMessage, "simulated failure")
	})

	t.Run("calling tool without task params when required returns error", func(t *testing.T) {
		server := NewMCPServer(
			"test-server",
			"1.0.0",
			WithTaskCapabilities(true, true, true),
		)

		ctx := context.Background()

		// Initialize
		server.HandleMessage(ctx, []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "initialize",
			"params": {
				"protocolVersion": "2025-11-05",
				"capabilities": {},
				"clientInfo": {"name": "test", "version": "1.0.0"}
			}
		}`))

		// Register a task tool with TaskSupportRequired
		tool := mcp.NewTool(
			"required_task_tool",
			mcp.WithTaskSupport(mcp.TaskSupportRequired),
		)

		err := server.AddTaskTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})
		require.NoError(t, err)

		// Call the tool WITHOUT task params
		callResponse := server.HandleMessage(ctx, []byte(`{
			"jsonrpc": "2.0",
			"id": 2,
			"method": "tools/call",
			"params": {
				"name": "required_task_tool"
			}
		}`))

		// Current implementation: returns INVALID_PARAMS because regular tool not found
		// (task tools are only in s.taskTools map, not s.tools map)
		// After TAS-16 is implemented: should validate task support mode and return a more specific error
		callErrResp, ok := callResponse.(mcp.JSONRPCError)
		require.True(t, ok, "Expected error response")
		assert.Equal(t, mcp.INVALID_PARAMS, callErrResp.Error.Code)
	})

	t.Run("task cancellation propagates to handler", func(t *testing.T) {
		server := NewMCPServer(
			"test-server",
			"1.0.0",
			WithTaskCapabilities(true, true, true),
		)

		ctx := context.Background()

		// Initialize
		server.HandleMessage(ctx, []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "initialize",
			"params": {
				"protocolVersion": "2025-11-05",
				"capabilities": {},
				"clientInfo": {"name": "test", "version": "1.0.0"}
			}
		}`))

		// Register a long-running task tool
		handlerStarted := make(chan struct{})
		contextCancelled := make(chan struct{})

		tool := mcp.NewTool(
			"long_operation",
			mcp.WithTaskSupport(mcp.TaskSupportRequired),
		)

		err := server.AddTaskTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			close(handlerStarted)

			// Wait for context cancellation
			<-ctx.Done()
			close(contextCancelled)

			return nil, ctx.Err()
		})
		require.NoError(t, err)

		// Call the tool
		callResponse := server.HandleMessage(ctx, []byte(`{
			"jsonrpc": "2.0",
			"id": 2,
			"method": "tools/call",
			"params": {
				"name": "long_operation",
				"task": {}
			}
		}`))

		callResp := callResponse.(mcp.JSONRPCResponse)
		callResult := callResp.Result.(mcp.CallToolResult)
		task := callResult.Meta.AdditionalFields["task"].(mcp.Task)
		taskID := task.TaskId

		// Wait for handler to start
		<-handlerStarted

		// Cancel the task
		cancelResponse := server.HandleMessage(ctx, []byte(fmt.Sprintf(`{
			"jsonrpc": "2.0",
			"id": 3,
			"method": "tasks/cancel",
			"params": {"taskId": "%s", "reason": "test cancellation"}
		}`, taskID)))

		_, ok := cancelResponse.(mcp.JSONRPCResponse)
		require.True(t, ok, "Expected successful cancel response")

		// Wait for context cancellation
		select {
		case <-contextCancelled:
			// Good - context was cancelled
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Context should have been cancelled")
		}

		// Verify task status is cancelled
		time.Sleep(10 * time.Millisecond) // Give completeTask time to run

		getResponse := server.HandleMessage(ctx, []byte(fmt.Sprintf(`{
			"jsonrpc": "2.0",
			"id": 4,
			"method": "tasks/get",
			"params": {"taskId": "%s"}
		}`, taskID)))

		getResp := getResponse.(mcp.JSONRPCResponse)
		getResult := getResp.Result.(mcp.GetTaskResult)
		assert.Equal(t, mcp.TaskStatusCancelled, getResult.Status)
	})
}

// TestMCPServer_ValidateTaskSupportRequired tests that tools with TaskSupportRequired
// must be called with task parameters.
func TestMCPServer_ValidateTaskSupportRequired(t *testing.T) {
	t.Run("regular tool with TaskSupportRequired fails without task params", func(t *testing.T) {
		server := NewMCPServer(
			"test-server",
			"1.0.0",
			WithTaskCapabilities(true, true, true),
		)

		ctx := context.Background()

		// Initialize
		server.HandleMessage(ctx, []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "initialize",
			"params": {
				"protocolVersion": "2025-11-05",
				"capabilities": {},
				"clientInfo": {"name": "test", "version": "1.0.0"}
			}
		}`))

		// Register a regular tool but with TaskSupportRequired
		// This is a configuration error that should be caught
		tool := mcp.NewTool(
			"must_use_tasks",
			mcp.WithTaskSupport(mcp.TaskSupportRequired),
		)

		// Note: Using AddTool (not AddTaskTool) to test validation in regular tool path
		server.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent("This should not be called"),
				},
			}, nil
		})

		// Call the tool WITHOUT task params
		callResponse := server.HandleMessage(ctx, []byte(`{
			"jsonrpc": "2.0",
			"id": 2,
			"method": "tools/call",
			"params": {
				"name": "must_use_tasks"
			}
		}`))

		// Should return INVALID_PARAMS error
		callErrResp, ok := callResponse.(mcp.JSONRPCError)
		require.True(t, ok, "Expected error response, got %T", callResponse)
		assert.Equal(t, mcp.INVALID_PARAMS, callErrResp.Error.Code)
		assert.Contains(t, callErrResp.Error.Message, "requires task augmentation")
		assert.Contains(t, callErrResp.Error.Message, "must_use_tasks")
	})

	t.Run("regular tool with TaskSupportRequired succeeds with task params", func(t *testing.T) {
		server := NewMCPServer(
			"test-server",
			"1.0.0",
			WithTaskCapabilities(true, true, true),
		)

		ctx := context.Background()

		// Initialize
		server.HandleMessage(ctx, []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "initialize",
			"params": {
				"protocolVersion": "2025-11-05",
				"capabilities": {},
				"clientInfo": {"name": "test", "version": "1.0.0"}
			}
		}`))

		// Register as both regular tool and task tool
		tool := mcp.NewTool(
			"hybrid_tool",
			mcp.WithTaskSupport(mcp.TaskSupportRequired),
		)

		// Register as regular tool
		server.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent("Should not be called without task params"),
				},
			}, nil
		})

		// Also register as task tool
		err := server.AddTaskTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent("Task tool executed"),
				},
			}, nil
		})
		require.NoError(t, err)

		// Call the tool WITH task params - should route to task handler
		callResponse := server.HandleMessage(ctx, []byte(`{
			"jsonrpc": "2.0",
			"id": 2,
			"method": "tools/call",
			"params": {
				"name": "hybrid_tool",
				"task": {}
			}
		}`))

		// Should succeed and return task creation result
		callResp, ok := callResponse.(mcp.JSONRPCResponse)
		require.True(t, ok, "Expected success response, got %T", callResponse)

		callResult := callResp.Result.(mcp.CallToolResult)
		task, ok := callResult.Meta.AdditionalFields["task"].(mcp.Task)
		require.True(t, ok, "Expected task in meta")
		assert.NotEmpty(t, task.TaskId)
		assert.Equal(t, mcp.TaskStatusWorking, task.Status)
	})

	t.Run("tool without TaskSupport field works normally", func(t *testing.T) {
		server := NewMCPServer(
			"test-server",
			"1.0.0",
		)

		ctx := context.Background()

		// Initialize
		server.HandleMessage(ctx, []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "initialize",
			"params": {
				"protocolVersion": "2025-11-05",
				"capabilities": {},
				"clientInfo": {"name": "test", "version": "1.0.0"}
			}
		}`))

		// Register a normal tool without any task support configured
		tool := mcp.NewTool("normal_tool")

		server.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent("Normal tool executed"),
				},
			}, nil
		})

		// Call the tool normally (without task params)
		callResponse := server.HandleMessage(ctx, []byte(`{
			"jsonrpc": "2.0",
			"id": 2,
			"method": "tools/call",
			"params": {
				"name": "normal_tool"
			}
		}`))

		// Should succeed
		callResp, ok := callResponse.(mcp.JSONRPCResponse)
		require.True(t, ok, "Expected success response, got %T", callResponse)

		callResult := callResp.Result.(mcp.CallToolResult)
		require.Len(t, callResult.Content, 1)
		textContent := callResult.Content[0].(mcp.TextContent)
		assert.Equal(t, "Normal tool executed", textContent.Text)
	})

	t.Run("tool with TaskSupportForbidden works normally", func(t *testing.T) {
		server := NewMCPServer(
			"test-server",
			"1.0.0",
		)

		ctx := context.Background()

		// Initialize
		server.HandleMessage(ctx, []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "initialize",
			"params": {
				"protocolVersion": "2025-11-05",
				"capabilities": {},
				"clientInfo": {"name": "test", "version": "1.0.0"}
			}
		}`))

		// Register a tool with explicit TaskSupportForbidden
		tool := mcp.NewTool(
			"forbidden_tool",
			mcp.WithTaskSupport(mcp.TaskSupportForbidden),
		)

		server.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent("Forbidden tool executed"),
				},
			}, nil
		})

		// Call the tool normally (without task params)
		callResponse := server.HandleMessage(ctx, []byte(`{
			"jsonrpc": "2.0",
			"id": 2,
			"method": "tools/call",
			"params": {
				"name": "forbidden_tool"
			}
		}`))

		// Should succeed
		callResp, ok := callResponse.(mcp.JSONRPCResponse)
		require.True(t, ok, "Expected success response, got %T", callResponse)

		callResult := callResp.Result.(mcp.CallToolResult)
		require.Len(t, callResult.Content, 1)
		textContent := callResult.Content[0].(mcp.TextContent)
		assert.Equal(t, "Forbidden tool executed", textContent.Text)
	})
}
