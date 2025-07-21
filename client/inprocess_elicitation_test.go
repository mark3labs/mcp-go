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
		Response: mcp.ElicitationResponse{
			Type: mcp.ElicitationResponseTypeAccept,
			Value: map[string]interface{}{
				"response": "User provided data",
				"accepted": true,
			},
		},
	}, nil
}

func TestInProcessElicitation(t *testing.T) {
	// Create server with elicitation enabled
	mcpServer := server.NewMCPServer("test-server", "1.0.0", server.WithElicitation())

	// Add a tool that uses elicitation
	mcpServer.AddTool(mcp.Tool{
		Name:        "test_elicitation",
		Description: "Test elicitation functionality",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"action": map[string]any{
					"type":        "string",
					"description": "Action to perform",
				},
			},
			Required: []string{"action"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		action, err := request.RequireString("action")
		if err != nil {
			return nil, err
		}

		// Create elicitation request
		elicitationRequest := mcp.ElicitationRequest{
			Params: mcp.ElicitationParams{
				Message: "Need additional information for " + action,
				RequestedSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"confirm": map[string]interface{}{
							"type":        "boolean",
							"description": "Confirm the action",
						},
						"details": map[string]interface{}{
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
		switch result.Response.Type {
		case mcp.ElicitationResponseTypeAccept:
			responseText = "User accepted and provided data"
		case mcp.ElicitationResponseTypeDecline:
			responseText = "User declined to provide information"
		case mcp.ElicitationResponseTypeCancel:
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
	client, err := NewInProcessClientWithElicitationHandler(mcpServer, mockHandler)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Start the client
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}

	// Initialize the client
	_, err = client.Initialize(context.Background(), mcp.InitializeRequest{
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
func NewInProcessClientWithElicitationHandler(server *server.MCPServer, handler ElicitationHandler) (*Client, error) {
	// Create a wrapper that implements server.ElicitationHandler
	serverHandler := &inProcessElicitationHandlerWrapper{handler: handler}

	inProcessTransport := transport.NewInProcessTransportWithOptions(server,
		transport.WithElicitationHandler(serverHandler))

	client := NewClient(inProcessTransport)
	client.elicitationHandler = handler

	return client, nil
}

// inProcessElicitationHandlerWrapper wraps client.ElicitationHandler to implement server.ElicitationHandler
type inProcessElicitationHandlerWrapper struct {
	handler ElicitationHandler
}

func (w *inProcessElicitationHandlerWrapper) Elicit(ctx context.Context, request mcp.ElicitationRequest) (*mcp.ElicitationResult, error) {
	return w.handler.Elicit(ctx, request)
}
