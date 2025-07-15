package client

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// MockSamplingHandler implements SamplingHandler for testing
type MockSamplingHandler struct{}

func (h *MockSamplingHandler) CreateMessage(ctx context.Context, request mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	return &mcp.CreateMessageResult{
		SamplingMessage: mcp.SamplingMessage{
			Role: mcp.RoleAssistant,
			Content: mcp.TextContent{
				Type: "text",
				Text: "Mock response from sampling handler",
			},
		},
		Model:      "mock-model",
		StopReason: "endTurn",
	}, nil
}

func TestInProcessSampling(t *testing.T) {
	// Create server with sampling enabled
	mcpServer := server.NewMCPServer("test-server", "1.0.0")
	mcpServer.EnableSampling()

	// Add a tool that uses sampling
	mcpServer.AddTool(mcp.Tool{
		Name:        "test_sampling",
		Description: "Test sampling functionality",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"message": map[string]any{
					"type":        "string",
					"description": "Message to send to LLM",
				},
			},
			Required: []string{"message"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		message, err := request.RequireString("message")
		if err != nil {
			return nil, err
		}

		// Create sampling request
		samplingRequest := mcp.CreateMessageRequest{
			CreateMessageParams: mcp.CreateMessageParams{
				Messages: []mcp.SamplingMessage{
					{
						Role: mcp.RoleUser,
						Content: mcp.TextContent{
							Type: "text",
							Text: message,
						},
					},
				},
				MaxTokens:   100,
				Temperature: 0.7,
			},
		}

		// Request sampling from client
		result, err := mcpServer.RequestSampling(ctx, samplingRequest)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: "Sampling failed: " + err.Error(),
					},
				},
				IsError: true,
			}, nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "Sampling result: " + result.Content.(mcp.TextContent).Text,
				},
			},
		}, nil
	})

	// Create client with sampling handler
	mockHandler := &MockSamplingHandler{}
	client, err := NewInProcessClientWithSamplingHandler(mcpServer, mockHandler)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Start the client
	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("Failed to start client: %v", err)
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
		t.Fatalf("Failed to initialize: %v", err)
	}

	// Call the tool that uses sampling
	result, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "test_sampling",
			Arguments: map[string]any{
				"message": "Hello, world!",
			},
		},
	})
	if err != nil {
		t.Fatalf("Tool call failed: %v", err)
	}

	// Verify the result contains the mock response
	if len(result.Content) == 0 {
		t.Fatal("Expected content in result")
	}

	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatal("Expected text content")
	}

	expectedText := "Sampling result: Mock response from sampling handler"
	if textContent.Text != expectedText {
		t.Errorf("Expected %q, got %q", expectedText, textContent.Text)
	}
}
