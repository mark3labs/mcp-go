package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAddSessionResource tests adding a single resource to a session
func TestAddSessionResource(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithResourceCapabilities(false, true))
	ctx := context.Background()

	// Create a session with resources support
	sessionChan := make(chan mcp.JSONRPCNotification, 10)
	session := &sessionTestClientWithResources{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
		initialized:         true,
		sessionResources:    make(map[string]ServerResource),
	}

	// Register the session
	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Add a single session resource with handler
	resource := mcp.NewResource("test://session-resource", "Session Resource")
	handler := func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      request.Params.URI,
				MIMEType: "text/plain",
				Text:     "session resource content",
			},
		}, nil
	}

	err = server.AddSessionResource(session.SessionID(), resource, handler)
	require.NoError(t, err)

	// Check that notification was sent
	select {
	case notification := <-sessionChan:
		assert.Equal(t, "notifications/resources/list_changed", notification.Method)
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected notification not received")
	}

	// Verify resource was added to session
	sessionResources := session.GetSessionResources()
	assert.Len(t, sessionResources, 1)
	assert.Contains(t, sessionResources, "test://session-resource")

	// Verify the handler works
	serverResource := sessionResources["test://session-resource"]
	contents, err := serverResource.Handler(ctx, mcp.ReadResourceRequest{Params: mcp.ReadResourceParams{URI: "test://session-resource"}})
	require.NoError(t, err)
	assert.Len(t, contents, 1)
	assert.Equal(t, "session resource content", contents[0].(mcp.TextResourceContents).Text)
}

// TestAddSessionResources tests adding multiple resources to a session
func TestAddSessionResources(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithResourceCapabilities(false, true))
	ctx := context.Background()

	// Create a session with resources support
	sessionChan := make(chan mcp.JSONRPCNotification, 10)
	session := &sessionTestClientWithResources{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
		initialized:         true,
		sessionResources:    make(map[string]ServerResource),
	}

	// Register the session
	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Add multiple session resources
	resources := []ServerResource{
		{
			Resource: mcp.NewResource("test://resource1", "Resource 1"),
			Handler: func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
				return []mcp.ResourceContents{
					mcp.TextResourceContents{URI: request.Params.URI, MIMEType: "text/plain", Text: "content 1"},
				}, nil
			},
		},
		{
			Resource: mcp.NewResource("test://resource2", "Resource 2"),
			Handler: func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
				return []mcp.ResourceContents{
					mcp.TextResourceContents{URI: request.Params.URI, MIMEType: "text/plain", Text: "content 2"},
				}, nil
			},
		},
	}

	err = server.AddSessionResources(session.SessionID(), resources...)
	require.NoError(t, err)

	// Check that only ONE notification was sent for batch addition
	select {
	case notification := <-sessionChan:
		assert.Equal(t, "notifications/resources/list_changed", notification.Method)
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected notification not received")
	}

	// Ensure no additional notifications
	select {
	case <-sessionChan:
		t.Error("Unexpected additional notification received")
	case <-time.After(50 * time.Millisecond):
		// Expected: no more notifications
	}

	// Verify all resources were added
	sessionResources := session.GetSessionResources()
	assert.Len(t, sessionResources, 2)
	assert.Contains(t, sessionResources, "test://resource1")
	assert.Contains(t, sessionResources, "test://resource2")

	// Test overwriting existing resources
	updatedResource := ServerResource{
		Resource: mcp.NewResource("test://resource1", "Updated Resource 1"),
		Handler: func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{
				mcp.TextResourceContents{URI: request.Params.URI, MIMEType: "text/plain", Text: "updated content 1"},
			}, nil
		},
	}

	err = server.AddSessionResources(session.SessionID(), updatedResource)
	require.NoError(t, err)

	// Verify resource was updated
	sessionResources = session.GetSessionResources() // Refresh the map
	contents, err := sessionResources["test://resource1"].Handler(ctx, mcp.ReadResourceRequest{Params: mcp.ReadResourceParams{URI: "test://resource1"}})
	require.NoError(t, err)
	assert.Equal(t, "updated content 1", contents[0].(mcp.TextResourceContents).Text)
}

