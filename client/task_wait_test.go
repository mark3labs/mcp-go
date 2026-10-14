package client

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_WaitForTask(t *testing.T) {
	tests := []struct {
		name       string
		responses  []mcp.Task
		fallback   time.Duration
		wantStatus mcp.TaskStatus
		wantCalls  int
	}{
		{
			name: "returns an already terminal task without waiting",
			responses: []mcp.Task{
				{TaskId: "task-1", Status: mcp.TaskStatusCompleted},
			},
			fallback:   time.Second,
			wantStatus: mcp.TaskStatusCompleted,
			wantCalls:  1,
		},
		{
			name: "polls until the task reaches a terminal state",
			responses: []mcp.Task{
				{TaskId: "task-1", Status: mcp.TaskStatusWorking},
				{TaskId: "task-1", Status: mcp.TaskStatusFailed},
			},
			fallback:   time.Millisecond,
			wantStatus: mcp.TaskStatusFailed,
			wantCalls:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := newTaskSequenceTransport(tt.responses)
			client := NewClient(transport, WithSession())

			result, err := client.WaitForTask(t.Context(), mcp.GetTaskRequest{
				Params: mcp.GetTaskParams{TaskId: "task-1"},
			}, tt.fallback)

			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, result.Status)
			assert.Equal(t, tt.wantCalls, transport.callCount())
		})
	}
}

func TestClient_WaitForTask_UsesServerPollInterval(t *testing.T) {
	pollInterval := int64(40)
	transport := newTaskSequenceTransport([]mcp.Task{
		{TaskId: "task-1", Status: mcp.TaskStatusWorking, PollInterval: &pollInterval},
		{TaskId: "task-1", Status: mcp.TaskStatusCompleted},
	})
	client := NewClient(transport, WithSession())

	started := time.Now()
	_, err := client.WaitForTask(t.Context(), mcp.GetTaskRequest{
		Params: mcp.GetTaskParams{TaskId: "task-1"},
	}, time.Millisecond)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(started), 30*time.Millisecond)
}

func TestClient_WaitForTask_ContextCancellationInterruptsPolling(t *testing.T) {
	pollInterval := int64(60_000)
	transport := newTaskSequenceTransport([]mcp.Task{
		{TaskId: "task-1", Status: mcp.TaskStatusWorking, PollInterval: &pollInterval},
	})
	client := NewClient(transport, WithSession())
	ctx, cancel := context.WithCancel(t.Context())
	transport.afterResponse = cancel

	_, err := client.WaitForTask(ctx, mcp.GetTaskRequest{
		Params: mcp.GetTaskParams{TaskId: "task-1"},
	}, time.Second)

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
	assert.Equal(t, 1, transport.callCount())
}

func TestClient_WaitForTask_RejectsNonPositiveFallbackInterval(t *testing.T) {
	client := NewClient(newTaskSequenceTransport(nil), WithSession())

	_, err := client.WaitForTask(t.Context(), mcp.GetTaskRequest{
		Params: mcp.GetTaskParams{TaskId: "task-1"},
	}, 0)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidFallbackPollInterval)
}

func TestClient_WaitForTask_RejectsOverflowingServerPollInterval(t *testing.T) {
	pollInterval := int64(1<<63-1)/int64(time.Millisecond) + 1
	client := NewClient(newTaskSequenceTransport([]mcp.Task{
		{TaskId: "task-1", Status: mcp.TaskStatusWorking, PollInterval: &pollInterval},
	}), WithSession())

	_, err := client.WaitForTask(t.Context(), mcp.GetTaskRequest{
		Params: mcp.GetTaskParams{TaskId: "task-1"},
	}, time.Millisecond)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrServerPollIntervalOverflow)
}

func TestClient_WaitForTask_WrapsGetTaskError(t *testing.T) {
	wantErr := errors.New("request failed")
	transport := newTaskSequenceTransport(nil)
	transport.err = wantErr
	client := NewClient(transport, WithSession())

	_, err := client.WaitForTask(t.Context(), mcp.GetTaskRequest{
		Params: mcp.GetTaskParams{TaskId: "task-1"},
	}, time.Millisecond)

	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
	assert.Contains(t, err.Error(), "get task:")
}

type taskSequenceTransport struct {
	mu            sync.Mutex
	responses     []mcp.Task
	calls         int
	afterResponse func()
	err           error
}

func newTaskSequenceTransport(responses []mcp.Task) *taskSequenceTransport {
	return &taskSequenceTransport{responses: responses}
}

func (t *taskSequenceTransport) Start(context.Context) error { return nil }

func (t *taskSequenceTransport) SendRequest(
	_ context.Context,
	request transport.JSONRPCRequest,
) (*transport.JSONRPCResponse, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if request.Method != string(mcp.MethodTasksGet) {
		return nil, errors.New("unexpected request method")
	}
	if t.err != nil {
		return nil, t.err
	}
	if t.calls >= len(t.responses) {
		return nil, errors.New("no scripted task response")
	}

	task := t.responses[t.calls]
	t.calls++
	if t.afterResponse != nil {
		t.afterResponse()
	}
	raw, err := json.Marshal(mcp.GetTaskResult{Task: task})
	if err != nil {
		return nil, err
	}
	return &transport.JSONRPCResponse{Result: raw}, nil
}

func (t *taskSequenceTransport) SendNotification(context.Context, mcp.JSONRPCNotification) error {
	return nil
}

func (t *taskSequenceTransport) SetNotificationHandler(func(mcp.JSONRPCNotification)) {}
func (t *taskSequenceTransport) Close() error                                         { return nil }
func (t *taskSequenceTransport) GetSessionId() string                                 { return "" }

func (t *taskSequenceTransport) callCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}
