package client

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
)

func TestInProcessMCPClient(t *testing.T) {
	mcpServer := server.NewMCPServer(
		"test-server",
		"1.0.0",
		server.WithResourceCapabilities(true, true),
		server.WithPromptCapabilities(true),
		server.WithToolCapabilities(true),
	)

	// Add a test tool
	mcpServer.AddTool(mcp.NewTool(
		"test-tool",
		mcp.WithDescription("Test tool"),
		mcp.WithString("parameter-1", mcp.Description("A string tool parameter")),
		mcp.WithTitleAnnotation("Test Tool Annotation Title"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "Input parameter: " + request.GetArguments()["parameter-1"].(string),
				},
				mcp.AudioContent{
					Type:     "audio",
					Data:     "base64-encoded-audio-data",
					MIMEType: "audio/wav",
				},
			},
		}, nil
	})

	mcpServer.AddResource(
		mcp.Resource{
			URI:  "resource://testresource",
			Name: "My Resource",
		},
		func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      "resource://testresource",
					MIMEType: "text/plain",
					Text:     "test content",
				},
			}, nil
		},
	)

	mcpServer.AddPrompt(
		mcp.Prompt{
			Name:        "test-prompt",
			Description: "A test prompt",
			Arguments: []mcp.PromptArgument{
				{
					Name:        "arg1",
					Description: "First argument",
				},
			},
		},
		func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return &mcp.GetPromptResult{
				Messages: []mcp.PromptMessage{
					{
						Role: mcp.RoleAssistant,
						Content: mcp.TextContent{
							Type: "text",
							Text: "Test prompt with arg1: " + request.Params.Arguments["arg1"],
						},
					},
					{
						Role: mcp.RoleUser,
						Content: mcp.AudioContent{
							Type:     "audio",
							Data:     "base64-encoded-audio-data",
							MIMEType: "audio/wav",
						},
					},
				},
			}, nil
		},
	)

	t.Run("Can initialize and make requests", func(t *testing.T) {
		client, err := NewInProcessClient(mcpServer)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		// Start the client
		if err := client.Start(t.Context()); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		result, err := client.Initialize(t.Context(), initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}

		if result.ServerInfo.Name != "test-server" {
			t.Errorf(
				"Expected server name 'test-server', got '%s'",
				result.ServerInfo.Name,
			)
		}

		// Test Ping
		if err := client.Ping(t.Context()); err != nil {
			t.Errorf("Ping failed: %v", err)
		}

		// Test ListTools
		toolsRequest := mcp.ListToolsRequest{}
		toolListResult, err := client.ListTools(t.Context(), toolsRequest)
		if err != nil {
			t.Errorf("ListTools failed: %v", err)
		}
		if toolListResult == nil || len((*toolListResult).Tools) == 0 {
			t.Errorf("Expected one tool")
		}
		testToolAnnotations := (*toolListResult).Tools[0].Annotations
		if testToolAnnotations.Title != "Test Tool Annotation Title" ||
			*testToolAnnotations.ReadOnlyHint != true ||
			*testToolAnnotations.DestructiveHint != false ||
			*testToolAnnotations.IdempotentHint != true ||
			*testToolAnnotations.OpenWorldHint != false {
			t.Errorf("The annotations of the tools are invalid")
		}
	})

	t.Run("Handles errors properly", func(t *testing.T) {
		client, err := NewInProcessClient(mcpServer)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		if err := client.Start(t.Context()); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Try to make a request without initializing
		toolsRequest := mcp.ListToolsRequest{}
		_, err = client.ListTools(t.Context(), toolsRequest)
		if err == nil {
			t.Error("Expected error when making request before initialization")
		}
	})

	t.Run("CallTool", func(t *testing.T) {
		client, err := NewInProcessClient(mcpServer)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		if err := client.Start(t.Context()); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		_, err = client.Initialize(t.Context(), initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}

		request := mcp.CallToolRequest{}
		request.Params.Name = "test-tool"
		request.Params.Arguments = map[string]any{
			"parameter-1": "value1",
		}

		result, err := client.CallTool(t.Context(), request)
		if err != nil {
			t.Fatalf("CallTool failed: %v", err)
		}

		if len(result.Content) != 2 {
			t.Errorf("Expected 2 content item, got %d", len(result.Content))
		}
	})

	t.Run("Ping", func(t *testing.T) {
		client, err := NewInProcessClient(mcpServer)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		if err := client.Start(t.Context()); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		_, err = client.Initialize(t.Context(), initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}

		err = client.Ping(t.Context())
		if err != nil {
			t.Errorf("Ping failed: %v", err)
		}
	})

	t.Run("ListResources", func(t *testing.T) {
		client, err := NewInProcessClient(mcpServer)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		if err := client.Start(t.Context()); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		_, err = client.Initialize(t.Context(), initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}

		request := mcp.ListResourcesRequest{}
		result, err := client.ListResources(t.Context(), request)
		if err != nil {
			t.Errorf("ListResources failed: %v", err)
		}

		if len(result.Resources) != 1 {
			t.Errorf("Expected 1 resource, got %d", len(result.Resources))
		}
	})

	t.Run("ReadResource", func(t *testing.T) {
		client, err := NewInProcessClient(mcpServer)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		if err := client.Start(t.Context()); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		_, err = client.Initialize(t.Context(), initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}

		request := mcp.ReadResourceRequest{}
		request.Params.URI = "resource://testresource"

		result, err := client.ReadResource(t.Context(), request)
		if err != nil {
			t.Errorf("ReadResource failed: %v", err)
		}

		if len(result.Contents) != 1 {
			t.Errorf("Expected 1 content item, got %d", len(result.Contents))
		}
	})

	t.Run("ListPrompts", func(t *testing.T) {
		client, err := NewInProcessClient(mcpServer)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		if err := client.Start(t.Context()); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		_, err = client.Initialize(t.Context(), initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}
		request := mcp.ListPromptsRequest{}
		result, err := client.ListPrompts(t.Context(), request)
		if err != nil {
			t.Errorf("ListPrompts failed: %v", err)
		}

		if len(result.Prompts) != 1 {
			t.Errorf("Expected 1 prompt, got %d", len(result.Prompts))
		}
	})

	t.Run("GetPrompt", func(t *testing.T) {
		client, err := NewInProcessClient(mcpServer)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		if err := client.Start(t.Context()); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		_, err = client.Initialize(t.Context(), initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}

		request := mcp.GetPromptRequest{}
		request.Params.Name = "test-prompt"
		request.Params.Arguments = map[string]string{
			"arg1": "arg1 value",
		}

		result, err := client.GetPrompt(t.Context(), request)
		if err != nil {
			t.Errorf("GetPrompt failed: %v", err)
		}

		if len(result.Messages) != 2 {
			t.Errorf("Expected 2 message, got %d", len(result.Messages))
		}
	})

	t.Run("ListTools", func(t *testing.T) {
		client, err := NewInProcessClient(mcpServer)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		if err := client.Start(t.Context()); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		_, err = client.Initialize(t.Context(), initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}

		request := mcp.ListToolsRequest{}
		result, err := client.ListTools(t.Context(), request)
		if err != nil {
			t.Errorf("ListTools failed: %v", err)
		}

		if len(result.Tools) != 1 {
			t.Errorf("Expected 1 tool, got %d", len(result.Tools))
		}
	})
}