// TestDeleteSessionResources tests removing resources from a session
func TestDeleteSessionResources(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithResourceCapabilities(false, true))
	ctx := context.Background()

	// Create a session with pre-existing resources
	sessionChan := make(chan mcp.JSONRPCNotification, 10)
	session := &sessionTestClientWithResources{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
		initialized:         true,
		sessionResources: map[string]ServerResource{
			"test://resource1": {Resource: mcp.NewResource("test://resource1", "Resource 1")},
			"test://resource2": {Resource: mcp.NewResource("test://resource2", "Resource 2")},
			"test://resource3": {Resource: mcp.NewResource("test://resource3", "Resource 3")},
		},
	}

	// Register the session
	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Delete subset of resources
	err = server.DeleteSessionResources(session.SessionID(), "test://resource1", "test://resource3")
	require.NoError(t, err)

	// Check that notification was sent
	select {
	case notification := <-sessionChan:
		assert.Equal(t, "notifications/resources/list_changed", notification.Method)
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected notification not received")
	}

	// Verify correct resources were removed
	sessionResources := session.GetSessionResources()
	assert.Len(t, sessionResources, 1)
	assert.NotContains(t, sessionResources, "test://resource1")
	assert.Contains(t, sessionResources, "test://resource2")
	assert.NotContains(t, sessionResources, "test://resource3")

	// Delete non-existent resource (should not error)
	err = server.DeleteSessionResources(session.SessionID(), "test://nonexistent")
	require.NoError(t, err)

	// Verify no notification is sent for non-existent resource deletion
	select {
	case <-sessionChan:
		t.Error("Unexpected notification received when deleting non-existent resource")
	case <-time.After(100 * time.Millisecond):
		// Expected: no notification for non-existent resource
	}
}

// TestSessionResourcesWithGlobalResources tests merging of global and session resources
func TestSessionResourcesWithGlobalResources(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithResourceCapabilities(false, true))
	ctx := context.Background()

	// Add global resources
	server.AddResource(
		mcp.NewResource("test://global1", "Global Resource 1"),
		func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{
				mcp.TextResourceContents{URI: request.Params.URI, MIMEType: "text/plain", Text: "global content 1"},
			}, nil
		},
	)
	server.AddResource(
		mcp.NewResource("test://global2", "Global Resource 2"),
		func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{
				mcp.TextResourceContents{URI: request.Params.URI, MIMEType: "text/plain", Text: "global content 2"},
			}, nil
		},
	)

	// Create a session
	session := &sessionTestClientWithResources{
		sessionID:           "session-1",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
		initialized:         true,
		sessionResources:    make(map[string]ServerResource),
	}

	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Add session resource that overrides a global resource
	err = server.AddSessionResource(
		session.SessionID(),
		mcp.NewResource("test://global1", "Session Override Resource"),
		func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{
				mcp.TextResourceContents{URI: request.Params.URI, MIMEType: "text/plain", Text: "session override content"},
			}, nil
		},
	)
	require.NoError(t, err)

	// Add a session-only resource
	err = server.AddSessionResource(
		session.SessionID(),
		mcp.NewResource("test://session1", "Session Resource 1"),
		func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{
				mcp.TextResourceContents{URI: request.Params.URI, MIMEType: "text/plain", Text: "session content 1"},
			}, nil
		},
	)
	require.NoError(t, err)

	// Create a context with the session for server operations
	sessionCtx := server.WithContext(ctx, session)

	// Test ListResources to verify merge behavior
	listResult, rerr := server.handleListResources(sessionCtx, "test-id", mcp.ListResourcesRequest{})
	require.Nil(t, rerr)
	require.NotNil(t, listResult)

	// Should have 3 resources: global2, session-overridden global1, and session1
	assert.Len(t, listResult.Resources, 3)

	// Verify all expected resources are present
	resourceMap := make(map[string]mcp.Resource)
	for _, r := range listResult.Resources {
		resourceMap[r.URI] = r
	}

	// Global resource 2 should appear unchanged
	assert.Contains(t, resourceMap, "test://global2")
	assert.Equal(t, "Global Resource 2", resourceMap["test://global2"].Name)

	// Global resource 1 should be overridden by session resource
	assert.Contains(t, resourceMap, "test://global1")
	assert.Equal(t, "Session Override Resource", resourceMap["test://global1"].Name)

	// Session-only resource should appear
	assert.Contains(t, resourceMap, "test://session1")
	assert.Equal(t, "Session Resource 1", resourceMap["test://session1"].Name)

	// Test ReadResource to verify handlers are correctly resolved
	// Test reading the overridden resource - should use session handler
	readResult1, rerr := server.handleReadResource(sessionCtx, "test-id", mcp.ReadResourceRequest{
		Params: mcp.ReadResourceParams{URI: "test://global1"},
	})
	require.Nil(t, rerr)
	require.NotNil(t, readResult1)
	assert.Len(t, readResult1.Contents, 1)
	assert.Equal(t, "session override content", readResult1.Contents[0].(mcp.TextResourceContents).Text)

	// Test reading global resource that wasn't overridden
	readResult2, rerr := server.handleReadResource(sessionCtx, "test-id", mcp.ReadResourceRequest{
		Params: mcp.ReadResourceParams{URI: "test://global2"},
	})
	require.Nil(t, rerr)
	require.NotNil(t, readResult2)
	assert.Len(t, readResult2.Contents, 1)
	assert.Equal(t, "global content 2", readResult2.Contents[0].(mcp.TextResourceContents).Text)

	// Test reading session-only resource
	readResult3, rerr := server.handleReadResource(sessionCtx, "test-id", mcp.ReadResourceRequest{
		Params: mcp.ReadResourceParams{URI: "test://session1"},
	})
	require.Nil(t, rerr)
	require.NotNil(t, readResult3)
	assert.Len(t, readResult3.Contents, 1)
	assert.Equal(t, "session content 1", readResult3.Contents[0].(mcp.TextResourceContents).Text)
}

