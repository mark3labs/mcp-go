package client

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
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

	t.Run("Can Configure a server with a pre-existing session", func(t *testing.T) {
		sessionID := uuid.NewString()
		client, err := NewStreamableHttpClient(testServer.URL, transport.WithSession(sessionID))
		if err != nil {
			t.Fatalf("create client failed %v", err)
		}
		if client.IsInitialized() != true {
			t.Fatalf("Client is not initialized")
		}
	})

	t.Run("Can receive notification from server", func(t *testing.T) {
		client, err := NewStreamableHttpClient(testServer.URL)
		if err != nil {
			t.Fatalf("create client failed %v", err)
			return
		}

		notificationNum := NewSafeMap()
		client.OnNotification(func(notification mcp.JSONRPCNotification) {
			notificationNum.Increment(notification.Method)
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

			if n := notificationNum.Get("notifications/progress"); n != 1 {
				t.Errorf("Expected 1 progross notification item, got %d", n)
			}
			if n := notificationNum.Len(); n != 1 {
				t.Errorf("Expected 1 type of notification, got %d", n)
			}
		})

		t.Run("Can not receive global notifications from server by default", func(t *testing.T) {
			addServerToolfunc("hello1")
			time.Sleep(time.Millisecond * 50)

			helloNotifications := notificationNum.Get("hello1")
			if helloNotifications != 0 {
				t.Errorf("Expected 0 notification item, got %d", helloNotifications)
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

			notificationNum := NewSafeMap()
			client.OnNotification(func(notification mcp.JSONRPCNotification) {
				notificationNum.Increment(notification.Method)
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

			if n := notificationNum.Get("notifications/progress"); n != 1 {
				t.Errorf("Expected 1 progross notification item, got %d", n)
			}
			if n := notificationNum.Len(); n != 1 {
				t.Errorf("Expected 1 type of notification, got %d", n)
			}

			// can receive global notification
			addServerToolfunc("hello2")
			time.Sleep(time.Millisecond * 50) // wait for the notification to be sent as upper action is async

			n := notificationNum.Get("notifications/tools/list_changed")
			if n != 1 {
				t.Errorf("Expected 1 notification item, got %d, %v", n, notificationNum)
			}
		})

	})
}

type SafeMap struct {
	mu   sync.RWMutex
	data map[string]int
}

func NewSafeMap() *SafeMap {
	return &SafeMap{
		data: make(map[string]int),
	}
}

func (sm *SafeMap) Increment(key string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.data[key]++
}

func (sm *SafeMap) Get(key string) int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.data[key]
}

func (sm *SafeMap) Len() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.data)
}
