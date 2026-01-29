package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskLifecycleHooks_OnTaskCreated(t *testing.T) {
	hooks := &Hooks{}
	var createdTasks []mcp.Task
	var mu sync.Mutex

	hooks.AddOnTaskCreated(func(ctx context.Context, task mcp.Task) {
		mu.Lock()
		defer mu.Unlock()
		createdTasks = append(createdTasks, task)
	})

	server := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
		WithHooks(hooks),
	)

	// Add a task-augmented tool
	tool := mcp.NewTool("test_tool",
		mcp.WithDescription("Test tool"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)

	err := server.AddTaskTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		time.Sleep(50 * time.Millisecond)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "done",
				},
			},
		}, nil
	})
	require.NoError(t, err)

	// Call the task tool
	request := mcp.CallToolRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodToolsCall),
		},
		Params: mcp.CallToolParams{
			Name: "test_tool",
			Task: &mcp.TaskParams{},
		},
	}
	ctx := context.Background()

	result, errResp := server.handleToolCall(ctx, "test-1", request)
	require.Nil(t, errResp)
	require.NotNil(t, result)

	// Verify OnTaskCreated was called
	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(createdTasks) == 1
	}, time.Second, 10*time.Millisecond)

	mu.Lock()
	createdTask := createdTasks[0]
	mu.Unlock()

	assert.Equal(t, mcp.TaskStatusWorking, createdTask.Status)
	assert.NotEmpty(t, createdTask.TaskId)
	assert.NotEmpty(t, createdTask.CreatedAt)
}

func TestTaskLifecycleHooks_OnTaskStatusChanged(t *testing.T) {
	hooks := &Hooks{}
	var statusChanges []struct {
		task      mcp.Task
		oldStatus mcp.TaskStatus
	}
	var mu sync.Mutex

	hooks.AddOnTaskStatusChanged(func(ctx context.Context, task mcp.Task, oldStatus mcp.TaskStatus) {
		mu.Lock()
		defer mu.Unlock()
		statusChanges = append(statusChanges, struct {
			task      mcp.Task
			oldStatus mcp.TaskStatus
		}{task, oldStatus})
	})

	server := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
		WithHooks(hooks),
	)

	// Add a task-augmented tool
	tool := mcp.NewTool("test_tool",
		mcp.WithDescription("Test tool"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)

	err := server.AddTaskTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		time.Sleep(50 * time.Millisecond)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "done",
				},
			},
		}, nil
	})
	require.NoError(t, err)

	// Call the task tool
	request := mcp.CallToolRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodToolsCall),
		},
		Params: mcp.CallToolParams{
			Name: "test_tool",
			Task: &mcp.TaskParams{},
		},
	}
	ctx := context.Background()

	result, errResp := server.handleToolCall(ctx, "test-1", request)
	require.Nil(t, errResp)
	require.NotNil(t, result)

	// Wait for task to complete
	time.Sleep(200 * time.Millisecond)

	// Verify OnTaskStatusChanged was called once (working → completed)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, statusChanges, 1, "Expected one status change (working → completed)")

	change := statusChanges[0]
	assert.Equal(t, mcp.TaskStatusWorking, change.oldStatus)
	assert.Equal(t, mcp.TaskStatusCompleted, change.task.Status)
}

func TestTaskLifecycleHooks_OnTaskCompleted_Success(t *testing.T) {
	hooks := &Hooks{}
	var completedTasks []struct {
		task mcp.Task
		err  error
	}
	var mu sync.Mutex

	hooks.AddOnTaskCompleted(func(ctx context.Context, task mcp.Task, err error) {
		mu.Lock()
		defer mu.Unlock()
		completedTasks = append(completedTasks, struct {
			task mcp.Task
			err  error
		}{task, err})
	})

	server := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
		WithHooks(hooks),
	)

	// Add a task-augmented tool
	tool := mcp.NewTool("test_tool",
		mcp.WithDescription("Test tool"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)

	err := server.AddTaskTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		time.Sleep(50 * time.Millisecond)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "done",
				},
			},
		}, nil
	})
	require.NoError(t, err)

	// Call the task tool
	request := mcp.CallToolRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodToolsCall),
		},
		Params: mcp.CallToolParams{
			Name: "test_tool",
			Task: &mcp.TaskParams{},
		},
	}
	ctx := context.Background()

	result, errResp := server.handleToolCall(ctx, "test-1", request)
	require.Nil(t, errResp)
	require.NotNil(t, result)

	// Wait for task to complete
	time.Sleep(200 * time.Millisecond)

	// Verify OnTaskCompleted was called
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, completedTasks, 1)

	completed := completedTasks[0]
	assert.Equal(t, mcp.TaskStatusCompleted, completed.task.Status)
	assert.Nil(t, completed.err, "Successful task should have nil error")
}

