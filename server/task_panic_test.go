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

func TestExecuteTaskTool_PanicRecovery(t *testing.T) {
	s := NewMCPServer("test", "1.0.0")

	// Register a task tool that panics
	s.AddTaskTools(ServerTaskTool{
		Tool: mcp.Tool{
			Name:        "panic-tool",
			Description: "A tool that panics",
		},
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
			panic("deliberate panic in task handler")
		},
	})

	// Create a task
	ctx := t.Context()
	taskID := "test-panic-task"
	entry, err := s.createTask(ctx, taskID, "panic-tool", nil, nil)
	require.NoError(t, err)

	// Execute in a goroutine (same as production path)
	taskTool := s.taskTools["panic-tool"]
	request := mcp.CallToolRequest{}
	request.Params.Name = "panic-tool"

	go s.executeTaskTool(ctx, entry, taskTool, request)

	// Wait for the task to complete (it should be marked failed, not crash)
	select {
	case <-entry.done:
		// Task completed without crashing the process
	case <-time.After(5 * time.Second):
		t.Fatal("task did not complete within timeout; panic recovery may have failed")
	}

	// Verify task status
	s.tasksMu.RLock()
	assert.True(t, entry.completed)
	assert.Equal(t, mcp.TaskStatusFailed, entry.task.Status)
	assert.Contains(t, entry.task.StatusMessage, "panic in task tool handler")
	assert.Contains(t, entry.task.StatusMessage, "deliberate panic in task handler")
	s.tasksMu.RUnlock()
}

func TestScheduleTaskCleanup_GoroutinesExitAfterTTL(t *testing.T) {
	s := NewMCPServer("test", "1.0.0")

	const numTasks = 10
	var wg sync.WaitGroup

	for i := range numTasks {
		taskID := fmt.Sprintf("leak-test-%d", i)

		s.tasksMu.Lock()
		s.tasks[taskID] = &taskEntry{
			task: mcp.NewTask(taskID),
			done: make(chan struct{}),
		}
		s.tasksMu.Unlock()

		wg.Go(func() {
			s.scheduleTaskCleanup(taskID, 50)
		})
	}

	// All goroutines should exit after TTL (50ms).
	waitCh := make(chan struct{})
	go func() { wg.Wait(); close(waitCh) }()

	select {
	case <-waitCh:
		// All cleanup goroutines exited.
	case <-time.After(2 * time.Second):
		t.Fatal("scheduleTaskCleanup goroutines did not exit after TTL")
	}
}

func TestScheduleTaskCleanup_CleansUpAfterTTL(t *testing.T) {
	s := NewMCPServer("test", "1.0.0")

	taskID := "test-ttl-task"

	// Add a task entry
	s.tasksMu.Lock()
	s.tasks[taskID] = &taskEntry{
		task: mcp.NewTask(taskID),
		done: make(chan struct{}),
	}
	s.tasksMu.Unlock()

	// Schedule cleanup with a very short TTL (50ms)
	go s.scheduleTaskCleanup(taskID, 50)

	// Wait for cleanup to happen
	time.Sleep(200 * time.Millisecond)

	// Task should be removed from tasks map
	s.tasksMu.RLock()
	_, exists := s.tasks[taskID]
	_, expired := s.expiredTasks[taskID]
	s.tasksMu.RUnlock()

	assert.False(t, exists, "task should be removed after TTL")
	assert.True(t, expired, "task should appear in expiredTasks tombstone")
}
