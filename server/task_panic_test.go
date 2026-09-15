package server

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestExecuteTaskTool_PanicRecovery verifies that a panicking task tool handler is recovered
// and updates the task status to failed.
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

// TestScheduleTaskCleanup_GoroutinesExitAfterTTL verifies that cleanup goroutines exit
// after their TTL expires and do not leak.
func TestScheduleTaskCleanup_GoroutinesExitAfterTTL(t *testing.T) {
	s := NewMCPServer("test", "1.0.0")

	const numTasks = 10
	var wg sync.WaitGroup

	for i := range numTasks {
		taskID := fmt.Sprintf("leak-test-%d", i)

		entry := &taskEntry{
			task: mcp.NewTask(taskID),
			done: make(chan struct{}),
		}
		s.tasksMu.Lock()
		s.tasks[taskID] = entry
		s.tasksMu.Unlock()

		wg.Go(func() {
			s.scheduleTaskCleanup(taskID, entry, 50)
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

// TestScheduleTaskCleanup_CleansUpAfterTTL verifies that a task is removed from storage
// and added to expiredTasks after its TTL expires.
func TestScheduleTaskCleanup_CleansUpAfterTTL(t *testing.T) {
	s := NewMCPServer("test", "1.0.0")

	taskID := "test-ttl-task"

	// Add a task entry
	entry := &taskEntry{
		task: mcp.NewTask(taskID),
		done: make(chan struct{}),
	}
	s.tasksMu.Lock()
	s.tasks[taskID] = entry
	s.tasksMu.Unlock()

	// Schedule cleanup with a very short TTL (50ms)
	go s.scheduleTaskCleanup(taskID, entry, 50)

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

// TestScheduleTaskCleanup_CancelsRunningTaskOnTTL verifies execution stops and frees its concurrency slot.
func TestScheduleTaskCleanup_CancelsRunningTaskOnTTL(t *testing.T) {
	tests := []struct {
		name              string
		regularTool       bool
		expireBeforeStart bool
	}{
		{name: "running task tool"},
		{name: "running regular tool", regularTool: true},
		{name: "task tool expired before execution", expireBeforeStart: true},
		{name: "regular tool expired before execution", regularTool: true, expireBeforeStart: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewMCPServer("test", "1.0.0", WithMaxConcurrentTasks(1))
			const taskID = "ttl-task"
			entry, err := s.createTask(t.Context(), taskID, "ttl-tool", nil, nil)
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			started := make(chan struct{})
			exited := make(chan struct{})
			t.Cleanup(func() {
				cancel()
				select {
				case <-exited:
				case <-time.After(time.Second):
					t.Error("task execution did not exit after test cleanup")
				}
			})

			handler := func(taskCtx context.Context) error {
				close(started)
				<-taskCtx.Done()
				return taskCtx.Err()
			}
			request := mcp.CallToolRequest{
				Params: mcp.CallToolParams{Name: "ttl-tool"},
			}

			if tt.expireBeforeStart {
				s.scheduleTaskCleanup(taskID, entry, 50)
			}

			go func() {
				defer close(exited)
				if tt.regularTool {
					s.executeRegularToolAsTask(ctx, entry, ServerTool{
						Handler: func(taskCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
							return nil, handler(taskCtx)
						},
					}, request)
				} else {
					s.executeTaskTool(ctx, entry, ServerTaskTool{
						Handler: func(taskCtx context.Context, request mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
							return nil, handler(taskCtx)
						},
					}, request)
				}
			}()

			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("task handler did not start")
			}

			if !tt.expireBeforeStart {
				s.scheduleTaskCleanup(taskID, entry, 50)
			}

			select {
			case <-exited:
			case <-time.After(time.Second):
				t.Fatal("task execution did not exit after TTL expiration")
			}

			assert.NoError(t, ctx.Err(), "TTL expiration must not cancel the parent context")
			select {
			case <-entry.done:
			default:
				t.Error("task completion was not signalled")
			}

			s.tasksMu.RLock()
			assert.True(t, entry.completed)
			assert.Equal(t, mcp.TaskStatusCancelled, entry.task.Status)
			assert.Zero(t, s.activeTasks)
			assert.NotContains(t, s.tasks, taskID)
			assert.Contains(t, s.expiredTasks, taskID)
			s.tasksMu.RUnlock()

			nextEntry, err := s.createTask(ctx, "next-task", "ttl-tool", nil, nil)
			require.NoError(t, err, "expired task must release its concurrency slot")
			s.completeTask(nextEntry, nil, nil)
		})
	}
}

// TestScheduleTaskCleanup_CancellationConditions verifies cancellation guards and lock ownership.
func TestScheduleTaskCleanup_CancellationConditions(t *testing.T) {
	tests := []struct {
		name          string
		exists        bool
		completed     bool
		withCancel    bool
		wantCancelled bool
	}{
		{name: "running", exists: true, withCancel: true, wantCancelled: true},
		{name: "completed", exists: true, completed: true, withCancel: true},
		{name: "without cancel function", exists: true},
		{name: "missing", withCancel: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewMCPServer("test", "1.0.0")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			const taskID = "ttl-task"
			entry := &taskEntry{task: mcp.NewTask(taskID), completed: tt.completed}
			if tt.withCancel {
				entry.cancelFunc = func() {
					if assert.True(t, s.tasksMu.TryLock(), "cancel must run outside tasksMu") {
						s.tasksMu.Unlock()
					}
					cancel()
				}
			}
			if tt.exists {
				s.tasks[taskID] = entry
			}

			s.scheduleTaskCleanup(taskID, entry, 1)

			if tt.wantCancelled {
				assert.ErrorIs(t, ctx.Err(), context.Canceled)
			} else {
				assert.NoError(t, ctx.Err())
			}
			assert.NotContains(t, s.tasks, taskID)
			if tt.exists {
				assert.Contains(t, s.expiredTasks, taskID)
			} else {
				assert.NotContains(t, s.expiredTasks, taskID)
			}
		})
	}
}

// TestScheduleTaskCleanup_StaleTimerDoesNotCancelReplacementTask verifies that an expired TTL
// timer from a previous task does not cancel or remove a newly replaced task with the same ID.
func TestScheduleTaskCleanup_StaleTimerDoesNotCancelReplacementTask(t *testing.T) {
	s := NewMCPServer("test", "1.0.0")
	const taskID = "replaced-task"

	ctxOld, cancelOld := context.WithCancel(t.Context())
	defer cancelOld()
	oldEntry := &taskEntry{
		task:       mcp.NewTask(taskID),
		cancelFunc: cancelOld,
		done:       make(chan struct{}),
	}

	ctxNew, cancelNew := context.WithCancel(t.Context())
	defer cancelNew()
	newEntry := &taskEntry{
		task:       mcp.NewTask(taskID),
		cancelFunc: cancelNew,
		done:       make(chan struct{}),
	}

	// Store newEntry under the same taskID
	s.tasksMu.Lock()
	s.tasks[taskID] = newEntry
	s.tasksMu.Unlock()

	// Trigger cleanup timer bound to oldEntry
	s.scheduleTaskCleanup(taskID, oldEntry, 1)

	// The replacement task must not be cancelled or removed
	assert.NoError(t, ctxNew.Err(), "stale cleanup timer must not cancel replacement task")
	assert.NoError(t, ctxOld.Err())

	s.tasksMu.RLock()
	assert.Equal(t, newEntry, s.tasks[taskID], "replacement task must remain in tasks map")
	assert.NotContains(t, s.expiredTasks, taskID, "replacement task ID must not be marked as expired")
	s.tasksMu.RUnlock()
}