func TestTaskLifecycleHooks_OnTaskCompleted_Failure(t *testing.T) {
	hooks := &Hooks{}
	var completedTasks []struct {
		task mcp.Task
		err  error
	}
	var mu sync.Mutex

	hooks.AddOnTaskCompleted(func(ctx context.Context, task mcp.Task, err error) {
		mu.Lock()
		defer mu.Unlock()
		completedTasks = append(completedTasks, struct {
			task mcp.Task
			err  error
		}{task, err})
	})

	server := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
		WithHooks(hooks),
	)

	// Add a task-augmented tool that fails
	tool := mcp.NewTool("failing_tool",
		mcp.WithDescription("Tool that fails"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)

	err := server.AddTaskTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		time.Sleep(50 * time.Millisecond)
		return nil, assert.AnError
	})
	require.NoError(t, err)

	// Call the task tool
	request := mcp.CallToolRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodToolsCall),
		},
		Params: mcp.CallToolParams{
			Name: "failing_tool",
			Task: &mcp.TaskParams{},
		},
	}
	ctx := context.Background()

	result, errResp := server.handleToolCall(ctx, "test-1", request)
	require.Nil(t, errResp)
	require.NotNil(t, result)

	// Wait for task to complete
	time.Sleep(200 * time.Millisecond)

	// Verify OnTaskCompleted was called with error
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, completedTasks, 1)

	completed := completedTasks[0]
	assert.Equal(t, mcp.TaskStatusFailed, completed.task.Status)
	assert.NotNil(t, completed.err, "Failed task should have non-nil error")
	assert.Equal(t, assert.AnError, completed.err)
}

func TestTaskLifecycleHooks_OnTaskCompleted_Cancelled(t *testing.T) {
	hooks := &Hooks{}
	var completedTasks []struct {
		task mcp.Task
		err  error
	}
	var mu sync.Mutex

	hooks.AddOnTaskCompleted(func(ctx context.Context, task mcp.Task, err error) {
		mu.Lock()
		defer mu.Unlock()
		completedTasks = append(completedTasks, struct {
			task mcp.Task
			err  error
		}{task, err})
	})

	server := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
		WithHooks(hooks),
	)

	// Add a long-running task tool
	tool := mcp.NewTool("slow_tool",
		mcp.WithDescription("Slow tool"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)

	err := server.AddTaskTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		time.Sleep(5 * time.Second) // Long enough to cancel
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "done",
				},
			},
		}, nil
	})
	require.NoError(t, err)

	// Call the task tool
	request := mcp.CallToolRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodToolsCall),
		},
		Params: mcp.CallToolParams{
			Name: "slow_tool",
			Task: &mcp.TaskParams{},
		},
	}
	ctx := context.Background()

	result, errResp := server.handleToolCall(ctx, "test-1", request)
	require.Nil(t, errResp)
	require.NotNil(t, result)

	// Extract task ID from result
	meta := result.Meta
	require.NotNil(t, meta)
	taskData, ok := meta.AdditionalFields["task"]
	require.True(t, ok)
	task, ok := taskData.(mcp.Task)
	require.True(t, ok)
	taskID := task.TaskId

	// Cancel the task
	time.Sleep(100 * time.Millisecond) // Wait for task to start
	cancelErr := server.cancelTask(ctx, taskID)
	require.NoError(t, cancelErr)

	// Wait for hook to be called
	time.Sleep(100 * time.Millisecond)

	// Verify OnTaskCompleted was called for cancelled task
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, completedTasks, 1)

	completed := completedTasks[0]
	assert.Equal(t, mcp.TaskStatusCancelled, completed.task.Status)
	assert.Nil(t, completed.err, "Cancelled task should have nil error")
}

func TestTaskLifecycleHooks_AllHooksCalled(t *testing.T) {
	hooks := &Hooks{}
	var events []string
	var mu sync.Mutex

	hooks.AddOnTaskCreated(func(ctx context.Context, task mcp.Task) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, "created:"+task.TaskId)
	})

	hooks.AddOnTaskStatusChanged(func(ctx context.Context, task mcp.Task, oldStatus mcp.TaskStatus) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, "status_changed:"+string(oldStatus)+"→"+string(task.Status))
	})

	hooks.AddOnTaskCompleted(func(ctx context.Context, task mcp.Task, err error) {
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			events = append(events, "completed:failed")
		} else {
			events = append(events, "completed:success")
		}
	})

	server := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
		WithHooks(hooks),
	)

	// Add a task-augmented tool
	tool := mcp.NewTool("test_tool",
		mcp.WithDescription("Test tool"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)

	err := server.AddTaskTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		time.Sleep(50 * time.Millisecond)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "done",
				},
			},
		}, nil
	})
	require.NoError(t, err)

	// Call the task tool
	request := mcp.CallToolRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodToolsCall),
		},
		Params: mcp.CallToolParams{
			Name: "test_tool",
			Task: &mcp.TaskParams{},
		},
	}
	ctx := context.Background()

	result, errResp := server.handleToolCall(ctx, "test-1", request)
	require.Nil(t, errResp)
	require.NotNil(t, result)

	// Wait for task to complete
	time.Sleep(200 * time.Millisecond)

	// Verify all hooks were called in correct order
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, events, 3, "Expected 3 events: created, status_changed, completed")

	assert.Contains(t, events[0], "created:")
	assert.Equal(t, "status_changed:working→completed", events[1])
	assert.Equal(t, "completed:success", events[2])
}

