package server

import (
	"context"
	"encoding/json"
	"maps"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

type sessionTestClientWithResourceTemplates struct {
	sessionID                string
	notificationChannel      chan mcp.JSONRPCNotification
	initialized              atomic.Bool
	sessionResourceTemplates map[string]ServerResourceTemplate
	mu                       sync.RWMutex
}

func (f *sessionTestClientWithResourceTemplates) SessionID() string {
	return f.sessionID
}

func (f *sessionTestClientWithResourceTemplates) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return f.notificationChannel
}

func (f *sessionTestClientWithResourceTemplates) Initialize() {
	f.initialized.Store(true)
}

func (f *sessionTestClientWithResourceTemplates) Initialized() bool {
	return f.initialized.Load()
}

func (f *sessionTestClientWithResourceTemplates) GetSessionResourceTemplates() map[string]ServerResourceTemplate {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return maps.Clone(f.sessionResourceTemplates)
}

func (f *sessionTestClientWithResourceTemplates) SetSessionResourceTemplates(templates map[string]ServerResourceTemplate) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessionResourceTemplates = maps.Clone(templates)
}

var _ SessionWithResourceTemplates = (*sessionTestClientWithResourceTemplates)(nil)

func TestSessionWithResourceTemplates_Integration(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0")

	sessionTemplate := ServerResourceTemplate{
		Template: mcp.NewResourceTemplate("test://session/{id}", "session-template"),
		Handler: func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{mcp.TextResourceContents{
				URI:  request.Params.URI,
				Text: "session-template result",
			}}, nil
		},
	}

	session := &sessionTestClientWithResourceTemplates{
		sessionID:           "session-1",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
		sessionResourceTemplates: map[string]ServerResourceTemplate{
			"test://session/{id}": sessionTemplate,
		},
	}
	session.initialized.Store(true)

	err := server.RegisterSession(context.Background(), session)
	require.NoError(t, err)

	testReq := mcp.ReadResourceRequest{}
	testReq.Params.URI = "test://session/123"

	sessionCtx := server.WithContext(context.Background(), session)

	s := ClientSessionFromContext(sessionCtx)
	require.NotNil(t, s, "Session should be available from context")
	assert.Equal(t, session.SessionID(), s.SessionID(), "Session ID should match")

	swrt, ok := s.(SessionWithResourceTemplates)
	require.True(t, ok, "Session should implement SessionWithResourceTemplates")

	templates := swrt.GetSessionResourceTemplates()
	require.NotNil(t, templates, "Session resource templates should be available")
	require.Contains(t, templates, "test://session/{id}", "Session should have test://session/{id}")

	t.Run("test session resource template access", func(t *testing.T) {
		template, exists := templates["test://session/{id}"]
		require.True(t, exists, "Session resource template should exist in the map")
		require.NotNil(t, template, "Session resource template should not be nil")

		result, err := template.Handler(sessionCtx, testReq)
		require.NoError(t, err, "No error calling session resource template handler directly")
		require.NotNil(t, result, "Result should not be nil")
		require.Len(t, result, 1, "Result should have one content item")

		textContent, ok := result[0].(mcp.TextResourceContents)
		require.True(t, ok, "Content should be TextResourceContents")
		assert.Equal(t, "session-template result", textContent.Text, "Result text should match")
	})
}

