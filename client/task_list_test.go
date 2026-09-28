package client

import (
	"context"
	"testing"

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
