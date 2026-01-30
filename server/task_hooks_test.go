package server

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskHooks_TaskCreated(t *testing.T) {
	var mu sync.Mutex
	var capturedMetrics []TaskMetrics

	hooks := &TaskHooks{}
	hooks.AddOnTaskCreated(func(ctx context.Context, metrics TaskMetrics) {
		mu.Lock()
		defer mu.Unlock()
		capturedMetrics = append(capturedMetrics, metrics)
	})

	server := NewMCPServer("test-server", "1.0.0", WithTaskHooks(hooks))

	// Create a task
	ctx := context.Background()
	_, err := server.createTask(ctx, "test-task-1", "test-tool", nil, nil)
	require.NoError(t, err)

	// Give hook time to execute
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, capturedMetrics, 1)
	assert.Equal(t, "test-task-1", capturedMetrics[0].TaskID)
	assert.Equal(t, "test-tool", capturedMetrics[0].ToolName)
	assert.Equal(t, mcp.TaskStatusWorking, capturedMetrics[0].Status)
	assert.NotZero(t, capturedMetrics[0].CreatedAt)
}

func TestTaskHooks_TaskCompleted(t *testing.T) {
	var mu sync.Mutex
	var completedMetrics []TaskMetrics

	hooks := &TaskHooks{}
	hooks.AddOnTaskCompleted(func(ctx context.Context, metrics TaskMetrics) {
		mu.Lock()
		defer mu.Unlock()
		completedMetrics = append(completedMetrics, metrics)
	})

	server := NewMCPServer("test-server", "1.0.0", WithTaskHooks(hooks))

	// Create and complete a task
	ctx := context.Background()
	entry, err := server.createTask(ctx, "test-task-2", "test-tool", nil, nil)
	require.NoError(t, err)

	// Complete the task successfully
	server.completeTask(entry, "result", nil)

	// Give hook time to execute
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, completedMetrics, 1)
	assert.Equal(t, "test-task-2", completedMetrics[0].TaskID)
	assert.Equal(t, "test-tool", completedMetrics[0].ToolName)
	assert.Equal(t, mcp.TaskStatusCompleted, completedMetrics[0].Status)
	assert.NotNil(t, completedMetrics[0].CompletedAt)
	assert.Greater(t, completedMetrics[0].Duration, time.Duration(0))
	assert.Nil(t, completedMetrics[0].Error)
}

func TestTaskHooks_TaskFailed(t *testing.T) {
	var mu sync.Mutex
	var failedMetrics []TaskMetrics

	hooks := &TaskHooks{}
	hooks.AddOnTaskFailed(func(ctx context.Context, metrics TaskMetrics) {
		mu.Lock()
		defer mu.Unlock()
		failedMetrics = append(failedMetrics, metrics)
	})

	server := NewMCPServer("test-server", "1.0.0", WithTaskHooks(hooks))

	// Create and fail a task
	ctx := context.Background()
	entry, err := server.createTask(ctx, "test-task-3", "test-tool", nil, nil)
	require.NoError(t, err)

	// Fail the task
	testErr := errors.New("task failed")
	server.completeTask(entry, nil, testErr)

	// Give hook time to execute
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, failedMetrics, 1)
	assert.Equal(t, "test-task-3", failedMetrics[0].TaskID)
	assert.Equal(t, "test-tool", failedMetrics[0].ToolName)
	assert.Equal(t, mcp.TaskStatusFailed, failedMetrics[0].Status)
	assert.Equal(t, "task failed", failedMetrics[0].StatusMessage)
	assert.NotNil(t, failedMetrics[0].CompletedAt)
	assert.Greater(t, failedMetrics[0].Duration, time.Duration(0))
	assert.Equal(t, testErr, failedMetrics[0].Error)
}

func TestTaskHooks_TaskCancelled(t *testing.T) {
	var mu sync.Mutex
	var cancelledMetrics []TaskMetrics

	hooks := &TaskHooks{}
	hooks.AddOnTaskCancelled(func(ctx context.Context, metrics TaskMetrics) {
		mu.Lock()
		defer mu.Unlock()
		cancelledMetrics = append(cancelledMetrics, metrics)
	})

	server := NewMCPServer("test-server", "1.0.0", WithTaskHooks(hooks))

	// Create a task
	ctx := context.Background()
	entry, err := server.createTask(ctx, "test-task-4", "test-tool", nil, nil)
	require.NoError(t, err)

	// Add cancel function
	cancelCtx, cancel := context.WithCancel(ctx)
	server.tasksMu.Lock()
	entry.cancelFunc = cancel
	server.tasksMu.Unlock()

	// Cancel the task
	err = server.cancelTask(cancelCtx, "test-task-4")
	require.NoError(t, err)

	// Give hook time to execute
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, cancelledMetrics, 1)
	assert.Equal(t, "test-task-4", cancelledMetrics[0].TaskID)
	assert.Equal(t, "test-tool", cancelledMetrics[0].ToolName)
	assert.Equal(t, mcp.TaskStatusCancelled, cancelledMetrics[0].Status)
	assert.NotNil(t, cancelledMetrics[0].CompletedAt)
	assert.Greater(t, cancelledMetrics[0].Duration, time.Duration(0))
}

