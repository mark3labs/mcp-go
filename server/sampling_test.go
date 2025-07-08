package server

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestMCPServer_RequestSampling_NoSession(t *testing.T) {
	server := NewMCPServer("test", "1.0.0")
	server.EnableSampling()

	request := mcp.CreateMessageRequest{
		CreateMessageParams: mcp.CreateMessageParams{
			Messages: []mcp.SamplingMessage{
				{Role: mcp.RoleUser, Content: mcp.TextContent{Type: "text", Text: "Test"}},
			},
			MaxTokens: 100,
		},
	}

	_, err := server.RequestSampling(context.Background(), request)

	if err == nil {
		t.Error("expected error when no session available")
	}

	expectedError := "no active session"
	if err.Error() != expectedError {
		t.Errorf("expected error %q, got %q", expectedError, err.Error())
	}
}

// mockSession implements ClientSession for testing
type mockSession struct {
	sessionID string
}

func (m *mockSession) SessionID() string {
	return m.sessionID
}

func (m *mockSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return make(chan mcp.JSONRPCNotification, 1)
}

func (m *mockSession) Initialize() {}

func (m *mockSession) Initialized() bool {
	return true
}

// mockSamplingSession implements SessionWithSampling for testing
type mockSamplingSession struct {
	mockSession
	result *mcp.CreateMessageResult
	err    error
}

func (m *mockSamplingSession) RequestSampling(ctx context.Context, request mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func TestMCPServer_RequestSampling_Success(t *testing.T) {
	server := NewMCPServer("test", "1.0.0")
	server.EnableSampling()

	// Create a mock sampling session
	mockSession := &mockSamplingSession{
		mockSession: mockSession{sessionID: "test-session"},
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

	// Create context with session
	ctx := context.Background()
	ctx = server.WithContext(ctx, mockSession)

	request := mcp.CreateMessageRequest{
		CreateMessageParams: mcp.CreateMessageParams{
			Messages: []mcp.SamplingMessage{
				{Role: mcp.RoleUser, Content: mcp.TextContent{Type: "text", Text: "Test"}},
			},
			MaxTokens: 100,
		},
	}

	result, err := server.RequestSampling(ctx, request)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Error("expected result, got nil")
		return
	}

	if result.Model != "test-model" {
		t.Errorf("expected model %q, got %q", "test-model", result.Model)
	}
}
