package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// TestSamplingIntegration tests end-to-end sampling functionality
func TestSamplingIntegration(t *testing.T) {
	// Create a server with sampling capability
	mcpServer := server.NewMCPServer(
		"sampling-test-server", 
		"1.0.0",
		server.WithSampling(),
	)

	// Add a tool that uses sampling
	mcpServer.AddTool(
		mcp.NewTool(
			"test_sampling",
			mcp.WithDescription("Tests sampling functionality"),
			mcp.WithString("prompt", mcp.Required(), mcp.Description("Prompt to send to LLM")),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			prompt := request.GetString("prompt", "")
			
			// Get sampling context
			samplingCtx := server.SamplingContextFromContext(ctx)
			if samplingCtx == nil {
				return mcp.NewToolResultError("Sampling not available"), nil
			}
			
			// Request analysis from LLM
			result, err := samplingCtx.Sample(ctx, 
				server.StringInput(fmt.Sprintf("Echo this back: %s", prompt)),
				server.WithTemperature(0.0),
				server.WithMaxTokens(100),
			)
			if err != nil {
				return mcp.NewToolResultError("Sampling failed: " + err.Error()), nil
			}
			
			return mcp.NewToolResultText("LLM Response: " + result.Text()), nil
		},
	)

	// Create an in-process transport for testing
	inProcessTransport := transport.NewInProcessTransport(mcpServer)

	// Create a client with a simple sampling handler
	mcpClient := client.NewClient(
		inProcessTransport,
		client.WithSamplingHandler(client.SamplingHandlerFunc(func(ctx context.Context, req *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
			// Simple echo handler for testing
			if len(req.Messages) == 0 {
				return nil, fmt.Errorf("no messages in request")
			}
			
			// Extract the text from the first message
			firstMessage := req.Messages[0]
			textContent, ok := mcp.AsTextContent(firstMessage.Content)
			if !ok {
				return nil, fmt.Errorf("first message is not text content")
			}
			
			// Echo back the text
			return &mcp.CreateMessageResult{
				Role:    mcp.RoleAssistant,
				Content: mcp.NewTextContent("Echoed: " + textContent.Text),
				Model:   "test-model",
			}, nil
		})),
	)

	// Start the client
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := mcpClient.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}
	defer mcpClient.Close()

	// Initialize the client
	_, err = mcpClient.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "sampling-test-client",
				Version: "1.0.0",
			},
			Capabilities: mcp.ClientCapabilities{
				Sampling: &struct{}{}, // Enable sampling capability
			},
		},
	})
	if err != nil {
		t.Fatalf("Failed to initialize client: %v", err)
	}

	// Call the tool that uses sampling
	result, err := mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "test_sampling",
			Arguments: map[string]any{
				"prompt": "Hello, World!",
			},
		},
	})
	if err != nil {
		t.Fatalf("Failed to call tool: %v", err)
	}

	// Verify the result
	if result.Content == nil {
		t.Fatal("Tool result content is nil")
	}

	contents := result.Content.([]mcp.Content); textContent, ok := mcp.AsTextContent(contents[0])
	if !ok {
		t.Logf("Tool result content type: %T", result.Content)
		t.Fatal("Tool result is not text content")
	}
		if len(contents) == 0 {
		t.Fatal("Tool result content is empty")
	}

	expectedText := "LLM Response: Echoed: Echo this back: Hello, World!"
	if textContent.Text != expectedText {
		t.Errorf("Unexpected tool result. Got: %s, Expected: %s", textContent.Text, expectedText)
	}

	t.Log("✅ Sampling integration test passed!")
	t.Logf("Tool result: %s", textContent.Text)
}