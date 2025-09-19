package client

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// MockElicitationHandler implements ElicitationHandler for testing
type MockElicitationHandler struct {
	// Track calls for verification
	CallCount   int
	LastRequest mcp.ElicitationRequest
}

func (h *MockElicitationHandler) Elicit(ctx context.Context, request mcp.ElicitationRequest) (*mcp.ElicitationResult, error) {
	h.CallCount++
	h.LastRequest = request

	// Simulate user accepting and providing data
	return &mcp.ElicitationResult{
		ElicitationResponse: mcp.ElicitationResponse{
			Action: mcp.ElicitationResponseActionAccept,
			Content: map[string]any{
				"confirm": true,
				"details": "User provided additional details",
			},
		},
	}, nil
}

func TestInProcessElicitation(t *testing.T) {
	// Create server with elicitation enabled
	mcpServer := server.NewMCPServer("test-server", "1.0.0", server.WithElicitation())

	// Add a tool that uses elicitation
	mcpServer.AddTool(mcp.NewTool(
		"test_elicitation",
		mcp.WithDescription("Test elicitation functionality"),
		mcp.WithString("action", mcp.Description("Action to perform"), mcp.Required()),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		action, err := request.RequireString("action")
		if err != nil {
			return nil, err
		}

		// Create elicitation request
		elicitationRequest := mcp.ElicitationRequest{
			Params: mcp.ElicitationParams{
				Message: "Need additional information for " + action,
				RequestedSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"confirm": map[string]any{
							"type":        "boolean",
							"description": "Confirm the action",
						},
						"details": map[string]any{
							"type":        "string",
							"description": "Additional details",
						},
					},
					"required": []string{"confirm"},
				},
			},
		}

		// Request elicitation from client
		result, err := mcpServer.RequestElicitation(ctx, elicitationRequest)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: "Elicitation failed: " + err.Error(),
					},
				},
				IsError: true,
			}, nil
		}

		// Handle the response
		var responseText string
		switch result.Action {
		case mcp.ElicitationResponseActionAccept:
			responseText = "User accepted and provided data"
		case mcp.ElicitationResponseActionDecline:
			responseText = "User declined to provide information"
		case mcp.ElicitationResponseActionCancel:
			responseText = "User cancelled the request"
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: responseText,
				},
			},
		}, nil
	})

	// Create handler for elicitation
	mockHandler := &MockElicitationHandler{}

	// Create in-process client with elicitation handler
	client := NewInProcessClientWithElicitationHandler(mcpServer, mockHandler)
	defer client.Close()

	// Start the client
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}

	// Initialize the client
	_, err := client.Initialize(context.Background(), mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "test-client",
				Version: "1.0.0",
			},
			Capabilities: mcp.ClientCapabilities{
				Elicitation: &struct{}{},
			},
		},
	})
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	// Call the tool that triggers elicitation
	result, err := client.CallTool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "test_elicitation",
			Arguments: map[string]any{
				"action": "test-action",
			},
		},
	})

	if err != nil {
		t.Fatalf("Failed to call tool: %v", err)
	}

	// Verify the result
	if len(result.Content) == 0 {
		t.Fatal("Expected content in result")
	}

	// Assert that the result is not flagged as an error for the accept path
	if result.IsError {
		t.Error("Expected result to not be flagged as error for accept response")
	}

	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatal("Expected text content")
	}

	if textContent.Text != "User accepted and provided data" {
		t.Errorf("Unexpected result: %s", textContent.Text)
	}

	// Verify the handler was called
	if mockHandler.CallCount != 1 {
		t.Errorf("Expected handler to be called once, got %d", mockHandler.CallCount)
	}

	if mockHandler.LastRequest.Params.Message != "Need additional information for test-action" {
		t.Errorf("Unexpected elicitation message: %s", mockHandler.LastRequest.Params.Message)
	}
}

// NewInProcessClientWithElicitationHandler creates an in-process client with elicitation support
func NewInProcessClientWithElicitationHandler(server *server.MCPServer, handler ElicitationHandler) *Client {
	// Create a wrapper that implements server.ElicitationHandler
	serverHandler := &inProcessElicitationHandlerWrapper{handler: handler}

	inProcessTransport := transport.NewInProcessTransportWithOptions(server,
		transport.WithElicitationHandler(serverHandler))

	client := NewClient(inProcessTransport)

	return client
}

// inProcessElicitationHandlerWrapper wraps client.ElicitationHandler to implement server.ElicitationHandler
type inProcessElicitationHandlerWrapper struct {
	handler ElicitationHandler
}

func (w *inProcessElicitationHandlerWrapper) Elicit(ctx context.Context, request mcp.ElicitationRequest) (*mcp.ElicitationResult, error) {
	return w.handler.Elicit(ctx, request)
}