func TestTaskLifecycleHooks_MultipleHooksPerType(t *testing.T) {
	hooks := &Hooks{}
	var hook1Called, hook2Called bool
	var mu sync.Mutex

	hooks.AddOnTaskCreated(func(ctx context.Context, task mcp.Task) {
		mu.Lock()
		defer mu.Unlock()
		hook1Called = true
	})

	hooks.AddOnTaskCreated(func(ctx context.Context, task mcp.Task) {
		mu.Lock()
		defer mu.Unlock()
		hook2Called = true
	})

	server := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
		WithHooks(hooks),
	)

	// Add a task-augmented tool
	tool := mcp.NewTool("test_tool",
		mcp.WithDescription("Test tool"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)

	err := server.AddTaskTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "done",
				},
			},
		}, nil
	})
	require.NoError(t, err)

	// Call the task tool
	request := mcp.CallToolRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodToolsCall),
		},
		Params: mcp.CallToolParams{
			Name: "test_tool",
			Task: &mcp.TaskParams{},
		},
	}
	ctx := context.Background()

	result, errResp := server.handleToolCall(ctx, "test-1", request)
	require.Nil(t, errResp)
	require.NotNil(t, result)

	// Wait for hooks to be called
	time.Sleep(100 * time.Millisecond)

	// Verify both hooks were called
	mu.Lock()
	defer mu.Unlock()
	assert.True(t, hook1Called, "First hook should be called")
	assert.True(t, hook2Called, "Second hook should be called")
}

func TestTaskLifecycleHooks_NoHooksRegistered(t *testing.T) {
	// Test that server works correctly with no hooks registered
	server := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	// Add a task-augmented tool
	tool := mcp.NewTool("test_tool",
		mcp.WithDescription("Test tool"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)

	err := server.AddTaskTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "done",
				},
			},
		}, nil
	})
	require.NoError(t, err)

	// Call the task tool - should not panic
	request := mcp.CallToolRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodToolsCall),
		},
		Params: mcp.CallToolParams{
			Name: "test_tool",
			Task: &mcp.TaskParams{},
		},
	}
	ctx := context.Background()

	result, errResp := server.handleToolCall(ctx, "test-1", request)
	require.Nil(t, errResp)
	require.NotNil(t, result)

	// Wait for task to complete
	time.Sleep(200 * time.Millisecond)

	// If we got here without panic, test passes
}

func TestTaskLifecycleHooks_HookContextIndependent(t *testing.T) {
	// Test that hooks receive background context and aren't affected by original context cancellation
	hooks := &Hooks{}
	var hookCalled bool
	var mu sync.Mutex

	hooks.AddOnTaskCompleted(func(ctx context.Context, task mcp.Task, err error) {
		mu.Lock()
		defer mu.Unlock()
		hookCalled = true
		// Verify we received a background context (not cancelled)
		assert.NoError(t, ctx.Err(), "Hook context should not be cancelled")
	})

	server := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
		WithHooks(hooks),
	)

	// Add a task-augmented tool
	tool := mcp.NewTool("test_tool",
		mcp.WithDescription("Test tool"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)

	err := server.AddTaskTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "done",
				},
			},
		}, nil
	})
	require.NoError(t, err)

	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Call the task tool
	request := mcp.CallToolRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodToolsCall),
		},
		Params: mcp.CallToolParams{
			Name: "test_tool",
			Task: &mcp.TaskParams{},
		},
	}
	result, errResp := server.handleToolCall(ctx, "test-1", request)
	require.Nil(t, errResp)
	require.NotNil(t, result)

	// Cancel the context immediately
	cancel()

	// Wait for task to complete and hook to be called
	time.Sleep(200 * time.Millisecond)

	// Verify hook was called despite context cancellation
	mu.Lock()
	defer mu.Unlock()
	assert.True(t, hookCalled, "Hook should be called even if original context cancelled")
}
