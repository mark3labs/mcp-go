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
