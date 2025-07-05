package client

import (
	"context"
	"encoding/json"
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

// mockTransport implements transport.Interface for testing
type mockTransport struct {
	requestChan  chan transport.JSONRPCRequest
	responseChan chan *transport.JSONRPCResponse
	started      bool
}

func newMockTransport() *mockTransport {
	return &mockTransport{
		requestChan:  make(chan transport.JSONRPCRequest, 1),
		responseChan: make(chan *transport.JSONRPCResponse, 1),
	}
}

func (m *mockTransport) Start(ctx context.Context) error {
	m.started = true
	return nil
}

func (m *mockTransport) SendRequest(ctx context.Context, request transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	m.requestChan <- request
	select {
	case response := <-m.responseChan:
		return response, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *mockTransport) SendNotification(ctx context.Context, notification mcp.JSONRPCNotification) error {
	return nil
}

func (m *mockTransport) SetNotificationHandler(handler func(notification mcp.JSONRPCNotification)) {
}

func (m *mockTransport) Close() error {
	return nil
}

func (m *mockTransport) GetSessionId() string {
	return "mock-session-id"
}

func TestClient_Initialize_WithSampling(t *testing.T) {
	handler := &mockSamplingHandler{
		result: &mcp.CreateMessageResult{
			SamplingMessage: mcp.SamplingMessage{
				Role: mcp.RoleAssistant,
				Content: mcp.TextContent{
					Type: "text",
					Text: "Test response",
				},
			},
			Model:      "test-model",
			StopReason: "endTurn",
		},
	}

	// Create mock transport
	mockTransport := newMockTransport()

	// Create client with sampling handler and mock transport
	client := &Client{
		transport:       mockTransport,
		samplingHandler: handler,
	}

	// Start the client
	ctx := context.Background()
	err := client.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}

	// Prepare mock response for initialization
	initResponse := &transport.JSONRPCResponse{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(1),
		Result:  []byte(`{"protocolVersion":"2024-11-05","capabilities":{"logging":{},"prompts":{},"resources":{},"tools":{}},"serverInfo":{"name":"test-server","version":"1.0.0"}}`),
	}

	// Send the response in a goroutine
	go func() {
		mockTransport.responseChan <- initResponse
	}()

	// Call Initialize with appropriate parameters
	initRequest := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "test-client",
				Version: "1.0.0",
			},
			Capabilities: mcp.ClientCapabilities{
				Roots: &struct {
					ListChanged bool `json:"listChanged,omitempty"`
				}{
					ListChanged: true,
				},
			},
		},
	}

	result, err := client.Initialize(ctx, initRequest)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Verify the result
	if result == nil {
		t.Fatal("Initialize result should not be nil")
	}

	// Verify that the request was sent through the transport
	select {
	case request := <-mockTransport.requestChan:
		// Verify the request method
		if request.Method != "initialize" {
			t.Errorf("Expected method 'initialize', got '%s'", request.Method)
		}

		// Verify the request has the correct structure
		if request.Params == nil {
			t.Fatal("Request params should not be nil")
		}

		// Parse the params to verify sampling capability is included
		paramsBytes, err := json.Marshal(request.Params)
		if err != nil {
			t.Fatalf("Failed to marshal request params: %v", err)
		}

		var params struct {
			ProtocolVersion string                 `json:"protocolVersion"`
			ClientInfo      mcp.Implementation     `json:"clientInfo"`
			Capabilities    mcp.ClientCapabilities `json:"capabilities"`
		}

		err = json.Unmarshal(paramsBytes, &params)
		if err != nil {
			t.Fatalf("Failed to unmarshal request params: %v", err)
		}

		// Verify sampling capability is included in the request
		if params.Capabilities.Sampling == nil {
			t.Error("Sampling capability should be included in initialization request when handler is configured")
		}

		// Verify other expected fields
		if params.ProtocolVersion != mcp.LATEST_PROTOCOL_VERSION {
			t.Errorf("Expected protocol version '%s', got '%s'", mcp.LATEST_PROTOCOL_VERSION, params.ProtocolVersion)
		}

		if params.ClientInfo.Name != "test-client" {
			t.Errorf("Expected client name 'test-client', got '%s'", params.ClientInfo.Name)
		}

	default:
		t.Error("Expected initialization request to be sent through transport")
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