// TestInProcessMCPClient_Notifications verifies that server-to-client
// notifications sent during a tool call (e.g. notifications/progress) are
// actually delivered to the in-process client's registered handler. This is
// the round-trip regression test for the in-process transport's
// notification-forwarding goroutine.
func TestInProcessMCPClient_Notifications(t *testing.T) {
	mcpServer := server.NewMCPServer(
		"test-server",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	mcpServer.AddTool(
		mcp.NewTool("notify"),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			srv := server.ServerFromContext(ctx)
			err := srv.SendNotificationToClient(
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

	client, err := NewInProcessClient(mcpServer)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	received := make(chan mcp.JSONRPCNotification, 1)
	client.OnNotification(func(notification mcp.JSONRPCNotification) {
		received <- notification
	})

	if err := client.Start(t.Context()); err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}

	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "test-client",
		Version: "1.0.0",
	}
	if _, err := client.Initialize(t.Context(), initRequest); err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	request := mcp.CallToolRequest{}
	request.Params.Name = "notify"
	if _, err := client.CallTool(t.Context(), request); err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	select {
	case notification := <-received:
		if notification.Method != "notifications/progress" {
			t.Errorf("Expected notifications/progress, got %s", notification.Method)
		}
		progress, ok := notification.Params.AdditionalFields["progress"]
		if !ok {
			t.Errorf("Expected progress field in notification params")
		}
		if fmt.Sprintf("%v", progress) != "10" {
			t.Errorf("Expected progress 10, got %v", progress)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for notification from in-process transport")
	}
}

func TestInProcessMCPClient_SubscriptionFilter(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		listen   bool
		wantList []string
	}{
		{
			name:     "modern tools subscription",
			version:  mcp.ProtocolVersion20260728,
			listen:   true,
			wantList: []string{mcp.MethodNotificationToolsListChanged},
		},
		{
			name:    "legacy without subscription",
			version: mcp.ProtocolVersion20251125,
			wantList: []string{
				mcp.MethodNotificationPromptsListChanged,
				mcp.MethodNotificationResourcesListChanged,
				"custom/broadcast",
				mcp.MethodNotificationToolsListChanged,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			srv := server.NewMCPServer("subscription-test", "1.0.0",
				server.WithToolCapabilities(true),
				server.WithPromptCapabilities(true),
				server.WithResourceCapabilities(true, true),
			)
			client, err := NewInProcessClient(srv)
			require.NoError(t, err)
			defer client.Close()

			received := make(chan mcp.JSONRPCNotification, 8)
			client.OnNotification(func(notification mcp.JSONRPCNotification) {
				received <- notification
			})
			require.NoError(t, client.Start(ctx))
			request := mcp.InitializeRequest{}
			request.Params.ProtocolVersion = tt.version
			request.Params.ClientInfo = mcp.Implementation{Name: "subscription-client", Version: "1.0.0"}
			_, err = client.Initialize(ctx, request)
			require.NoError(t, err)
			require.Equal(t, tt.version, client.ProtocolVersion())

			next := func() mcp.JSONRPCNotification {
				t.Helper()
				select {
				case notification := <-received:
					return notification
				case <-ctx.Done():
					t.Fatal("timed out waiting for notification")
					return mcp.JSONRPCNotification{}
				}
			}
			var subscriptionID any
			if tt.listen {
				listenCtx, stop := context.WithCancel(ctx)
				done := make(chan error, 1)
				go func() {
					done <- client.Listen(listenCtx, mcp.SubscriptionFilter{ToolsListChanged: true})
				}()
				defer func() {
					stop()
					select {
					case err := <-done:
						require.NoError(t, err)
					case <-ctx.Done():
						t.Error("subscription did not close")
					}
				}()
				ack := next()
				require.Equal(t, mcp.MethodNotificationSubscriptionsAcknowledged, ack.Method)
				subscriptionID = ack.Params.Meta[mcp.MetaKeySubscriptionID]
				require.NotNil(t, subscriptionID)
			}

			srv.AddPrompt(mcp.NewPrompt("new-prompt"),
				func(context.Context, mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
					return mcp.NewGetPromptResult("", nil), nil
				})
			srv.AddResource(mcp.NewResource("file:///new-resource", "new-resource"),
				func(context.Context, mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
					return []mcp.ResourceContents{mcp.TextResourceContents{URI: "file:///new-resource", Text: "content"}}, nil
				})
			srv.SendNotificationToAllClients("custom/broadcast", nil)
			srv.AddTool(mcp.NewTool("new-tool"),
				func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return mcp.NewToolResultText("result"), nil
				})

			var methods []string
			for {
				notification := next()
				require.Equal(t, subscriptionID, notification.Params.Meta[mcp.MetaKeySubscriptionID])
				methods = append(methods, notification.Method)
				// All broadcasts share the session's FIFO queue. This last,
				// opted-in notification is the barrier for the preceding ones.
				if notification.Method == mcp.MethodNotificationToolsListChanged {
					break
				}
			}
			require.Equal(t, tt.wantList, methods)
		})
	}
}

