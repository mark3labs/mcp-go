package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// mockProtocolTransport implements transport.Interface for testing protocol negotiation
type mockProtocolTransport struct {
	responses           map[string]string
	notificationHandler func(mcp.JSONRPCNotification)
	started             bool
	closed              bool
}

func (m *mockProtocolTransport) Start(ctx context.Context) error {
	m.started = true
	return nil
}

func (m *mockProtocolTransport) SendRequest(ctx context.Context, request transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	responseStr, ok := m.responses[request.Method]
	if !ok {
		return nil, fmt.Errorf("no mock response for method %s", request.Method)
	}

	return &transport.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result:  json.RawMessage(responseStr),
	}, nil
}

func (m *mockProtocolTransport) SendNotification(ctx context.Context, notification mcp.JSONRPCNotification) error {
	return nil
}

func (m *mockProtocolTransport) SetNotificationHandler(handler func(notification mcp.JSONRPCNotification)) {
	m.notificationHandler = handler
}

func (m *mockProtocolTransport) Close() error {
	m.closed = true
	return nil
}

func (m *mockProtocolTransport) GetSessionId() string {
	return "mock-session"
}

func TestProtocolVersionNegotiation(t *testing.T) {
	tests := []struct {
		name          string
		serverVersion string
		expectError   bool
		errorContains string
	}{
		{
			name:          "supported latest version",
			serverVersion: mcp.LATEST_PROTOCOL_VERSION,
			expectError:   false,
		},
		{
			name:          "supported older version 2025-03-26",
			serverVersion: "2025-03-26",
			expectError:   false,
		},
		{
			name:          "supported older version 2024-11-05",
			serverVersion: "2024-11-05",
			expectError:   false,
		},
		{
			name:          "unsupported version",
			serverVersion: "2023-01-01",
			expectError:   true,
			errorContains: "unsupported protocol version",
		},
		{
			name:          "unsupported future version",
			serverVersion: "2030-01-01",
			expectError:   true,
			errorContains: "unsupported protocol version",
		},
		{
			name:          "empty protocol version",
			serverVersion: "",
			expectError:   true,
			errorContains: "unsupported protocol version",
		},
		{
			name:          "malformed protocol version - invalid format",
			serverVersion: "not-a-date",
			expectError:   true,
			errorContains: "unsupported protocol version",
		},
		{
			name:          "malformed protocol version - partial date",
			serverVersion: "2025-06",
			expectError:   true,
			errorContains: "unsupported protocol version",
		},
		{
			name:          "malformed protocol version - just numbers",
			serverVersion: "20250618",
			expectError:   true,
			errorContains: "unsupported protocol version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock transport that returns specific version
			mockTransport := &mockProtocolTransport{
				responses: map[string]string{
					"initialize": fmt.Sprintf(`{
						"protocolVersion": "%s",
						"capabilities": {},
						"serverInfo": {"name": "test", "version": "1.0"}
					}`, tt.serverVersion),
				},
			}

			client := NewClient(mockTransport)

			_, err := client.Initialize(context.Background(), mcp.InitializeRequest{
				Params: mcp.InitializeParams{
					ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
					ClientInfo:      mcp.Implementation{Name: "test-client", Version: "1.0"},
					Capabilities:    mcp.ClientCapabilities{},
				},
			})

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error containing %q, got %q", tt.errorContains, err.Error())
				}
				// Verify it's the correct error type
				if !mcp.IsUnsupportedProtocolVersion(err) {
					t.Errorf("expected UnsupportedProtocolVersionError, got %T", err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				// Verify the protocol version was stored
				if client.protocolVersion != tt.serverVersion {
					t.Errorf("expected protocol version %q, got %q", tt.serverVersion, client.protocolVersion)
				}
			}
		})
	}
}

// mockHTTPTransport implements both transport.Interface and transport.HTTPConnection
type mockHTTPTransport struct {
	mockProtocolTransport
	protocolVersion string
}

func (m *mockHTTPTransport) SetProtocolVersion(version string) {
	m.protocolVersion = version
}

func TestProtocolVersionHeaderSetting(t *testing.T) {
	// Create mock HTTP transport
	mockTransport := &mockHTTPTransport{
		mockProtocolTransport: mockProtocolTransport{
			responses: map[string]string{
				"initialize": fmt.Sprintf(`{
					"protocolVersion": "%s",
					"capabilities": {},
					"serverInfo": {"name": "test", "version": "1.0"}
				}`, mcp.LATEST_PROTOCOL_VERSION),
			},
		},
	}

	client := NewClient(mockTransport)

	_, err := client.Initialize(context.Background(), mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcp.Implementation{Name: "test-client", Version: "1.0"},
			Capabilities:    mcp.ClientCapabilities{},
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify SetProtocolVersion was called on HTTP transport
	if mockTransport.protocolVersion != mcp.LATEST_PROTOCOL_VERSION {
		t.Errorf("expected SetProtocolVersion to be called with %q, got %q",
			mcp.LATEST_PROTOCOL_VERSION, mockTransport.protocolVersion)
	}
}

func TestUnsupportedProtocolVersionError_Is(t *testing.T) {
	// Test that errors.Is works correctly with UnsupportedProtocolVersionError
	err1 := mcp.UnsupportedProtocolVersionError{Version: "2023-01-01"}
	err2 := mcp.UnsupportedProtocolVersionError{Version: "2024-01-01"}

	// Test Is method
	if !err1.Is(err2) {
		t.Error("expected UnsupportedProtocolVersionError.Is to return true for same error type")
	}

	// Test with different error type
	otherErr := fmt.Errorf("some other error")
	if err1.Is(otherErr) {
		t.Error("expected UnsupportedProtocolVersionError.Is to return false for different error type")
	}

	// Test IsUnsupportedProtocolVersion helper
	if !mcp.IsUnsupportedProtocolVersion(err1) {
		t.Error("expected IsUnsupportedProtocolVersion to return true")
	}
	if mcp.IsUnsupportedProtocolVersion(otherErr) {
		t.Error("expected IsUnsupportedProtocolVersion to return false for different error type")
	}
}