// TestAddSessionResourcesUninitialized tests adding resources to uninitialized session
func TestAddSessionResourcesUninitialized(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithResourceCapabilities(false, true))
	ctx := context.Background()

	// Create an uninitialized session
	sessionChan := make(chan mcp.JSONRPCNotification, 10)
	session := &sessionTestClientWithResources{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
		initialized:         false, // Not initialized
		sessionResources:    make(map[string]ServerResource),
	}

	// Register the session
	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Add resources to uninitialized session
	err = server.AddSessionResource(
		session.SessionID(),
		mcp.NewResource("test://resource1", "Resource 1"),
		func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{
				mcp.TextResourceContents{URI: request.Params.URI, MIMEType: "text/plain", Text: "content 1"},
			}, nil
		},
	)
	require.NoError(t, err)

	// Verify NO notification was sent (session not initialized)
	select {
	case <-sessionChan:
		t.Error("Unexpected notification received for uninitialized session")
	case <-time.After(100 * time.Millisecond):
		// Expected: no notification
	}

	// Verify resource was still added
	sessionResources := session.GetSessionResources()
	assert.Len(t, sessionResources, 1)
	assert.Contains(t, sessionResources, "test://resource1")

	// Initialize session
	session.Initialize()

	// Add another resource after initialization
	err = server.AddSessionResource(
		session.SessionID(),
		mcp.NewResource("test://resource2", "Resource 2"),
		func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{
				mcp.TextResourceContents{URI: request.Params.URI, MIMEType: "text/plain", Text: "content 2"},
			}, nil
		},
	)
	require.NoError(t, err)

	// Now notification should be sent
	select {
	case notification := <-sessionChan:
		assert.Equal(t, "notifications/resources/list_changed", notification.Method)
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected notification not received after initialization")
	}

	// Verify both resources are accessible
	assert.Len(t, session.GetSessionResources(), 2)
}

// TestDeleteSessionResourcesUninitialized tests deleting resources from uninitialized session
func TestDeleteSessionResourcesUninitialized(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithResourceCapabilities(false, true))
	ctx := context.Background()

	// Create an uninitialized session with resources
	sessionChan := make(chan mcp.JSONRPCNotification, 10)
	session := &sessionTestClientWithResources{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
		initialized:         false, // Not initialized
		sessionResources: map[string]ServerResource{
			"test://resource1": {Resource: mcp.NewResource("test://resource1", "Resource 1")},
			"test://resource2": {Resource: mcp.NewResource("test://resource2", "Resource 2")},
		},
	}

	// Register the session
	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Delete resources from uninitialized session
	err = server.DeleteSessionResources(session.SessionID(), "test://resource1")
	require.NoError(t, err)

	// Verify NO notification was sent (session not initialized)
	select {
	case <-sessionChan:
		t.Error("Unexpected notification received for uninitialized session")
	case <-time.After(100 * time.Millisecond):
		// Expected: no notification
	}

	// Verify resource was still deleted
	sessionResources := session.GetSessionResources()
	assert.Len(t, sessionResources, 1)
	assert.NotContains(t, sessionResources, "test://resource1")
	assert.Contains(t, sessionResources, "test://resource2")
}

