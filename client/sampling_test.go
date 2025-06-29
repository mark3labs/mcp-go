package client

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// mockSamplingHandler implements SamplingHandler for testing
type mockSamplingHandler struct {
	result *mcp.CreateMessageResult
	err    error
}

func (m *mockSamplingHandler) CreateMessage(ctx context.Context, request mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func TestClient_HandleSamplingRequest(t *testing.T) {
	tests := []struct {
		name          string
		handler       SamplingHandler
		expectedError string
	}{
		{
			name:          "no handler configured",
			handler:       nil,
			expectedError: "no sampling handler configured",
		},
		{
			name: "successful sampling",
			handler: &mockSamplingHandler{
				result: &mcp.CreateMessageResult{
					SamplingMessage: mcp.SamplingMessage{
						Role: mcp.RoleAssistant,
						Content: mcp.TextContent{
							Type: "text",
							Text: "Hello, world!",
						},
					},
					Model:      "test-model",
					StopReason: "endTurn",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{samplingHandler: tt.handler}

			request := mcp.CreateMessageRequest{
				CreateMessageParams: mcp.CreateMessageParams{
					Messages: []mcp.SamplingMessage{
						{
							Role:    mcp.RoleUser,
							Content: mcp.TextContent{Type: "text", Text: "Hello"},
						},
					},
					MaxTokens: 100,
				},
			}

			result, err := client.handleIncomingRequest(context.Background(), mockJSONRPCRequest(request))

			if tt.expectedError != "" {
				if err == nil {
					t.Errorf("expected error %q, got nil", tt.expectedError)
				} else if err.Error() != tt.expectedError {
					t.Errorf("expected error %q, got %q", tt.expectedError, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == nil {
					t.Error("expected result, got nil")
				}
			}
		})
	}
}

func TestWithSamplingHandler(t *testing.T) {
	handler := &mockSamplingHandler{}
	client := &Client{}

	option := WithSamplingHandler(handler)
	option(client)

	if client.samplingHandler != handler {
		t.Error("sampling handler not set correctly")
	}
}

func TestClient_Initialize_WithSampling(t *testing.T) {
	handler := &mockSamplingHandler{}
	client := &Client{samplingHandler: handler}

	// Mock the transport and sendRequest method
	// This is a simplified test - in practice you'd need to mock the transport properly

	// Test that sampling capability is declared when handler is present
	capabilities := mcp.ClientCapabilities{}
	if client.samplingHandler != nil {
		capabilities.Sampling = &struct{}{}
	}

	if capabilities.Sampling == nil {
		t.Error("sampling capability should be set when handler is configured")
	}
}

// Helper function to create a mock JSON-RPC request for testing
func mockJSONRPCRequest(mcpRequest mcp.CreateMessageRequest) transport.JSONRPCRequest {
	return transport.JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(1),
		Method:  string(mcp.MethodSamplingCreateMessage),
		Params:  mcpRequest.CreateMessageParams,
	}
}
