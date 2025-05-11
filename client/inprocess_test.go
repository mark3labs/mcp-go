package client

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
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
					Text: "Input parameter: " + request.Params.Arguments["parameter-1"].(string),
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
		if err := client.Start(context.Background()); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		result, err := client.Initialize(context.Background(), initRequest)
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
		if err := client.Ping(context.Background()); err != nil {
			t.Errorf("Ping failed: %v", err)
		}

		// Test ListTools
		toolsRequest := mcp.ListToolsRequest{}
		toolListResult, err := client.ListTools(context.Background(), toolsRequest)
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

		if err := client.Start(context.Background()); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Try to make a request without initializing
		toolsRequest := mcp.ListToolsRequest{}
		_, err = client.ListTools(context.Background(), toolsRequest)
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

		if err := client.Start(context.Background()); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		_, err = client.Initialize(context.Background(), initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}

		request := mcp.CallToolRequest{}
		request.Params.Name = "test-tool"
		request.Params.Arguments = map[string]any{
			"parameter-1": "value1",
		}

		result, err := client.CallTool(context.Background(), request)
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

		if err := client.Start(context.Background()); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		_, err = client.Initialize(context.Background(), initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}

		err = client.Ping(context.Background())
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

		if err := client.Start(context.Background()); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		_, err = client.Initialize(context.Background(), initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}

		request := mcp.ListResourcesRequest{}
		result, err := client.ListResources(context.Background(), request)
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

		if err := client.Start(context.Background()); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		_, err = client.Initialize(context.Background(), initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}

		request := mcp.ReadResourceRequest{}
		request.Params.URI = "resource://testresource"

		result, err := client.ReadResource(context.Background(), request)
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

		if err := client.Start(context.Background()); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		_, err = client.Initialize(context.Background(), initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}
		request := mcp.ListPromptsRequest{}
		result, err := client.ListPrompts(context.Background(), request)
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

		if err := client.Start(context.Background()); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		_, err = client.Initialize(context.Background(), initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}

		request := mcp.GetPromptRequest{}
		request.Params.Name = "test-prompt"
		request.Params.Arguments = map[string]string{
			"arg1": "arg1 value",
		}

		result, err := client.GetPrompt(context.Background(), request)
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

		if err := client.Start(context.Background()); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		_, err = client.Initialize(context.Background(), initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}

		request := mcp.ListToolsRequest{}
		result, err := client.ListTools(context.Background(), request)
		if err != nil {
			t.Errorf("ListTools failed: %v", err)
		}

		if len(result.Tools) != 1 {
			t.Errorf("Expected 1 tool, got %d", len(result.Tools))
		}
	})
}
