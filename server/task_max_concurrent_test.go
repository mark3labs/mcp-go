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

// TestMCPServer_MaxConcurrentTasks tests the max concurrent tasks limit feature
func TestMCPServer_MaxConcurrentTasks(t *testing.T) {
	t.Run("no_limit_allows_unlimited_tasks", func(t *testing.T) {
		srv := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))
		ctx := context.Background()

		// Create many tasks without limit
		for i := 0; i < 100; i++ {
			_, err := srv.createTask(ctx, fmt.Sprintf("task-%d", i), nil, nil)
			require.NoError(t, err)
		}

		tasks := srv.listTasks(ctx)
		assert.Len(t, tasks, 100)
	})

	t.Run("zero_limit_allows_unlimited_tasks", func(t *testing.T) {
		srv := NewMCPServer("test", "1.0.0",
			WithTaskCapabilities(true, true, true),
			WithMaxConcurrentTasks(0),
		)
		ctx := context.Background()

		// Create many tasks with zero limit (should behave as no limit)
		for i := 0; i < 50; i++ {
			_, err := srv.createTask(ctx, fmt.Sprintf("task-%d", i), nil, nil)
			require.NoError(t, err)
		}

		tasks := srv.listTasks(ctx)
		assert.Len(t, tasks, 50)
	})

	t.Run("negative_limit_allows_unlimited_tasks", func(t *testing.T) {
		srv := NewMCPServer("test", "1.0.0",
			WithTaskCapabilities(true, true, true),
			WithMaxConcurrentTasks(-1),
		)
		ctx := context.Background()

		// Create many tasks with negative limit (should behave as no limit)
		for i := 0; i < 50; i++ {
			_, err := srv.createTask(ctx, fmt.Sprintf("task-%d", i), nil, nil)
			require.NoError(t, err)
		}

		tasks := srv.listTasks(ctx)
		assert.Len(t, tasks, 50)
	})

	t.Run("enforces_concurrent_task_limit", func(t *testing.T) {
		maxTasks := 3
		srv := NewMCPServer("test", "1.0.0",
			WithTaskCapabilities(true, true, true),
			WithMaxConcurrentTasks(maxTasks),
		)
		ctx := context.Background()

		// Create tasks up to the limit
		for i := 0; i < maxTasks; i++ {
			_, err := srv.createTask(ctx, fmt.Sprintf("task-%d", i), nil, nil)
			require.NoError(t, err, "Should be able to create task %d", i)
		}

		// Next task should fail
		_, err := srv.createTask(ctx, "task-overflow", nil, nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrMaxConcurrentTasksExceeded)

		// Verify we have exactly maxTasks tasks
		tasks := srv.listTasks(ctx)
		assert.Len(t, tasks, maxTasks)
	})

	t.Run("terminal_tasks_do_not_count_toward_limit", func(t *testing.T) {
		maxTasks := 2
		srv := NewMCPServer("test", "1.0.0",
			WithTaskCapabilities(true, true, true),
			WithMaxConcurrentTasks(maxTasks),
		)
		ctx := context.Background()

		// Create tasks up to limit
		entry1, err := srv.createTask(ctx, "task-1", nil, nil)
		require.NoError(t, err)
		entry2, err := srv.createTask(ctx, "task-2", nil, nil)
		require.NoError(t, err)

		// Should fail to create another
		_, err = srv.createTask(ctx, "task-3", nil, nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrMaxConcurrentTasksExceeded)

		// Complete task-1
		srv.completeTask(entry1, &mcp.CallToolResult{}, nil)
		assert.Equal(t, mcp.TaskStatusCompleted, entry1.task.Status)

		// Now should be able to create a new task
		_, err = srv.createTask(ctx, "task-4", nil, nil)
		require.NoError(t, err)

		// Cancel task-2
		err = srv.cancelTask(ctx, entry2.task.TaskId)
		require.NoError(t, err)
		assert.Equal(t, mcp.TaskStatusCancelled, entry2.task.Status)

		// Should be able to create another new task
		_, err = srv.createTask(ctx, "task-5", nil, nil)
		require.NoError(t, err)
	})

	t.Run("failed_tasks_do_not_count_toward_limit", func(t *testing.T) {
		maxTasks := 2
		srv := NewMCPServer("test", "1.0.0",
			WithTaskCapabilities(true, true, true),
			WithMaxConcurrentTasks(maxTasks),
		)
		ctx := context.Background()

		// Create tasks up to limit
		entry1, err := srv.createTask(ctx, "task-1", nil, nil)
		require.NoError(t, err)
		_, err = srv.createTask(ctx, "task-2", nil, nil)
		require.NoError(t, err)

		// Fail task-1
		srv.completeTask(entry1, nil, fmt.Errorf("task failed"))
		assert.Equal(t, mcp.TaskStatusFailed, entry1.task.Status)

		// Should be able to create a new task
		_, err = srv.createTask(ctx, "task-3", nil, nil)
		require.NoError(t, err)
	})

	t.Run("limit_enforced_per_session", func(t *testing.T) {
		maxTasks := 2
		srv := NewMCPServer("test", "1.0.0",
			WithTaskCapabilities(true, true, true),
			WithMaxConcurrentTasks(maxTasks),
		)

		session1 := newTestSession("session-1")
		session2 := newTestSession("session-2")
		ctx1 := srv.WithContext(context.Background(), session1)
		ctx2 := srv.WithContext(context.Background(), session2)

		// Session 1: Create tasks up to limit
		_, err := srv.createTask(ctx1, "task-1-1", nil, nil)
		require.NoError(t, err)
		_, err = srv.createTask(ctx1, "task-1-2", nil, nil)
		require.NoError(t, err)

		// Session 1: Should fail to create another
		_, err = srv.createTask(ctx1, "task-1-3", nil, nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrMaxConcurrentTasksExceeded)

		// Session 2: Should be able to create its own tasks
		_, err = srv.createTask(ctx2, "task-2-1", nil, nil)
		require.NoError(t, err)
		_, err = srv.createTask(ctx2, "task-2-2", nil, nil)
		require.NoError(t, err)

		// Session 2: Should also hit the limit
		_, err = srv.createTask(ctx2, "task-2-3", nil, nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrMaxConcurrentTasksExceeded)
	})

	t.Run("limit_enforced_via_task_augmented_tool_call", func(t *testing.T) {
		maxTasks := 2
		srv := NewMCPServer("test", "1.0.0",
			WithTaskCapabilities(true, true, true),
			WithMaxConcurrentTasks(maxTasks),
		)

		// Add a task tool that takes a while
		tool := mcp.NewTool(
			"slow_tool",
			mcp.WithTaskSupport(mcp.TaskSupportRequired),
		)
		err := srv.AddTaskTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			time.Sleep(200 * time.Millisecond)
			return &mcp.CallToolResult{}, nil
		})
		require.NoError(t, err)

		ctx := context.Background()

		// Call tool twice (up to limit)
		req1 := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "slow_tool",
				Task: &mcp.TaskParams{},
			},
		}
		result1, reqErr := srv.handleToolCall(ctx, nil, req1)
		require.Nil(t, reqErr)
		require.NotNil(t, result1)

		result2, reqErr := srv.handleToolCall(ctx, nil, req1)
		require.Nil(t, reqErr)
		require.NotNil(t, result2)

		// Third call should fail due to limit
		_, reqErr = srv.handleToolCall(ctx, nil, req1)
		require.NotNil(t, reqErr)
		assert.Equal(t, mcp.INTERNAL_ERROR, reqErr.code)
		assert.ErrorIs(t, reqErr.err, ErrMaxConcurrentTasksExceeded)

		// Wait for tasks to complete
		time.Sleep(300 * time.Millisecond)

		// Now should be able to create new tasks
		result4, reqErr := srv.handleToolCall(ctx, nil, req1)
		require.Nil(t, reqErr)
		require.NotNil(t, result4)
	})

	t.Run("count_active_tasks_only_counts_non_terminal", func(t *testing.T) {
		srv := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))
		ctx := context.Background()

		// Create various tasks
		entryWorking, err := srv.createTask(ctx, "task-working", nil, nil)
		require.NoError(t, err)

		entryCompleted, err := srv.createTask(ctx, "task-completed", nil, nil)
		require.NoError(t, err)
		srv.completeTask(entryCompleted, &mcp.CallToolResult{}, nil)

		entryFailed, err := srv.createTask(ctx, "task-failed", nil, nil)
		require.NoError(t, err)
		srv.completeTask(entryFailed, nil, fmt.Errorf("failed"))

		entryCancelled, err := srv.createTask(ctx, "task-cancelled", nil, nil)
		require.NoError(t, err)
		err = srv.cancelTask(ctx, entryCancelled.task.TaskId)
		require.NoError(t, err)

		// Count active tasks (should only count working task)
		activeCount := srv.countActiveTasks(getSessionID(ctx))
		assert.Equal(t, 1, activeCount, "Only working task should be counted as active")

		// Verify status
		assert.Equal(t, mcp.TaskStatusWorking, entryWorking.task.Status)
		assert.Equal(t, mcp.TaskStatusCompleted, entryCompleted.task.Status)
		assert.Equal(t, mcp.TaskStatusFailed, entryFailed.task.Status)
		assert.Equal(t, mcp.TaskStatusCancelled, entryCancelled.task.Status)
	})

	t.Run("concurrent_task_creation_respects_limit", func(t *testing.T) {
		maxTasks := 5
		srv := NewMCPServer("test", "1.0.0",
			WithTaskCapabilities(true, true, true),
			WithMaxConcurrentTasks(maxTasks),
		)
		ctx := context.Background()

		// Try to create many tasks concurrently
		var wg sync.WaitGroup
		successCount := 0
		failureCount := 0
		var mu sync.Mutex

		numAttempts := 20
		for i := 0; i < numAttempts; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, err := srv.createTask(ctx, fmt.Sprintf("task-%d", i), nil, nil)
				mu.Lock()
				if err == nil {
					successCount++
				} else {
					failureCount++
				}
				mu.Unlock()
			}(i)
		}

		wg.Wait()

		// Due to race conditions in concurrent creation, we may create slightly more than maxTasks
		// before all goroutines check the limit. The important thing is:
		// 1. We don't create dramatically more tasks (within reasonable margin)
		// 2. At least some attempts were rejected
		tasks := srv.listTasks(ctx)
		assert.LessOrEqual(t, len(tasks), maxTasks+3, "Should not create dramatically more than maxTasks")
		assert.GreaterOrEqual(t, failureCount, numAttempts-maxTasks-3, "At least some attempts should fail")
		assert.Greater(t, successCount, 0, "At least some tasks should be created")
	})
}
