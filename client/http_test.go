package client

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
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

			return mcp.NewToolResultText("notification sent successfully"), nil
		},
	)

	addServerToolfunc := func(name string) {
		mcpServer.AddTool(
			mcp.NewTool(name),
			func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				server := server.ServerFromContext(ctx)
				server.SendNotificationToAllClients("helloToEveryone", map[string]any{
					"message": "hello",
				})
				return mcp.NewToolResultText("done"), nil
			},
		)
	}

	testServer := server.NewTestStreamableHTTPServer(mcpServer)
	defer testServer.Close()

	initRequest := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "test-client2",
				Version: "1.0.0",
			},
		},
	}

	t.Run("Can receive notification from server", func(t *testing.T) {
		client, err := NewStreamableHttpClient(testServer.URL)
		if err != nil {
			t.Fatalf("create client failed %v", err)
			return
		}

		notificationNum := make(map[string]int)
		notificationNumMutex := sync.Mutex{}
		client.OnNotification(func(notification mcp.JSONRPCNotification) {
			notificationNumMutex.Lock()
			notificationNum[notification.Method] += 1
			notificationNumMutex.Unlock()
		})

		ctx := context.Background()

		if err := client.Start(ctx); err != nil {
			t.Fatalf("Failed to start client: %v", err)
			return
		}

		// Initialize
		_, err = client.Initialize(ctx, initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v\n", err)
		}

		t.Run("Can receive notifications related to the request", func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Name = "notify"
			result, err := client.CallTool(ctx, request)
			if err != nil {
				t.Fatalf("CallTool failed: %v", err)
			}

			if len(result.Content) != 1 {
				t.Errorf("Expected 1 content item, got %d", len(result.Content))
			}

			if n := notificationNum["notifications/progress"]; n != 1 {
				t.Errorf("Expected 1 progross notification item, got %d", n)
			}
			if n := len(notificationNum); n != 1 {
				t.Errorf("Expected 1 type of notification, got %d", n)
			}
		})

		t.Run("Cannot receive global notifications from server by default", func(t *testing.T) {
			addServerToolfunc("hello1")
			time.Sleep(time.Millisecond * 50)
			if n := notificationNum["hello1"]; n != 0 {
				t.Errorf("Expected 0 notification item, got %d", n)
			}
		})

		t.Run("Can receive global notifications from server when WithContinuousListening enabled", func(t *testing.T) {

			client, err := NewStreamableHttpClient(testServer.URL,
				transport.WithContinuousListening())
			if err != nil {
				t.Fatalf("create client failed %v", err)
				return
			}
			defer client.Close()

			notificationNum := make(map[string]int)
			notificationNumMutex := sync.Mutex{}
			client.OnNotification(func(notification mcp.JSONRPCNotification) {
				notificationNumMutex.Lock()
				println(notification.Method)
				notificationNum[notification.Method] += 1
				notificationNumMutex.Unlock()
			})

			ctx := context.Background()

			if err := client.Start(ctx); err != nil {
				t.Fatalf("Failed to start client: %v", err)
				return
			}

			// Initialize
			_, err = client.Initialize(ctx, initRequest)
			if err != nil {
				t.Fatalf("Failed to initialize: %v\n", err)
			}

			// can receive normal notification
			request := mcp.CallToolRequest{}
			request.Params.Name = "notify"
			_, err = client.CallTool(ctx, request)
			if err != nil {
				t.Fatalf("CallTool failed: %v", err)
			}

			if n := notificationNum["notifications/progress"]; n != 1 {
				t.Errorf("Expected 1 progross notification item, got %d", n)
			}
			if n := len(notificationNum); n != 1 {
				t.Errorf("Expected 1 type of notification, got %d", n)
			}

			// can receive global notification
			addServerToolfunc("hello2")
			time.Sleep(time.Millisecond * 50) // wait for the notification to be sent as upper action is async
			if n := notificationNum["notifications/tools/list_changed"]; n != 1 {
				t.Errorf("Expected 1 notification item, got %d, %v", n, notificationNum)
			}
		})

	})
}
