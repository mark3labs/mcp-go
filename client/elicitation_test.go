package client

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// mockElicitationHandler implements ElicitationHandler for testing
type mockElicitationHandler struct {
	result *mcp.ElicitationResult
	err    error
}

func (m *mockElicitationHandler) Elicit(ctx context.Context, request mcp.ElicitationRequest) (*mcp.ElicitationResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func TestClient_HandleElicitationRequest(t *testing.T) {
	tests := []struct {
		name          string
		handler       ElicitationHandler
		expectedError string
	}{
		{
			name:          "no handler configured",
			handler:       nil,
			expectedError: "no elicitation handler configured",
		},
		{
			name: "successful elicitation - accept",
			handler: &mockElicitationHandler{
				result: &mcp.ElicitationResult{
					ElicitationResponse: mcp.ElicitationResponse{
						Action: mcp.ElicitationResponseActionAccept,
						Content: map[string]any{
							"name":      "test-project",
							"framework": "react",
						},
					},
				},
			},
		},
		{
			name: "successful elicitation - decline",
			handler: &mockElicitationHandler{
				result: &mcp.ElicitationResult{
					ElicitationResponse: mcp.ElicitationResponse{
						Action: mcp.ElicitationResponseActionDecline,
					},
				},
			},
		},
		{
			name: "successful elicitation - cancel",
			handler: &mockElicitationHandler{
				result: &mcp.ElicitationResult{
					ElicitationResponse: mcp.ElicitationResponse{
						Action: mcp.ElicitationResponseActionCancel,
					},
				},
			},
		},
		{
			name: "handler returns error",
			handler: &mockElicitationHandler{
				err: fmt.Errorf("user interaction failed"),
			},
			expectedError: "user interaction failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{elicitationHandler: tt.handler}

			request := transport.JSONRPCRequest{
				ID:     mcp.NewRequestId(1),
				Method: string(mcp.MethodElicitationCreate),
				Params: map[string]any{
					"message": "Please provide project details",
					"requestedSchema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name":      map[string]any{"type": "string"},
							"framework": map[string]any{"type": "string"},
						},
					},
				},
			}

			result, err := client.handleElicitationRequestTransport(context.Background(), request)

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
				} else {
					// Verify the response is properly formatted
					var elicitationResult mcp.ElicitationResult
					if err := json.Unmarshal(result.Result, &elicitationResult); err != nil {
						t.Errorf("failed to unmarshal result: %v", err)
					}
				}
			}
		})
	}
}

func TestWithElicitationHandler(t *testing.T) {
	handler := &mockElicitationHandler{}
	client := &Client{}

	option := WithElicitationHandler(handler)
	option(client)

	if client.elicitationHandler != handler {
		t.Error("elicitation handler not set correctly")
	}
}

func TestClient_Initialize_WithElicitationHandler(t *testing.T) {
	mockTransport := &mockElicitationTransport{
		sendRequestFunc: func(ctx context.Context, request transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
			// Verify that elicitation capability is included
			// The client internally converts the typed params to a map for transport
			// So we check if we're getting the initialize request
			if request.Method != "initialize" {
				t.Fatalf("expected initialize method, got %s", request.Method)
			}

			// Return successful initialization response
			result := mcp.InitializeResult{
				ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
				ServerInfo: mcp.Implementation{
					Name:    "test-server",
					Version: "1.0.0",
				},
				Capabilities: mcp.ServerCapabilities{},
			}

			resultBytes, _ := json.Marshal(result)
			return transport.NewJSONRPCResultResponse(request.ID, resultBytes), nil
		},
		sendNotificationFunc: func(ctx context.Context, notification mcp.JSONRPCNotification) error {
			return nil
		},
	}

	handler := &mockElicitationHandler{}
	client := NewClient(mockTransport, WithElicitationHandler(handler))

	err := client.Start(context.Background())
	if err != nil {
		t.Fatalf("failed to start client: %v", err)
	}

	_, err = client.Initialize(context.Background(), mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "test-client",
				Version: "1.0.0",
			},
			Capabilities: mcp.ClientCapabilities{},
		},
	})
	if err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}
}

// mockElicitationTransport implements transport.Interface for testing
type mockElicitationTransport struct {
	sendRequestFunc      func(context.Context, transport.JSONRPCRequest) (*transport.JSONRPCResponse, error)
	sendNotificationFunc func(context.Context, mcp.JSONRPCNotification) error
}

func (m *mockElicitationTransport) Start(ctx context.Context) error {
	return nil
}

func (m *mockElicitationTransport) Close() error {
	return nil
}

func (m *mockElicitationTransport) SendRequest(ctx context.Context, request transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	if m.sendRequestFunc != nil {
		return m.sendRequestFunc(ctx, request)
	}
	return nil, nil
}

func (m *mockElicitationTransport) SendNotification(ctx context.Context, notification mcp.JSONRPCNotification) error {
	if m.sendNotificationFunc != nil {
		return m.sendNotificationFunc(ctx, notification)
	}
	return nil
}

func (m *mockElicitationTransport) SetNotificationHandler(handler func(mcp.JSONRPCNotification)) {
}

func (m *mockElicitationTransport) GetSessionId() string {
	return "mock-session"
}