// TestSessionResourceCapabilitiesBehavior tests capability-dependent notification behavior
func TestSessionResourceCapabilitiesBehavior(t *testing.T) {
	tests := []struct {
		name               string
		setupServer        func() *MCPServer
		expectNotification bool
	}{
		{
			name: "listChanged=true sends notifications",
			setupServer: func() *MCPServer {
				return NewMCPServer("test-server", "1.0.0", WithResourceCapabilities(false, true))
			},
			expectNotification: true,
		},
		{
			name: "listChanged=false sends no notifications",
			setupServer: func() *MCPServer {
				return NewMCPServer("test-server", "1.0.0", WithResourceCapabilities(false, false))
			},
			expectNotification: false,
		},
		{
			name: "no resource capability auto-registers and sends notifications",
			setupServer: func() *MCPServer {
				return NewMCPServer("test-server", "1.0.0")
			},
			expectNotification: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setupServer()
			ctx := context.Background()

			sessionChan := make(chan mcp.JSONRPCNotification, 10)
			session := &sessionTestClientWithResources{
				sessionID:           "session-1",
				notificationChannel: sessionChan,
				initialized:         true,
				sessionResources:    make(map[string]ServerResource),
			}

			err := server.RegisterSession(ctx, session)
			require.NoError(t, err)

			// Capture pre-call state
			preAddResourceCount := len(session.sessionResources)

			// Add a resource
			err = server.AddSessionResource(
				session.SessionID(),
				mcp.NewResource("test://resource", "Test Resource"),
				func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
					return []mcp.ResourceContents{
						mcp.TextResourceContents{URI: request.Params.URI, MIMEType: "text/plain", Text: "content"},
					}, nil
				},
			)
			require.NoError(t, err)

			// Verify post-call state: resource was added
			assert.Contains(t, session.sessionResources, "test://resource", "Resource should be present after AddSessionResource")
			assert.Equal(t, preAddResourceCount+1, len(session.sessionResources), "Resource count should increase by 1")

			// Verify the listChanged default behavior
			if server.capabilities.resources != nil {
				assert.Equal(t, server.capabilities.resources.listChanged, tt.expectNotification, "listChanged value should match expectation")
			}

			// Check notification based on expectation
			if tt.expectNotification {
				select {
				case notification := <-sessionChan:
					assert.Equal(t, "notifications/resources/list_changed", notification.Method)
				case <-time.After(100 * time.Millisecond):
					t.Error("Expected notification not received")
				}
			} else {
				select {
				case <-sessionChan:
					t.Error("Unexpected notification received")
				case <-time.After(100 * time.Millisecond):
					// Expected: no notification
				}
			}

			// Verify auto-registration behavior for servers without initial resource capabilities
			if tt.name == "no resource capability auto-registers and sends notifications" {
				// After first Add, capability should be auto-registered
				assert.NotNil(t, server.capabilities.resources, "Resource capability should be auto-registered")
				// When auto-registered, default listChanged should be true
				assert.Equal(t, true, server.capabilities.resources.listChanged, "Auto-registered resources should have listChanged=true by default")
			}
		})
	}
}

// TestSessionResourceOperationsAfterUnregister tests operations on unregistered sessions
func TestSessionResourceOperationsAfterUnregister(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithResourceCapabilities(false, true))
	ctx := context.Background()

	// Create and register a session
	session := &sessionTestClientWithResources{
		sessionID:           "session-1",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
		initialized:         true,
		sessionResources:    make(map[string]ServerResource),
	}

	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Unregister the session
	server.UnregisterSession(ctx, session.SessionID())

	// Attempt to add a resource (should fail)
	err = server.AddSessionResource(
		session.SessionID(),
		mcp.NewResource("test://resource", "Test Resource"),
		func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return nil, nil
		},
	)
	assert.ErrorIs(t, err, ErrSessionNotFound)

	// Attempt to delete resources (should fail)
	err = server.DeleteSessionResources(session.SessionID(), "test://resource")
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

