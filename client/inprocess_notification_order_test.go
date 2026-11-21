package client

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func startInProcessOrderClient(t *testing.T, s *server.MCPServer) *Client {
	t.Helper()
	c, err := NewInProcessClient(s)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Start(t.Context()))
	init := mcp.InitializeRequest{}
	init.Params.ProtocolVersion = mcp.LATEST_LEGACY_PROTOCOL_VERSION
	_, err = c.Initialize(t.Context(), init)
	require.NoError(t, err)
	return c
}

func callTool(t *testing.T, c *Client, name string) {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	_, err := c.CallTool(t.Context(), req)
	require.NoError(t, err)
}

func TestInProcessClient_RequestNotificationsArriveBeforeResult(t *testing.T) {
	s := server.NewMCPServer("test", "1")
	s.AddTool(mcp.NewTool("work"), func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		for i := 1; i <= 5; i++ {
			err := server.ServerFromContext(ctx).SendNotificationToClient(ctx, string(mcp.MethodNotificationProgress), map[string]any{"progressToken": "p", "progress": i})
			if err != nil {
				return nil, err
			}
		}
		return mcp.NewToolResultText("done"), nil
	})
	c := startInProcessOrderClient(t, s)

	var mu sync.Mutex
	var got []any
	c.OnNotification(func(n mcp.JSONRPCNotification) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, n.Params.AdditionalFields["progress"])
	})

	for range 50 {
		mu.Lock()
		got = nil
		mu.Unlock()

		callTool(t, c, "work")

		mu.Lock()
		require.Equal(t, []any{1, 2, 3, 4, 5}, got)
		mu.Unlock()
	}
}

func TestInProcessClient_NotificationHandlerCanCallServer(t *testing.T) {
	s := server.NewMCPServer("test", "1")
	s.AddTool(mcp.NewTool("work"), func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		err := server.ServerFromContext(ctx).SendNotificationToClient(ctx, string(mcp.MethodNotificationProgress), map[string]any{"progressToken": "p", "progress": 1})
		return mcp.NewToolResultText("done"), err
	})
	c := startInProcessOrderClient(t, s)

	listed := make(chan error, 1)
	c.OnNotification(func(mcp.JSONRPCNotification) {
		_, err := c.ListTools(t.Context(), mcp.ListToolsRequest{})
		listed <- err
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		callTool(t, c, "work")
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("CallTool did not return")
	}
	select {
	case err := <-listed:
		require.NoError(t, err)
	default:
		t.Fatal("notification handler did not run before CallTool returned")
	}
}

func TestInProcessClient_NotificationAfterRequestEndsIsQueued(t *testing.T) {
	release := make(chan struct{})
	s := server.NewMCPServer("test", "1")
	s.AddTool(mcp.NewTool("work"), func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		srv := server.ServerFromContext(ctx)
		later := context.WithoutCancel(ctx)
		go func() {
			<-release
			_ = srv.SendNotificationToClient(later, "notifications/late", nil)
		}()
		return mcp.NewToolResultText("done"), nil
	})
	c := startInProcessOrderClient(t, s)

	got := make(chan string, 1)
	c.OnNotification(func(n mcp.JSONRPCNotification) { got <- n.Method })

	callTool(t, c, "work")
	close(release)

	select {
	case method := <-got:
		require.Equal(t, "notifications/late", method)
	case <-time.After(5 * time.Second):
		t.Fatal("notification sent after the request ended was not delivered")
	}
}