func TestInProcessMCPClient_ReplacedSubscriptionKeepsItsFilter(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	srv := server.NewMCPServer("subscription-replacement", "1.0.0",
		server.WithToolCapabilities(true),
		server.WithPromptCapabilities(true),
	)
	client, err := NewInProcessClient(srv)
	require.NoError(t, err)
	defer client.Close()

	received := make(chan mcp.JSONRPCNotification, 8)
	client.OnNotification(func(notification mcp.JSONRPCNotification) {
		received <- notification
	})
	require.NoError(t, client.Start(ctx))
	request := mcp.InitializeRequest{}
	request.Params.ProtocolVersion = mcp.ProtocolVersion20260728
	request.Params.ClientInfo = mcp.Implementation{Name: "replacement-client", Version: "1.0.0"}
	_, err = client.Initialize(ctx, request)
	require.NoError(t, err)

	next := func() mcp.JSONRPCNotification {
		t.Helper()
		select {
		case notification := <-received:
			return notification
		case <-ctx.Done():
			t.Fatal("timed out waiting for notification")
			return mcp.JSONRPCNotification{}
		}
	}
	filter := mcp.SubscriptionFilter{ToolsListChanged: true}
	oldCtx, cancelOld := context.WithCancel(ctx)
	oldDone := make(chan error, 1)
	go func() {
		oldDone <- client.Listen(oldCtx, filter)
		close(oldDone)
	}()
	defer func() {
		cancelOld()
		select {
		case <-oldDone:
		case <-ctx.Done():
			t.Error("old subscription did not close")
		}
	}()
	oldAck := next()
	require.Equal(t, mcp.MethodNotificationSubscriptionsAcknowledged, oldAck.Method)

	// Send the replacement through the real client request path so the two RPCs
	// overlap until we cancel the old one after the replacement is acknowledged.
	newCtx, cancelNew := context.WithCancel(ctx)
	newDone := make(chan error, 1)
	go func() {
		_, err := client.sendRequest(newCtx, string(mcp.MethodSubscriptionsListen),
			mcp.SubscriptionsListenParams{Notifications: filter}, nil)
		newDone <- err
		close(newDone)
	}()
	defer func() {
		cancelNew()
		select {
		case <-newDone:
		case <-ctx.Done():
			t.Error("replacement subscription did not close")
		}
	}()
	newAck := next()
	require.Equal(t, mcp.MethodNotificationSubscriptionsAcknowledged, newAck.Method)
	require.NotEqual(t, oldAck.Params.Meta[mcp.MetaKeySubscriptionID], newAck.Params.Meta[mcp.MetaKeySubscriptionID])
	cancelOld()
	select {
	case err := <-oldDone:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("old subscription did not close")
	}

	// The opted-in tools notification is the FIFO barrier: an old cleanup
	// that clears the replacement filter lets the preceding prompt leak through.
	srv.SendNotificationToAllClients(mcp.MethodNotificationPromptsListChanged, nil)
	srv.SendNotificationToAllClients(mcp.MethodNotificationToolsListChanged, nil)
	notification := next()
	require.Equal(t, mcp.MethodNotificationToolsListChanged, notification.Method,
		"the old subscription must not clear the replacement's filter")
	require.Equal(t, newAck.Params.Meta[mcp.MetaKeySubscriptionID], notification.Params.Meta[mcp.MetaKeySubscriptionID])
}

