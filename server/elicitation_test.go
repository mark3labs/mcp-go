package server

import (
	"context"
	"errors"
	"testing"
	"time"

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
	sessionID   string
	result      *mcp.ElicitationResult
	err         error
	lastRequest mcp.ElicitationRequest
	notifyChan  chan mcp.JSONRPCNotification
}

func (m *mockElicitationSession) SessionID() string {
	return m.sessionID
}

func (m *mockElicitationSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	if m.notifyChan == nil {
		// Buffer of 100 to avoid blocking during tests with multiple notifications
		m.notifyChan = make(chan mcp.JSONRPCNotification, 100)
	}
	return m.notifyChan
}

func (m *mockElicitationSession) Initialize() {}

func (m *mockElicitationSession) Initialized() bool {
	return true
}

func (m *mockElicitationSession) RequestElicitation(ctx context.Context, request mcp.ElicitationRequest) (*mcp.ElicitationResult, error) {
	m.lastRequest = request
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
			RequestedSchema: map[string]any{
				"type": "object",
			},
		},
	}

	_, err := server.RequestElicitation(context.Background(), request)

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoActiveSession), "expected ErrNoActiveSession, got %v", err)
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
			RequestedSchema: map[string]any{
				"type": "object",
			},
		},
	}

	_, err := server.RequestElicitation(ctx, request)

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrElicitationNotSupported), "expected ErrElicitationNotSupported, got %v", err)
}

func TestMCPServer_RequestElicitation_Success(t *testing.T) {
	server := NewMCPServer("test", "1.0.0", WithElicitation())

	// Create a mock elicitation session
	mockSession := &mockElicitationSession{
		sessionID: "test-session",
		result: &mcp.ElicitationResult{
			ElicitationResponse: mcp.ElicitationResponse{
				Action: mcp.ElicitationResponseActionAccept,
				Content: map[string]any{
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
			RequestedSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"projectName": map[string]any{"type": "string"},
					"framework":   map[string]any{"type": "string"},
				},
			},
		},
	}

	result, err := server.RequestElicitation(ctx, request)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mcp.ElicitationResponseActionAccept, result.Action)

	value, ok := result.Content.(map[string]any)
	require.True(t, ok, "expected value to be a map")
	assert.Equal(t, "my-project", value["projectName"])
}

func TestRequestElicitation(t *testing.T) {
	tests := []struct {
		name          string
		session       ClientSession
		request       mcp.ElicitationRequest
		expectedError error
		expectedType  mcp.ElicitationResponseAction
	}{
		{
			name: "successful elicitation with accept",
			session: &mockElicitationSession{
				sessionID: "test-1",
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
			request: mcp.ElicitationRequest{
				Params: mcp.ElicitationParams{
					Message: "Please provide project details",
					RequestedSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name":      map[string]any{"type": "string"},
							"framework": map[string]any{"type": "string"},
						},
					},
				},
			},
			expectedType: mcp.ElicitationResponseActionAccept,
		},
		{
			name: "elicitation declined by user",
			session: &mockElicitationSession{
				sessionID: "test-2",
				result: &mcp.ElicitationResult{
					ElicitationResponse: mcp.ElicitationResponse{
						Action: mcp.ElicitationResponseActionDecline,
					},
				},
			},
			request: mcp.ElicitationRequest{
				Params: mcp.ElicitationParams{
					Message:         "Need some info",
					RequestedSchema: map[string]any{"type": "object"},
				},
			},
			expectedType: mcp.ElicitationResponseActionDecline,
		},
		{
			name:    "session does not support elicitation",
			session: &mockBasicSession{sessionID: "test-3"},
			request: mcp.ElicitationRequest{
				Params: mcp.ElicitationParams{
					Message:         "Need info",
					RequestedSchema: map[string]any{"type": "object"},
				},
			},
			expectedError: ErrElicitationNotSupported,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewMCPServer("test", "1.0", WithElicitation())
			ctx := server.WithContext(context.Background(), tt.session)

			result, err := server.RequestElicitation(ctx, tt.request)

			if tt.expectedError != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tt.expectedError), "expected %v, got %v", tt.expectedError, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tt.expectedType, result.Action)

			if tt.expectedType == mcp.ElicitationResponseActionAccept {
				assert.NotNil(t, result.Content)
			}
		})
	}
}

func TestRequestURLElicitation(t *testing.T) {
	s := NewMCPServer("test", "1.0", WithElicitation())

	mockSession := &mockElicitationSession{
		sessionID: "test-url-1",
		result: &mcp.ElicitationResult{
			ElicitationResponse: mcp.ElicitationResponse{
				Action: mcp.ElicitationResponseActionAccept,
			},
		},
	}

	ctx := context.Background()
	_, err := s.RequestURLElicitation(ctx, mockSession, "id-123", "https://example.com/auth", "Please auth")
	require.NoError(t, err)

	assert.Equal(t, mcp.ElicitationModeURL, mockSession.lastRequest.Params.Mode)
	assert.Equal(t, "id-123", mockSession.lastRequest.Params.ElicitationID)
	assert.Equal(t, "https://example.com/auth", mockSession.lastRequest.Params.URL)

	notifyChan := make(chan mcp.JSONRPCNotification, 1)
	mockSessionWithChan := &mockElicitationSession{
		sessionID:  "test-url-2",
		notifyChan: notifyChan,
	}

	err = s.SendElicitationComplete(ctx, mockSessionWithChan, "id-123")
	require.NoError(t, err)

	select {
	case notif := <-notifyChan:
		assert.Equal(t, "notifications/elicitation/complete", notif.Method)
		// Validate elicitationId is included in params
		elicitationID, ok := notif.Params.AdditionalFields["elicitationId"]
		assert.True(t, ok, "expected elicitationId in notification params")
		assert.Equal(t, "id-123", elicitationID)
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected notification not received")
	}
}

func TestSendElicitationComplete_NoPriorRequest(t *testing.T) {
	s := NewMCPServer("test", "1.0", WithElicitation())

	notifyChan := make(chan mcp.JSONRPCNotification, 1)
	mockSession := &mockElicitationSession{
		sessionID:  "test-session-complete",
		notifyChan: notifyChan,
	}

	// Call SendElicitationComplete directly without any prior request state
	// This verifies the server can send completion notifications independently
	err := s.SendElicitationComplete(context.Background(), mockSession, "independent-id-999")
	require.NoError(t, err)

	select {
	case notif := <-notifyChan:
		assert.Equal(t, "notifications/elicitation/complete", notif.Method)
		elicitationID, ok := notif.Params.AdditionalFields["elicitationId"]
		assert.True(t, ok, "expected elicitationId in notification params")
		assert.Equal(t, "independent-id-999", elicitationID)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected notification was not received")
	}
}
