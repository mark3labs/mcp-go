package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestTaskHooksCanReadTaskState(t *testing.T) {
	for _, phase := range []string{"created", "completed", "failed", "cancelled", "task handler cancellation", "regular handler cancellation"} {
		t.Run(phase, func(t *testing.T) {
			hooks := &TaskHooks{}
			s := NewMCPServer("test", "1", WithTaskHooks(hooks))
			observed := make(chan struct{})
			observe := func(context.Context, TaskMetrics) { s.listTasks(context.Background()); close(observed) }
			if phase == "created" {
				hooks.AddOnTaskCreated(observe)
			}
			done := make(chan error, 1)
			go func() {
				entry, err := s.createTask(context.Background(), "task", "tool", nil, nil)
				if err != nil {
					done <- err
					return
				}
				switch phase {
				case "completed":
					hooks.AddOnTaskCompleted(observe)
					s.completeTask(entry, nil, nil)
				case "failed":
					hooks.AddOnTaskFailed(observe)
					s.completeTask(entry, nil, errors.New("failure"))
				case "task handler cancellation":
					hooks.AddOnTaskCancelled(observe)
					s.executeTaskTool(context.Background(), entry, ServerTaskTool{Handler: func(context.Context, mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
						return nil, context.Canceled
					}}, mcp.CallToolRequest{})
				case "regular handler cancellation":
					hooks.AddOnTaskCancelled(observe)
					s.executeRegularToolAsTask(context.Background(), entry, ServerTool{Handler: func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) { return nil, context.Canceled }}, mcp.CallToolRequest{})
				case "cancelled":
					hooks.AddOnTaskCancelled(observe)
					err = s.cancelTask(context.Background(), "task")
				}
				done <- err
			}()
			select {
			case <-observed:
			case <-time.After(time.Second):
				t.Fatal("task hook deadlocked while reading task state")
			}
			require.NoError(t, <-done)
		})
	}
}

func TestTaskHookMetricsRemainEventSnapshotsDuringConcurrentCompletion(t *testing.T) {
	for i := range 100 {
		var mu sync.Mutex
		var events []TaskMetrics
		hooks := &TaskHooks{}
		s := NewMCPServer("test", "1", WithTaskHooks(hooks))
		hooks.AddOnTaskStatusChanged(func(ctx context.Context, metrics TaskMetrics) {
			s.listTasks(ctx)
			mu.Lock()
			events = append(events, metrics)
			mu.Unlock()
		})
		id := fmt.Sprintf("task-%d", i)
		entry, err := s.createTask(t.Context(), id, "tool", nil, nil)
		require.NoError(t, err)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); s.completeTask(entry, nil, nil) }()
		go func() { defer wg.Done(); _ = s.cancelTask(t.Context(), id) }()
		wg.Wait()
		require.Len(t, events, 2)
		require.Equal(t, mcp.TaskStatusWorking, events[0].Status)
		require.True(t, events[1].Status.IsTerminal())
		require.Nil(t, events[0].CompletedAt)
		require.NotNil(t, events[1].CompletedAt)
		task, _, err := s.getTask(t.Context(), id)
		require.NoError(t, err)
		require.Equal(t, task.Status, events[1].Status)
	}
}