// TestSessionResourcesConcurrency tests thread-safe resource operations
func TestSessionResourcesConcurrency(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithResourceCapabilities(false, true))
	ctx := context.Background()

	// Create a session
	session := &sessionTestClientWithResources{
		sessionID:           "session-1",
		notificationChannel: make(chan mcp.JSONRPCNotification, 100),
		initialized:         true,
		sessionResources:    make(map[string]ServerResource),
	}

	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Run concurrent operations
	done := make(chan bool)
	errors := make(chan error, 100)

	// Goroutine 1: Add resources
	go func() {
		for i := range 10 {
			uri := fmt.Sprintf("test://resource%d", i)
			err := server.AddSessionResource(
				session.SessionID(),
				mcp.NewResource(uri, fmt.Sprintf("Resource %d", i)),
				func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
					return []mcp.ResourceContents{
						mcp.TextResourceContents{URI: request.Params.URI, MIMEType: "text/plain", Text: "content"},
					}, nil
				},
			)
			if err != nil {
				errors <- err
			}
		}
		done <- true
	}()

	// Goroutine 2: Delete resources
	go func() {
		time.Sleep(10 * time.Millisecond) // Let some adds happen first
		for i := range 5 {
			uri := fmt.Sprintf("test://resource%d", i*2)
			err := server.DeleteSessionResources(session.SessionID(), uri)
			if err != nil {
				errors <- err
			}
		}
		done <- true
	}()

	// Goroutine 3: Add and delete same resource repeatedly
	go func() {
		for range 10 {
			// Add
			err := server.AddSessionResource(
				session.SessionID(),
				mcp.NewResource("test://concurrent", "Concurrent Resource"),
				func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
					return []mcp.ResourceContents{
						mcp.TextResourceContents{URI: request.Params.URI, MIMEType: "text/plain", Text: "concurrent"},
					}, nil
				},
			)
			if err != nil {
				errors <- err
			}
			// Delete
			err = server.DeleteSessionResources(session.SessionID(), "test://concurrent")
			if err != nil {
				errors <- err
			}
		}
		done <- true
	}()

	// Wait for all goroutines to complete
	for range 3 {
		<-done
	}

	// Check for errors
	close(errors)
	for err := range errors {
		t.Errorf("Concurrent operation error: %v", err)
	}

	// Verify final state is consistent
	sessionResources := session.GetSessionResources()
	assert.NotNil(t, sessionResources)
	// The exact count depends on timing, but it should be between 0 and 10
	assert.GreaterOrEqual(t, len(sessionResources), 0)
	assert.LessOrEqual(t, len(sessionResources), 11) // 10 regular + 1 concurrent
}

// TestSessionDoesNotSupportResources tests error handling for incompatible sessions
func TestSessionDoesNotSupportResources(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0")
	ctx := context.Background()

	// Create a session that doesn't implement SessionWithResources
	session := &sessionTestClientWithTools{
		sessionID:           "session-1",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
		initialized:         true,
		sessionTools:        make(map[string]ServerTool),
	}

	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Attempt to add a resource (should fail)
	err = server.AddSessionResource(
		session.SessionID(),
		mcp.NewResource("test://resource", "Test Resource"),
		func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return nil, nil
		},
	)
	assert.ErrorIs(t, err, ErrSessionDoesNotSupportResources)

	// Attempt to add multiple resources (should fail)
	err = server.AddSessionResources(
		session.SessionID(),
		ServerResource{
			Resource: mcp.NewResource("test://resource", "Test Resource"),
			Handler: func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
				return nil, nil
			},
		},
	)
	assert.ErrorIs(t, err, ErrSessionDoesNotSupportResources)

	// Attempt to delete resources (should fail)
	err = server.DeleteSessionResources(session.SessionID(), "test://resource")
	assert.ErrorIs(t, err, ErrSessionDoesNotSupportResources)
}

// TestNotificationErrorHandling tests graceful handling of notification failures
func TestNotificationErrorHandling(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithResourceCapabilities(false, true))

	// Set up error tracking with a channel to avoid race conditions
	errorChan := make(chan error, 1)
	if server.hooks == nil {
		server.hooks = &Hooks{}
	}
	server.hooks.OnError = []OnErrorHookFunc{
		func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
			select {
			case errorChan <- err:
			default:
				// Channel already has an error, ignore subsequent ones
			}
		},
	}

	ctx := context.Background()

	// Create a session with a blocking notification channel
	blockingChan := make(chan mcp.JSONRPCNotification) // No buffer, will block
	session := &sessionTestClientWithResources{
		sessionID:           "session-1",
		notificationChannel: blockingChan,
		initialized:         true,
		sessionResources:    make(map[string]ServerResource),
	}

	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Add a resource (notification will fail due to blocking channel)
	err = server.AddSessionResource(
		session.SessionID(),
		mcp.NewResource("test://resource", "Test Resource"),
		func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{
				mcp.TextResourceContents{URI: request.Params.URI, MIMEType: "text/plain", Text: "content"},
			}, nil
		},
	)

	// Operation should succeed despite notification failure
	require.NoError(t, err)

	// Wait for the error to be logged
	select {
	case capturedError := <-errorChan:
		// Verify error was logged
		assert.NotNil(t, capturedError)
		assert.Contains(t, capturedError.Error(), "channel")
	case <-time.After(200 * time.Millisecond):
		t.Error("Expected error was not logged")
	}

	// Verify resource was actually added despite notification failure
	sessionResources := session.GetSessionResources()
	assert.Len(t, sessionResources, 1)
	assert.Contains(t, sessionResources, "test://resource")
}
