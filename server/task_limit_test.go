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

func TestMaxConcurrentTasks(t *testing.T) {
	t.Run("allows tasks up to the limit", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0",
			WithTaskCapabilities(true, true, true),
			WithMaxConcurrentTasks(3),
		)
		ctx := context.Background()

		// Create 3 tasks (at the limit)
		_, err := server.createTask(ctx, "task-1", "test-tool", nil, nil)
		require.NoError(t, err)

		_, err = server.createTask(ctx, "task-2", "test-tool", nil, nil)
		require.NoError(t, err)

		_, err = server.createTask(ctx, "task-3", "test-tool", nil, nil)
		require.NoError(t, err)

		// Verify all tasks were created
		server.tasksMu.RLock()
		assert.Equal(t, 3, len(server.tasks))
		assert.Equal(t, 3, server.activeTasks)
		server.tasksMu.RUnlock()
	})

	t.Run("rejects tasks when limit is exceeded", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0",
			WithTaskCapabilities(true, true, true),
			WithMaxConcurrentTasks(2),
		)
		ctx := context.Background()

		// Create 2 tasks (at the limit)
		_, err := server.createTask(ctx, "task-1", "test-tool", nil, nil)
		require.NoError(t, err)

		_, err = server.createTask(ctx, "task-2", "test-tool", nil, nil)
		require.NoError(t, err)

		// Attempt to create a third task (should fail)
		_, err = server.createTask(ctx, "task-3", "test-tool", nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "max concurrent tasks limit reached")
		assert.Contains(t, err.Error(), "(2)")

		// Verify only 2 tasks were created
		server.tasksMu.RLock()
		assert.Equal(t, 2, len(server.tasks))
		assert.Equal(t, 2, server.activeTasks)
		server.tasksMu.RUnlock()
	})

	t.Run("allows new tasks after completing old ones", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0",
			WithTaskCapabilities(true, true, true),
			WithMaxConcurrentTasks(2),
		)
		ctx := context.Background()

		// Create 2 tasks (at the limit)
		entry1, err := server.createTask(ctx, "task-1", "test-tool", nil, nil)
		require.NoError(t, err)

		entry2, err := server.createTask(ctx, "task-2", "test-tool", nil, nil)
		require.NoError(t, err)

		// Attempt to create a third task (should fail)
		_, err = server.createTask(ctx, "task-3", "test-tool", nil, nil)
		require.Error(t, err)

		// Complete one task
		server.completeTask(entry1, "result", nil)

		// Now we should be able to create a new task
		_, err = server.createTask(ctx, "task-4", "test-tool", nil, nil)
		require.NoError(t, err)

		// Verify counter decremented then incremented
		server.tasksMu.RLock()
		assert.Equal(t, 3, len(server.tasks))  // task-1 (completed), task-2, task-4
		assert.Equal(t, 2, server.activeTasks) // Only task-2 and task-4 are active
		server.tasksMu.RUnlock()

		// Complete another task
		server.completeTask(entry2, "result", nil)

		// Create another task
		_, err = server.createTask(ctx, "task-5", "test-tool", nil, nil)
		require.NoError(t, err)

		server.tasksMu.RLock()
		assert.Equal(t, 2, server.activeTasks) // task-4 and task-5
		server.tasksMu.RUnlock()
	})

	t.Run("allows new tasks after cancelling old ones", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0",
			WithTaskCapabilities(true, true, true),
			WithMaxConcurrentTasks(2),
		)
		ctx := context.Background()

		// Create 2 tasks (at the limit)
		entry1, err := server.createTask(ctx, "task-1", "test-tool", nil, nil)
		require.NoError(t, err)

		_, err = server.createTask(ctx, "task-2", "test-tool", nil, nil)
		require.NoError(t, err)

		// Add cancel function to first task
		cancelCtx, cancel := context.WithCancel(ctx)
		server.tasksMu.Lock()
		entry1.cancelFunc = cancel
		server.tasksMu.Unlock()

		// Cancel one task
		err = server.cancelTask(cancelCtx, "task-1")
		require.NoError(t, err)

		// Now we should be able to create a new task
		_, err = server.createTask(ctx, "task-3", "test-tool", nil, nil)
		require.NoError(t, err)

		server.tasksMu.RLock()
		assert.Equal(t, 2, server.activeTasks) // task-2 and task-3
		server.tasksMu.RUnlock()
	})

	t.Run("no limit when maxConcurrentTasks is not set", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0",
			WithTaskCapabilities(true, true, true),
		)
		ctx := context.Background()

		// Create many tasks without limit
		for i := range 100 {
			_, err := server.createTask(ctx, fmt.Sprintf("task-%d", i), "test-tool", nil, nil)
			require.NoError(t, err)
		}

		server.tasksMu.RLock()
		assert.Equal(t, 100, len(server.tasks))
		assert.Equal(t, 100, server.activeTasks)
		server.tasksMu.RUnlock()
	})

	t.Run("no limit when maxConcurrentTasks is 0", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0",
			WithTaskCapabilities(true, true, true),
			WithMaxConcurrentTasks(0),
		)
		ctx := context.Background()

		// Create many tasks without limit
		for i := range 50 {
			_, err := server.createTask(ctx, fmt.Sprintf("task-%d", i), "test-tool", nil, nil)
			require.NoError(t, err)
		}

		server.tasksMu.RLock()
		assert.Equal(t, 50, len(server.tasks))
		assert.Equal(t, 50, server.activeTasks)
		server.tasksMu.RUnlock()
	})

	t.Run("concurrent task creation respects limit", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0",
			WithTaskCapabilities(true, true, true),
			WithMaxConcurrentTasks(10),
		)
		ctx := context.Background()

		var wg sync.WaitGroup
		successCount := 0
		failureCount := 0
		var mu sync.Mutex

		// Attempt to create 20 tasks concurrently (limit is 10)
		for i := range 20 {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				_, err := server.createTask(ctx, fmt.Sprintf("task-%d", id), "test-tool", nil, nil)
				mu.Lock()
				if err != nil {
					failureCount++
				} else {
					successCount++
				}
				mu.Unlock()
			}(i)
		}

		wg.Wait()

		// Exactly 10 should succeed, 10 should fail
		mu.Lock()
		assert.Equal(t, 10, successCount, "Should create exactly 10 tasks")
		assert.Equal(t, 10, failureCount, "Should reject exactly 10 tasks")
		mu.Unlock()

		server.tasksMu.RLock()
		assert.Equal(t, 10, len(server.tasks))
		assert.Equal(t, 10, server.activeTasks)
		server.tasksMu.RUnlock()
	})

	t.Run("handleTaskAugmentedToolCall returns error when limit exceeded", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0",
			WithTaskCapabilities(true, true, true),
			WithMaxConcurrentTasks(1),
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
			// Sleep to keep the task running
			time.Sleep(100 * time.Millisecond)
			return &mcp.CreateTaskResult{}, nil
		}
		server.AddTaskTool(tool, handler)

		ctx := context.Background()

		// Create first task (should succeed)
		request1 := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "async-tool",
				Task: &mcp.TaskParams{},
			},
		}
		result1, reqErr := server.handleTaskAugmentedToolCall(ctx, "test-id-1", request1)
		require.Nil(t, reqErr)
		require.NotNil(t, result1)

		// Attempt to create second task (should fail due to limit)
		request2 := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "async-tool",
				Task: &mcp.TaskParams{},
			},
		}
		result2, reqErr := server.handleTaskAugmentedToolCall(ctx, "test-id-2", request2)
		require.NotNil(t, reqErr)
		require.Nil(t, result2)
		assert.Equal(t, mcp.INTERNAL_ERROR, reqErr.code)
		assert.Contains(t, reqErr.err.Error(), "max concurrent tasks limit reached")
	})
}
