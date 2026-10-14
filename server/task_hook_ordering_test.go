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

func newHookOrderingServer(t *testing.T, hooks *TaskHooks) *MCPServer {
	t.Helper()
	server := NewMCPServer("test-server", "1.0.0", WithTaskHooks(hooks), WithTaskCapabilities(true, true, false))
	server.AddTaskTool(mcp.Tool{Name: "slow", Execution: &mcp.ToolExecution{TaskSupport: mcp.TaskSupportRequired}},
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		})
	return server
}

func startSlowTask(t *testing.T, server *MCPServer) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = server.handleTaskAugmentedToolCall(t.Context(), "slow", mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "slow", Task: &mcp.TaskParams{}}})
	}()
	return done
}

// A task's hooks come in order: a cancellation that lands while the created
// hook is still running fires its own hook after it, and still reaches the
// handler, which starts after the created hook returns.
func TestTaskHooksCreatedHookPrecedesCancellation(t *testing.T) {
	entered, release := make(chan string, 1), make(chan struct{})
	var mu sync.Mutex
	var events []string
	record := func(event string) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	}
	hooks := &TaskHooks{}
	hooks.AddOnTaskCreated(func(_ context.Context, m TaskMetrics) {
		entered <- m.TaskID
		<-release
		record("created")
	})
	cancelled := make(chan struct{})
	hooks.AddOnTaskCancelled(func(context.Context, TaskMetrics) {
		record("cancelled")
		close(cancelled)
	})
	server := newHookOrderingServer(t, hooks)
	handlerDone := startSlowTask(t, server)

	taskID := <-entered
	// The cancellation returns while the created hook still runs: its hook
	// waits for that one.
	require.NoError(t, server.cancelTask(t.Context(), taskID))
	close(release)
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the task was never cancelled")
	}
	select {
	case <-handlerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler of a task cancelled during its created hook kept running")
	}
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"created", "cancelled"}, events)
}

// A created hook may cancel the task it is called for: the cancellation
// returns, and its hook runs after the created one.
func TestTaskHooksCreatedHookMayCancelItsOwnTask(t *testing.T) {
	var server *MCPServer
	var mu sync.Mutex
	var events []string
	record := func(event string) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	}
	hooks := &TaskHooks{}
	cancelErr := make(chan error, 1)
	hooks.AddOnTaskCreated(func(ctx context.Context, m TaskMetrics) {
		cancelErr <- server.cancelTask(ctx, m.TaskID)
		record("created")
	})
	cancelled := make(chan struct{})
	hooks.AddOnTaskCancelled(func(context.Context, TaskMetrics) {
		record("cancelled")
		close(cancelled)
	})
	server = newHookOrderingServer(t, hooks)
	done := startSlowTask(t, server)

	select {
	case err := <-cancelErr:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("a created hook cancelling its own task hung")
	}
	<-done
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("the cancelled hook never ran")
	}
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"created", "cancelled"}, events)
}

// A created hook that panics does not leave the task's later hooks waiting:
// a cancellation that landed while it ran still gets its hook.
func TestTaskHooksPanickingCreatedHookDoesNotLoseCancellation(t *testing.T) {
	var server *MCPServer
	hooks := &TaskHooks{}
	hooks.AddOnTaskCreated(func(ctx context.Context, m TaskMetrics) {
		require.NoError(t, server.cancelTask(ctx, m.TaskID))
		panic("hook bug")
	})
	cancelled := make(chan struct{})
	hooks.AddOnTaskCancelled(func(context.Context, TaskMetrics) { close(cancelled) })
	server = NewMCPServer("test-server", "1.0.0", WithTaskHooks(hooks), WithTaskCapabilities(true, true, false))

	func() {
		defer func() { require.Equal(t, "hook bug", recover()) }()
		_, _ = server.createTask(t.Context(), "task-1", "tool", nil, nil)
	}()
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("the cancelled hook never ran after the created hook panicked")
	}
}

// A hook that waited for the created hook runs with the values of the
// request that raised it, but not its cancellation: that request may have
// ended by then.
func TestTaskHooksWaitingHookOutlivesItsRequest(t *testing.T) {
	type keyType struct{}
	entered, release := make(chan string, 1), make(chan struct{})
	hooks := &TaskHooks{}
	hooks.AddOnTaskCreated(func(_ context.Context, m TaskMetrics) {
		entered <- m.TaskID
		<-release
	})
	seen := make(chan context.Context, 1)
	hooks.AddOnTaskCancelled(func(ctx context.Context, _ TaskMetrics) { seen <- ctx })
	server := newHookOrderingServer(t, hooks)
	startSlowTask(t, server)

	taskID := <-entered
	// The cancel request ends, as a served request does, before the created
	// hook lets its hook run.
	cancelCtx, endRequest := context.WithCancel(context.WithValue(t.Context(), keyType{}, "request value"))
	require.NoError(t, server.cancelTask(cancelCtx, taskID))
	endRequest()
	close(release)
	select {
	case ctx := <-seen:
		assert.NoError(t, ctx.Err(), "the waiting hook ran on an ended context")
		assert.Equal(t, "request value", ctx.Value(keyType{}))
	case <-time.After(2 * time.Second):
		t.Fatal("the cancelled hook never ran")
	}
}