// TestInProcessMCPClient_CloseStopsNotificationForwarding verifies that
// Close() stops the notification-forwarding goroutine (no leak) and is
// safe to call more than once.
func TestInProcessMCPClient_CloseStopsNotificationForwarding(t *testing.T) {
	mcpServer := server.NewMCPServer("test-server", "1.0.0")

	const cycles = 5
	before := runtime.NumGoroutine()

	for range cycles {
		client, err := NewInProcessClient(mcpServer)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}

		if err := client.Start(t.Context()); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		if err := client.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		// Closing a second time must not panic and must not double-close
		// the done channel.
		if err := client.Close(); err != nil {
			t.Fatalf("Second Close failed: %v", err)
		}
	}

	require.Eventually(t, func() bool {
		runtime.Gosched()
		return runtime.NumGoroutine()-before <= 1
	}, 2*time.Second, 20*time.Millisecond, "notification-forwarding goroutine leaked after Close")
}

// TestInProcessMCPClient_CloseBeforeStart verifies that calling Close()
// before Start() does not leak the notification-forwarding goroutine. Prior
// to the fix, Close() called before Start() would find a nil done channel
// (created lazily in Start()) and no-op, but still consume the sync.Once —
// so a subsequent Start() would spawn forwardNotifications with no way to
// ever stop it.
func TestInProcessMCPClient_CloseBeforeStart(t *testing.T) {
	mcpServer := server.NewMCPServer("test-server", "1.0.0")

	const cycles = 5
	before := runtime.NumGoroutine()

	for range cycles {
		client, err := NewInProcessClient(mcpServer)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}

		// Close before Start.
		if err := client.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		// Start after Close should not spawn a forwarding goroutine that
		// can never be stopped, and must report the documented
		// ErrTransportClosed contract.
		if err := client.Start(t.Context()); !errors.Is(err, transport.ErrTransportClosed) {
			t.Fatalf("expected ErrTransportClosed from Start after Close, got %v", err)
		}
	}

	require.Eventually(t, func() bool {
		runtime.Gosched()
		return runtime.NumGoroutine()-before <= 1
	}, 2*time.Second, 20*time.Millisecond, "notification-forwarding goroutine leaked after Close-before-Start")
}
