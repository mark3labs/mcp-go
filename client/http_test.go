package client

import (
	"context"
	"fmt"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"testing"
	"time"
)

func TestHTTPClient(t *testing.T) {
	hooks := &server.Hooks{}
	hooks.AddAfterCallTool(func(ctx context.Context, id any, message *mcp.CallToolRequest, result *mcp.CallToolResult) {
		clientSession := server.ClientSessionFromContext(ctx)
		// wait until all the notifications are handled
		for len(clientSession.NotificationChannel()) > 0 {
		}
		time.Sleep(time.Millisecond * 50)
	})

	// Create MCP server with capabilities
	mcpServer := server.NewMCPServer(
		"test-server",
		"1.0.0",
		server.WithToolCapabilities(true),
		server.WithHooks(hooks),
	)

	mcpServer.AddTool(
		mcp.NewTool("notify"),
		func(
			ctx context.Context,
			request mcp.CallToolRequest,
		) (*mcp.CallToolResult, error) {
			server := server.ServerFromContext(ctx)
			err := server.SendNotificationToClient(
				ctx,
				"notifications/progress",
				map[string]any{
					"progress":      10,
					"total":         10,
					"progressToken": 0,
				},
			)
			if err != nil {
				return nil, fmt.Errorf("failed to send notification: %w", err)
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: "notification sent successfully",
					},
				},
			}, nil
		},
	)

	testServer := server.NewTestStreamableHTTPServer(mcpServer)
	defer testServer.Close()

	t.Run("Can receive notification from server", func(t *testing.T) {
		client, err := NewStreamableHttpClient(testServer.URL)
		if err != nil {
			t.Fatalf("create client failed %v", err)
			return
		}

		notificationNum := 0
		client.OnNotification(func(notification mcp.JSONRPCNotification) {
			notificationNum += 1
		})

		ctx := context.Background()

		if err := client.Start(ctx); err != nil {
			t.Fatalf("Failed to start client: %v", err)
			return
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		_, err = client.Initialize(ctx, initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v\n", err)
		}

		request := mcp.CallToolRequest{}
		request.Params.Name = "notify"
		result, err := client.CallTool(ctx, request)
		if err != nil {
			t.Fatalf("CallTool failed: %v", err)
		}

		if len(result.Content) != 1 {
			t.Errorf("Expected 1 content item, got %d", len(result.Content))
		}

		if notificationNum != 1 {
			t.Errorf("Expected 1 notification item, got %d", notificationNum)
		}
	})
}