func TestTaskHooks_TaskStatusChanged(t *testing.T) {
	var mu sync.Mutex
	var statusChanges []TaskMetrics

	hooks := &TaskHooks{}
	hooks.AddOnTaskStatusChanged(func(ctx context.Context, metrics TaskMetrics) {
		mu.Lock()
		defer mu.Unlock()
		statusChanges = append(statusChanges, metrics)
	})

	server := NewMCPServer("test-server", "1.0.0", WithTaskHooks(hooks))

	// Create a task (status change 1: working)
	ctx := context.Background()
	entry, err := server.createTask(ctx, "test-task-5", "test-tool", nil, nil)
	require.NoError(t, err)

	// Give hook time to execute
	time.Sleep(10 * time.Millisecond)

	// Complete the task (status change 2: completed)
	server.completeTask(entry, "result", nil)

	// Give hook time to execute
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Should have 2 status changes: working -> completed
	require.Len(t, statusChanges, 2)
	assert.Equal(t, mcp.TaskStatusWorking, statusChanges[0].Status)
	assert.Equal(t, mcp.TaskStatusCompleted, statusChanges[1].Status)
}

func TestTaskHooks_MultipleHooks(t *testing.T) {
	var mu sync.Mutex
	createdCount := 0
	completedCount := 0
	statusChangeCount := 0

	hooks := &TaskHooks{}
	hooks.AddOnTaskCreated(func(ctx context.Context, metrics TaskMetrics) {
		mu.Lock()
		defer mu.Unlock()
		createdCount++
	})
	hooks.AddOnTaskCompleted(func(ctx context.Context, metrics TaskMetrics) {
		mu.Lock()
		defer mu.Unlock()
		completedCount++
	})
	hooks.AddOnTaskStatusChanged(func(ctx context.Context, metrics TaskMetrics) {
		mu.Lock()
		defer mu.Unlock()
		statusChangeCount++
	})

	server := NewMCPServer("test-server", "1.0.0", WithTaskHooks(hooks))

	// Create and complete a task
	ctx := context.Background()
	entry, err := server.createTask(ctx, "test-task-6", "test-tool", nil, nil)
	require.NoError(t, err)
	server.completeTask(entry, "result", nil)

	// Give hooks time to execute
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	assert.Equal(t, 1, createdCount, "Should call created hook once")
	assert.Equal(t, 1, completedCount, "Should call completed hook once")
	assert.Equal(t, 2, statusChangeCount, "Should call status changed hook twice (working + completed)")
}

func TestTaskHooks_NilHooks(t *testing.T) {
	// Test that nil hooks don't cause panics
	server := NewMCPServer("test-server", "1.0.0")

	ctx := context.Background()
	entry, err := server.createTask(ctx, "test-task-7", "test-tool", nil, nil)
	require.NoError(t, err)

	// These should not panic
	assert.NotPanics(t, func() {
		server.completeTask(entry, "result", nil)
	})
}

func TestTaskHooks_IntegrationWithTaskTool(t *testing.T) {
	var mu sync.Mutex
	var allMetrics []TaskMetrics

	hooks := &TaskHooks{}
	// Capture all events
	hooks.AddOnTaskCreated(func(ctx context.Context, metrics TaskMetrics) {
		mu.Lock()
		defer mu.Unlock()
		allMetrics = append(allMetrics, metrics)
	})
	hooks.AddOnTaskCompleted(func(ctx context.Context, metrics TaskMetrics) {
		mu.Lock()
		defer mu.Unlock()
		allMetrics = append(allMetrics, metrics)
	})

	server := NewMCPServer("test-server", "1.0.0",
		WithTaskHooks(hooks),
		WithTaskCapabilities(true, false, false),
	)

	// Register a task tool
	tool := mcp.Tool{
		Name:        "async-tool",
		Description: "Test async tool",
		Execution: &mcp.ToolExecution{
			TaskSupport: mcp.TaskSupportRequired,
		},
	}
	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
		return &mcp.CreateTaskResult{}, nil
	}
	server.AddTaskTool(tool, handler)

	// Call the tool
	ctx := context.Background()
	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "async-tool",
			Task: &mcp.TaskParams{},
		},
	}

	result, reqErr := server.handleTaskAugmentedToolCall(ctx, "test-id", request)
	require.Nil(t, reqErr)
	require.NotNil(t, result)

	// Wait for async execution to complete
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Should have at least 2 events: created and completed
	assert.GreaterOrEqual(t, len(allMetrics), 2)
	assert.Equal(t, "async-tool", allMetrics[0].ToolName)
}
