package server

import (
	"context"
	"errors"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBasicRootsSession implements ClientSession for testing (without roots support)
type mockBasicRootsSession struct {
	sessionID string
}

func (m *mockBasicRootsSession) SessionID() string {
	return m.sessionID
}

func (m *mockBasicRootsSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return make(chan mcp.JSONRPCNotification, 1)
}

func (m *mockBasicRootsSession) Initialize() {}

func (m *mockBasicRootsSession) Initialized() bool {
	return true
}

// mockRootsSession implements SessionWithRoots for testing
type mockRootsSession struct {
	sessionID string
	result    *mcp.ListRootsResult
	err       error
}

func (m *mockRootsSession) SessionID() string {
	return m.sessionID
}

func (m *mockRootsSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return make(chan mcp.JSONRPCNotification, 1)
}

func (m *mockRootsSession) Initialize() {}

func (m *mockRootsSession) Initialized() bool {
	return true
}

func (m *mockRootsSession) ListRoots(ctx context.Context, request mcp.ListRootsRequest) (*mcp.ListRootsResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func TestMCPServer_RequestRoots_NoSession(t *testing.T) {
	server := NewMCPServer("test", "1.0.0")
	server.capabilities.roots = mcp.ToBoolPtr(true)

	request := mcp.ListRootsRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodListRoots),
		},
	}

	_, err := server.RequestRoots(context.Background(), request)

	if err == nil {
		t.Error("expected error when no session available")
	}

	if !errors.Is(err, ErrNoClientSession) {
		t.Errorf("expected ErrNoClientSession, got %v", err)
	}
}

func TestMCPServer_RequestRoots_SessionDoesNotSupportRoots(t *testing.T) {
	server := NewMCPServer("test", "1.0.0", WithRoots())

	// Use a regular session that doesn't implement SessionWithRoots
	mockSession := &mockBasicRootsSession{sessionID: "test-session"}

	ctx := context.Background()
	ctx = server.WithContext(ctx, mockSession)

	request := mcp.ListRootsRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodListRoots),
		},
	}

	_, err := server.RequestRoots(ctx, request)

	if err == nil {
		t.Error("expected error when session doesn't support roots")
	}

	if !errors.Is(err, ErrRootsNotSupported) {
		t.Errorf("expected ErrRootsNotSupported, got %v", err)
	}
}

func TestMCPServer_RequestRoots_Success(t *testing.T) {
	opts := []ServerOption{
		WithRoots(),
	}
	server := NewMCPServer("test", "1.0.0", opts...)

	// Create a mock roots session
	mockSession := &mockRootsSession{
		sessionID: "test-session",
		result: &mcp.ListRootsResult{
			Roots: []mcp.Root{
				{
					Name: ".kube",
					URI:  "file:///User/haxxx/.kube",
				},
				{
					Name: "project",
					URI:  "file:///User/haxxx/projects/snative",
				},
			},
		},
	}

	// Create context with session
	ctx := context.Background()
	ctx = server.WithContext(ctx, mockSession)

	request := mcp.ListRootsRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodListRoots),
		},
	}

	result, err := server.RequestRoots(ctx, request)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Error("expected result, got nil")
		return
	}

	if len(result.Roots) == 0 {
		t.Error("roots result is empty")
		return
	}

	for _, value := range result.Roots {
		if value.Name != "project" && value.Name != ".kube" {
			t.Errorf("expected root name %q, %q, got %q", "project", ".kube", value.Name)
		}
		if value.URI != "file:///User/haxxx/.kube" && value.URI != "file:///User/haxxx/projects/snative" {
			t.Errorf("expected root URI %q, %q, got %q", "file:///User/haxxx/.kube", "file:///User/haxxx/projects/snative", value.URI)
		}
	}
}

func TestRequestRoots(t *testing.T) {
	tests := []struct {
		name          string
		session       ClientSession
		request       mcp.ListRootsRequest
		expectedError error
	}{
		{
			name: "successful roots with name and uri",
			session: &mockRootsSession{
				sessionID: "test-1",
				result: &mcp.ListRootsResult{
					Roots: []mcp.Root{
						{
							Name: ".kube",
							URI:  "file:///User/haxxx/.kube",
						},
						{
							Name: "project",
							URI:  "file:///User/haxxx/projects/snative",
						},
					},
				},
			},
			request: mcp.ListRootsRequest{
				Request: mcp.Request{
					Method: string(mcp.MethodListRoots),
				},
			},
		},
		{
			name: "successful roots with empty list",
			session: &mockRootsSession{
				sessionID: "test-2",
				result: &mcp.ListRootsResult{
					Roots: []mcp.Root{},
				},
			},
			request: mcp.ListRootsRequest{
				Request: mcp.Request{
					Method: string(mcp.MethodListRoots),
				},
			},
		},
		{
			name:    "session does not support roots",
			session: &fakeSession{sessionID: "test-3"},
			request: mcp.ListRootsRequest{
				Request: mcp.Request{
					Method: string(mcp.MethodListRoots),
				},
			},
			expectedError: ErrRootsNotSupported,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewMCPServer("test", "1.0", WithRoots())
			ctx := server.WithContext(context.Background(), tt.session)

			result, err := server.RequestRoots(ctx, tt.request)

			if tt.expectedError != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tt.expectedError), "expected %v, got %v", tt.expectedError, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)

		})
	}
}
