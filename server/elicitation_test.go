package server

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBasicSession implements ClientSession for testing (without elicitation support)
type mockBasicSession struct {
	sessionID string
}

func (m *mockBasicSession) SessionID() string {
	return m.sessionID
}

func (m *mockBasicSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return make(chan mcp.JSONRPCNotification, 1)
}

func (m *mockBasicSession) Initialize() {}

func (m *mockBasicSession) Initialized() bool {
	return true
}

// mockElicitationSession implements SessionWithElicitation for testing
type mockElicitationSession struct {
	sessionID string
	result    *mcp.ElicitationResult
	err       error
}

func (m *mockElicitationSession) SessionID() string {
	return m.sessionID
}

func (m *mockElicitationSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return make(chan mcp.JSONRPCNotification, 1)
}

func (m *mockElicitationSession) Initialize() {}

func (m *mockElicitationSession) Initialized() bool {
	return true
}

func (m *mockElicitationSession) RequestElicitation(ctx context.Context, request mcp.ElicitationRequest) (*mcp.ElicitationResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func TestMCPServer_RequestElicitation_NoSession(t *testing.T) {
	server := NewMCPServer("test", "1.0.0")
	server.capabilities.elicitation = mcp.ToBoolPtr(true)

	request := mcp.ElicitationRequest{
		Params: mcp.ElicitationParams{
			Message: "Need some information",
			RequestedSchema: map[string]interface{}{
				"type": "object",
			},
		},
	}

	_, err := server.RequestElicitation(context.Background(), request)

	if err == nil {
		t.Error("expected error when no session available")
	}

	expectedError := "no active session"
	if err.Error() != expectedError {
		t.Errorf("expected error %q, got %q", expectedError, err.Error())
	}
}

func TestMCPServer_RequestElicitation_SessionDoesNotSupportElicitation(t *testing.T) {
	server := NewMCPServer("test", "1.0.0", WithElicitation())

	// Use a regular session that doesn't implement SessionWithElicitation
	mockSession := &mockBasicSession{sessionID: "test-session"}

	ctx := context.Background()
	ctx = server.WithContext(ctx, mockSession)

	request := mcp.ElicitationRequest{
		Params: mcp.ElicitationParams{
			Message: "Need some information",
			RequestedSchema: map[string]interface{}{
				"type": "object",
			},
		},
	}

	_, err := server.RequestElicitation(ctx, request)

	if err == nil {
		t.Error("expected error when session doesn't support elicitation")
	}

	expectedError := "session does not support elicitation"
	if err.Error() != expectedError {
		t.Errorf("expected error %q, got %q", expectedError, err.Error())
	}
}

func TestMCPServer_RequestElicitation_Success(t *testing.T) {
	server := NewMCPServer("test", "1.0.0", WithElicitation())

	// Create a mock elicitation session
	mockSession := &mockElicitationSession{
		sessionID: "test-session",
		result: &mcp.ElicitationResult{
			Response: mcp.ElicitationResponse{
				Type: mcp.ElicitationResponseTypeAccept,
				Value: map[string]interface{}{
					"projectName": "my-project",
					"framework":   "react",
				},
			},
		},
	}

	// Create context with session
	ctx := context.Background()
	ctx = server.WithContext(ctx, mockSession)

	request := mcp.ElicitationRequest{
		Params: mcp.ElicitationParams{
			Message: "Please provide project details",
			RequestedSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"projectName": map[string]interface{}{"type": "string"},
					"framework":   map[string]interface{}{"type": "string"},
				},
			},
		},
	}

	result, err := server.RequestElicitation(ctx, request)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Error("expected result, got nil")
		return
	}

	if result.Response.Type != mcp.ElicitationResponseTypeAccept {
		t.Errorf("expected response type %q, got %q", mcp.ElicitationResponseTypeAccept, result.Response.Type)
	}

	value, ok := result.Response.Value.(map[string]interface{})
	if !ok {
		t.Error("expected value to be a map")
		return
	}

	if value["projectName"] != "my-project" {
		t.Errorf("expected projectName %q, got %q", "my-project", value["projectName"])
	}
}

func TestRequestElicitation(t *testing.T) {
	tests := []struct {
		name          string
		session       ClientSession
		request       mcp.ElicitationRequest
		expectedError string
		expectedType  mcp.ElicitationResponseType
	}{
		{
			name: "successful elicitation with accept",
			session: &mockElicitationSession{
				sessionID: "test-1",
				result: &mcp.ElicitationResult{
					Response: mcp.ElicitationResponse{
						Type: mcp.ElicitationResponseTypeAccept,
						Value: map[string]interface{}{
							"name":      "test-project",
							"framework": "react",
						},
					},
				},
			},
			request: mcp.ElicitationRequest{
				Params: mcp.ElicitationParams{
					Message: "Please provide project details",
					RequestedSchema: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"name":      map[string]interface{}{"type": "string"},
							"framework": map[string]interface{}{"type": "string"},
						},
					},
				},
			},
			expectedType: mcp.ElicitationResponseTypeAccept,
		},
		{
			name: "elicitation declined by user",
			session: &mockElicitationSession{
				sessionID: "test-2",
				result: &mcp.ElicitationResult{
					Response: mcp.ElicitationResponse{
						Type: mcp.ElicitationResponseTypeDecline,
					},
				},
			},
			request: mcp.ElicitationRequest{
				Params: mcp.ElicitationParams{
					Message:         "Need some info",
					RequestedSchema: map[string]interface{}{"type": "object"},
				},
			},
			expectedType: mcp.ElicitationResponseTypeDecline,
		},
		{
			name:    "session does not support elicitation",
			session: &fakeSession{sessionID: "test-3"},
			request: mcp.ElicitationRequest{
				Params: mcp.ElicitationParams{
					Message:         "Need info",
					RequestedSchema: map[string]interface{}{"type": "object"},
				},
			},
			expectedError: "session does not support elicitation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewMCPServer("test", "1.0", WithElicitation())
			ctx := server.WithContext(context.Background(), tt.session)

			result, err := server.RequestElicitation(ctx, tt.request)

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tt.expectedType, result.Response.Type)

			if tt.expectedType == mcp.ElicitationResponseTypeAccept {
				assert.NotNil(t, result.Response.Value)
			}
		})
	}
}
