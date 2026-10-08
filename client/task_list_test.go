package client

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_ListTasksFollowsPagination(t *testing.T) {
	mcpServer := server.NewMCPServer(
		"test-server",
		"1.0.0",
		server.WithTaskCapabilities(true, true, true),
		server.WithPaginationLimit(2),
	)
	mcpServer.AddTool(
		mcp.NewTool("noop", mcp.WithTaskSupport(mcp.TaskSupportRequired)),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("ok"), nil
		},
	)

	var wantIDs []string
	for i := range 4 {
		response := mcpServer.HandleMessage(t.Context(), []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "tools/call",
			"params": {
				"name": "noop",
				"task": {"ttl": 60000}
			}
		}`))
		success, ok := response.(mcp.JSONRPCResponse)
		require.Truef(t, ok, "task %d: expected JSONRPCResponse, got %T", i, response)
		created, ok := success.Result.(*mcp.CreateTaskResult)
		require.Truef(t, ok, "task %d: expected *CreateTaskResult, got %T", i, success.Result)
		wantIDs = append(wantIDs, created.Task.TaskId)
	}

	c, err := NewInProcessClient(mcpServer)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Start(t.Context()))

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.ProtocolVersion20251125
	initReq.Params.ClientInfo = mcp.Implementation{Name: "test-client", Version: "1.0.0"}
	_, err = c.Initialize(t.Context(), initReq)
	require.NoError(t, err)

	page, err := c.ListTasksByPage(t.Context(), mcp.ListTasksRequest{})
	require.NoError(t, err)
	assert.Len(t, page.Tasks, 2)
	assert.NotEmpty(t, page.NextCursor)

	listed, err := c.ListTasks(t.Context(), mcp.ListTasksRequest{})
	require.NoError(t, err)
	assert.Empty(t, listed.NextCursor)

	var gotIDs []string
	for _, task := range listed.Tasks {
		gotIDs = append(gotIDs, task.TaskId)
	}
	assert.ElementsMatch(t, wantIDs, gotIDs)

	// A second call starts from the caller's cursor, which is still empty.
	again, err := c.ListTasks(t.Context(), mcp.ListTasksRequest{})
	require.NoError(t, err)
	assert.Len(t, again.Tasks, len(wantIDs))
}

func TestClient_ListTasksRejectsRepeatedCursor(t *testing.T) {
	tests := []struct {
		name      string
		pages     []mcp.ListTasksResult
		wantCalls int
	}{
		{
			name: "same cursor returned again",
			pages: []mcp.ListTasksResult{
				{Tasks: []mcp.Task{{TaskId: "t1"}}, PaginatedResult: mcp.PaginatedResult{NextCursor: "page-2"}},
				{Tasks: []mcp.Task{{TaskId: "t2"}}, PaginatedResult: mcp.PaginatedResult{NextCursor: "page-2"}},
			},
			wantCalls: 2,
		},
		{
			name: "cursor cycles back to an earlier page",
			pages: []mcp.ListTasksResult{
				{Tasks: []mcp.Task{{TaskId: "t1"}}, PaginatedResult: mcp.PaginatedResult{NextCursor: "page-2"}},
				{Tasks: []mcp.Task{{TaskId: "t2"}}, PaginatedResult: mcp.PaginatedResult{NextCursor: "page-3"}},
				{Tasks: []mcp.Task{{TaskId: "t3"}}, PaginatedResult: mcp.PaginatedResult{NextCursor: "page-2"}},
			},
			wantCalls: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := newListTasksPageTransport(tt.pages)
			c := NewClient(tr, WithSession())

			_, err := c.ListTasks(t.Context(), mcp.ListTasksRequest{})
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrRepeatedTaskListCursor)
			assert.Equal(t, tt.wantCalls, tr.callCount())
		})
	}
}

type listTasksPageTransport struct {
	mu    sync.Mutex
	pages []mcp.ListTasksResult
	calls int
}

func newListTasksPageTransport(pages []mcp.ListTasksResult) *listTasksPageTransport {
	return &listTasksPageTransport{pages: pages}
}

func (t *listTasksPageTransport) Start(context.Context) error { return nil }

func (t *listTasksPageTransport) SendRequest(
	_ context.Context,
	request transport.JSONRPCRequest,
) (*transport.JSONRPCResponse, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if request.Method != string(mcp.MethodTasksList) {
		return nil, errors.New("unexpected request method")
	}
	if t.calls >= len(t.pages) {
		return nil, errors.New("no scripted tasks/list page")
	}
	page := t.pages[t.calls]
	t.calls++
	raw, err := json.Marshal(page)
	if err != nil {
		return nil, err
	}
	return &transport.JSONRPCResponse{Result: raw}, nil
}

func (t *listTasksPageTransport) SendNotification(context.Context, mcp.JSONRPCNotification) error {
	return nil
}

func (t *listTasksPageTransport) SetNotificationHandler(func(mcp.JSONRPCNotification)) {}
func (t *listTasksPageTransport) Close() error                                         { return nil }
func (t *listTasksPageTransport) GetSessionId() string                                 { return "" }

func (t *listTasksPageTransport) callCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}