func TestMCPServer_ResourceTemplatesWithSessionResourceTemplates(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithResourceCapabilities(false, true))

	server.AddResourceTemplates(
		ServerResourceTemplate{
			Template: mcp.NewResourceTemplate("test://global/{id}", "global-template-1"),
			Handler: func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
				return []mcp.ResourceContents{mcp.TextResourceContents{
					URI:  request.Params.URI,
					Text: "global-template-1 result",
				}}, nil
			},
		},
		ServerResourceTemplate{
			Template: mcp.NewResourceTemplate("test://another/{id}", "global-template-2"),
			Handler: func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
				return []mcp.ResourceContents{mcp.TextResourceContents{
					URI:  request.Params.URI,
					Text: "global-template-2 result",
				}}, nil
			},
		},
	)

	session := &sessionTestClientWithResourceTemplates{
		sessionID:           "session-1",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
		sessionResourceTemplates: map[string]ServerResourceTemplate{
			"test://global/{id}": {
				Template: mcp.NewResourceTemplate("test://global/{id}", "global-template-1-overridden"),
				Handler: func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
					return []mcp.ResourceContents{mcp.TextResourceContents{
						URI:  request.Params.URI,
						Text: "session-overridden result",
					}}, nil
				},
			},
		},
	}
	session.initialized.Store(true)

	err := server.RegisterSession(context.Background(), session)
	require.NoError(t, err)

	sessionCtx := server.WithContext(context.Background(), session)
	resp := server.HandleMessage(sessionCtx, []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "resources/templates/list"
	}`))

	jsonResp, ok := resp.(mcp.JSONRPCResponse)
	require.True(t, ok, "Response should be a JSONRPCResponse")

	result, ok := jsonResp.Result.(mcp.ListResourceTemplatesResult)
	require.True(t, ok, "Result should be a ListResourceTemplatesResult")

	assert.Len(t, result.ResourceTemplates, 2, "Should have 2 resource templates")

	templateMap := make(map[string]mcp.ResourceTemplate)
	for _, template := range result.ResourceTemplates {
		templateMap[template.URITemplate.Raw()] = template
	}

	require.Contains(t, templateMap, "test://another/{id}", "Should have non-overridden global template")
	assert.Equal(t, "global-template-2", templateMap["test://another/{id}"].Name, "Global template name should match")

	require.Contains(t, templateMap, "test://global/{id}", "Should have overridden global template")
	assert.Equal(t, "global-template-1-overridden", templateMap["test://global/{id}"].Name, "Overridden template name should match session version")

	t.Run("read overridden resource via HandleMessage", func(t *testing.T) {
		readResp := server.HandleMessage(sessionCtx, []byte(`{
			"jsonrpc": "2.0",
			"id": 2,
			"method": "resources/read",
			"params": {
				"uri": "test://global/123"
			}
		}`))

		readJSONResp, ok := readResp.(mcp.JSONRPCResponse)
		require.True(t, ok, "Read response should be a JSONRPCResponse")

		readResult, ok := readJSONResp.Result.(mcp.ReadResourceResult)
		require.True(t, ok, "Result should be a ReadResourceResult")

		require.Len(t, readResult.Contents, 1, "Should have one content item")
		textContent, ok := readResult.Contents[0].(mcp.TextResourceContents)
		require.True(t, ok, "Content should be TextResourceContents")
		assert.Equal(t, "session-overridden result", textContent.Text, "Should return session handler's content")
	})
}

func TestMCPServer_AddSessionResourceTemplates(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithResourceCapabilities(false, true))
	ctx := context.Background()

	sessionChan := make(chan mcp.JSONRPCNotification, 10)
	session := &sessionTestClientWithResourceTemplates{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
	}
	session.initialized.Store(true)

	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	err = server.AddSessionResourceTemplates(session.SessionID(),
		ServerResourceTemplate{Template: mcp.NewResourceTemplate("test://session/{id}", "session-template")},
	)
	require.NoError(t, err)

	select {
	case notification := <-sessionChan:
		assert.Equal(t, "notifications/resources/list_changed", notification.Method)
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected notification not received")
	}

	assert.Len(t, session.GetSessionResourceTemplates(), 1)
	assert.Contains(t, session.GetSessionResourceTemplates(), "test://session/{id}")
}

func TestMCPServer_AddSessionResourceTemplate(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithResourceCapabilities(false, true))
	ctx := context.Background()

	sessionChan := make(chan mcp.JSONRPCNotification, 10)
	session := &sessionTestClientWithResourceTemplates{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
	}
	session.initialized.Store(true)

	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	err = server.AddSessionResourceTemplate(
		session.SessionID(),
		mcp.NewResourceTemplate("test://helper/{id}", "helper-template"),
		func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{mcp.TextResourceContents{
				URI:  request.Params.URI,
				Text: "helper result",
			}}, nil
		},
	)
	require.NoError(t, err)

	select {
	case notification := <-sessionChan:
		assert.Equal(t, "notifications/resources/list_changed", notification.Method)
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected notification not received")
	}

	assert.Len(t, session.GetSessionResourceTemplates(), 1)
	assert.Contains(t, session.GetSessionResourceTemplates(), "test://helper/{id}")
}

func TestMCPServer_AddSessionResourceTemplatesUninitialized(t *testing.T) {
	errorChan := make(chan error)
	hooks := &Hooks{}
	hooks.AddOnError(
		func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
			errorChan <- err
		},
	)

	server := NewMCPServer("test-server", "1.0.0",
		WithResourceCapabilities(false, true),
		WithHooks(hooks),
	)
	ctx := context.Background()

	sessionChan := make(chan mcp.JSONRPCNotification, 1)
	session := &sessionTestClientWithResourceTemplates{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
	}

	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	err = server.AddSessionResourceTemplates(session.SessionID(),
		ServerResourceTemplate{Template: mcp.NewResourceTemplate("test://uninit/{id}", "uninitialized-template")},
	)
	require.NoError(t, err)

	select {
	case err := <-errorChan:
		t.Error("Expected no errors, but OnError called with: ", err)
	case <-time.After(25 * time.Millisecond):
	}

	select {
	case <-sessionChan:
		t.Error("Expected no notification to be sent for uninitialized session")
	default:
	}

	assert.Len(t, session.GetSessionResourceTemplates(), 1)
	assert.Contains(t, session.GetSessionResourceTemplates(), "test://uninit/{id}")

	session.Initialize()

	err = server.AddSessionResourceTemplates(session.SessionID(),
		ServerResourceTemplate{Template: mcp.NewResourceTemplate("test://initialized/{id}", "initialized-template")},
	)
	require.NoError(t, err)

	select {
	case err := <-errorChan:
		t.Error("Expected no errors, but OnError called with:", err)
	case <-time.After(200 * time.Millisecond):
	}

	select {
	case notification := <-sessionChan:
		assert.Equal(t, "notifications/resources/list_changed", notification.Method)
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for expected notifications/resources/list_changed notification")
	}

	assert.Len(t, session.GetSessionResourceTemplates(), 2)
	assert.Contains(t, session.GetSessionResourceTemplates(), "test://uninit/{id}")
	assert.Contains(t, session.GetSessionResourceTemplates(), "test://initialized/{id}")
}

func TestMCPServer_DeleteSessionResourceTemplatesUninitialized(t *testing.T) {
	errorChan := make(chan error)
	hooks := &Hooks{}
	hooks.AddOnError(
		func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
			errorChan <- err
		},
	)

	server := NewMCPServer("test-server", "1.0.0",
		WithResourceCapabilities(false, true),
		WithHooks(hooks),
	)
	ctx := context.Background()

	sessionChan := make(chan mcp.JSONRPCNotification, 1)
	session := &sessionTestClientWithResourceTemplates{
		sessionID:           "uninitialized-session",
		notificationChannel: sessionChan,
		sessionResourceTemplates: map[string]ServerResourceTemplate{
			"test://delete/{id}": {Template: mcp.NewResourceTemplate("test://delete/{id}", "template-to-delete")},
			"test://keep/{id}":   {Template: mcp.NewResourceTemplate("test://keep/{id}", "template-to-keep")},
		},
	}

	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	err = server.DeleteSessionResourceTemplates(session.SessionID(), "test://delete/{id}")
	require.NoError(t, err)

	select {
	case err := <-errorChan:
		t.Errorf("Expected error hooks not to be called, got error: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	select {
	case <-sessionChan:
		t.Error("Expected no notification to be sent for uninitialized session")
	default:
	}

	assert.Len(t, session.GetSessionResourceTemplates(), 1)
	assert.NotContains(t, session.GetSessionResourceTemplates(), "test://delete/{id}")
	assert.Contains(t, session.GetSessionResourceTemplates(), "test://keep/{id}")

	session.Initialize()

	err = server.DeleteSessionResourceTemplates(session.SessionID(), "test://keep/{id}")
	require.NoError(t, err)

	select {
	case err := <-errorChan:
		t.Errorf("Expected error hooks not to be called, got error: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	select {
	case notification := <-sessionChan:
		assert.Equal(t, "notifications/resources/list_changed", notification.Method)
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected notification not received for initialized session")
	}

	assert.Len(t, session.GetSessionResourceTemplates(), 0)
}

func TestMCPServer_CallSessionResourceTemplate(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithResourceCapabilities(false, true))

	server.AddResourceTemplate(
		mcp.NewResourceTemplate("test://resource/{id}", "test_template"),
		func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{mcp.TextResourceContents{
				URI:  request.Params.URI,
				Text: "global result",
			}}, nil
		},
	)

	sessionChan := make(chan mcp.JSONRPCNotification, 10)
	session := &sessionTestClientWithResourceTemplates{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
	}

	err := server.RegisterSession(context.Background(), session)
	require.NoError(t, err)

	err = server.AddSessionResourceTemplate(
		session.SessionID(),
		mcp.NewResourceTemplate("test://resource/{id}", "test_template"),
		func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{mcp.TextResourceContents{
				URI:  request.Params.URI,
				Text: "session result",
			}}, nil
		},
	)
	require.NoError(t, err)

	sessionCtx := server.WithContext(context.Background(), session)
	resourceRequest := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "resources/read",
		"params": map[string]any{
			"uri": "test://resource/123",
		},
	}
	requestBytes, err := json.Marshal(resourceRequest)
	if err != nil {
		t.Fatalf("Failed to marshal resource request: %v", err)
	}

	response := server.HandleMessage(sessionCtx, requestBytes)
	resp, ok := response.(mcp.JSONRPCResponse)
	assert.True(t, ok)

	readResourceResult, ok := resp.Result.(mcp.ReadResourceResult)
	assert.True(t, ok)

	if text := readResourceResult.Contents[0].(mcp.TextResourceContents).Text; text != "session result" {
		t.Errorf("Expected result 'session result', got %q", text)
	}
}

func TestMCPServer_DeleteSessionResourceTemplates(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithResourceCapabilities(false, true))
	ctx := context.Background()

	sessionChan := make(chan mcp.JSONRPCNotification, 10)
	session := &sessionTestClientWithResourceTemplates{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
		sessionResourceTemplates: map[string]ServerResourceTemplate{
			"test://template1/{id}": {
				Template: mcp.NewResourceTemplate("test://template1/{id}", "session-template-1"),
			},
			"test://template2/{id}": {
				Template: mcp.NewResourceTemplate("test://template2/{id}", "session-template-2"),
			},
		},
	}
	session.initialized.Store(true)

	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	err = server.DeleteSessionResourceTemplates(session.SessionID(), "test://template1/{id}")
	require.NoError(t, err)

	select {
	case notification := <-sessionChan:
		assert.Equal(t, "notifications/resources/list_changed", notification.Method)
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected notification not received")
	}

	assert.Len(t, session.GetSessionResourceTemplates(), 1)
	assert.NotContains(t, session.GetSessionResourceTemplates(), "test://template1/{id}")
	assert.Contains(t, session.GetSessionResourceTemplates(), "test://template2/{id}")
}

func TestMCPServer_SessionResourceTemplateError(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0")
	ctx := context.Background()

	session := &sessionTestClient{
		sessionID:           "session-1",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
	}

	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	err = server.AddSessionResourceTemplates(session.SessionID(),
		ServerResourceTemplate{Template: mcp.NewResourceTemplate("test://template/{id}", "test-template")},
	)
	require.Error(t, err)
	assert.Equal(t, ErrSessionDoesNotSupportResourceTemplates, err)
}

func TestMCPServer_ResourceTemplatesNotificationsDisabled(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithResourceCapabilities(false, false))
	ctx := context.Background()

	sessionChan := make(chan mcp.JSONRPCNotification, 1)
	session := &sessionTestClientWithResourceTemplates{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
	}

	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	err = server.AddSessionResourceTemplates(session.SessionID(),
		ServerResourceTemplate{Template: mcp.NewResourceTemplate("test://template/{id}", "test-template")},
	)
	require.NoError(t, err)

	select {
	case <-sessionChan:
		t.Error("Expected no notification to be sent when capabilities.resources.listChanged is false")
	default:
	}

	assert.Len(t, session.GetSessionResourceTemplates(), 1)
	assert.Contains(t, session.GetSessionResourceTemplates(), "test://template/{id}")

	err = server.DeleteSessionResourceTemplates(session.SessionID(), "test://template/{id}")
	require.NoError(t, err)

	select {
	case <-sessionChan:
		t.Error("Expected no notification to be sent when capabilities.resources.listChanged is false")
	default:
	}

	assert.Len(t, session.GetSessionResourceTemplates(), 0)
}
