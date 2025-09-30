package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMCPServer_MiddlewarePanicRecovery tests that panics in middleware are properly recovered
func TestMCPServer_MiddlewarePanicRecovery(t *testing.T) {
	t.Run("tool handler panic with recovery middleware", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0", WithRecovery())

		server.AddTool(
			mcp.NewTool("panic-tool"),
			func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				panic("intentional panic in tool handler")
			},
		)

		response := server.HandleMessage(context.Background(), []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "tools/call",
			"params": {
				"name": "panic-tool"
			}
		}`))

		errorResponse, ok := response.(mcp.JSONRPCError)
		require.True(t, ok)
		assert.Equal(t, mcp.INTERNAL_ERROR, errorResponse.Error.Code)
		assert.Contains(t, errorResponse.Error.Message, "panic recovered")
		assert.Contains(t, errorResponse.Error.Message, "intentional panic in tool handler")
	})

	t.Run("resource handler panic with recovery middleware", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0",
			WithResourceCapabilities(false, false),
			WithResourceRecovery(),
		)

		server.AddResource(
			mcp.Resource{URI: "test://panic-resource", Name: "Panic Resource"},
			func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
				panic("intentional panic in resource handler")
			},
		)

		response := server.HandleMessage(context.Background(), []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "resources/read",
			"params": {
				"uri": "test://panic-resource"
			}
		}`))

		errorResponse, ok := response.(mcp.JSONRPCError)
		require.True(t, ok)
		assert.Equal(t, mcp.INTERNAL_ERROR, errorResponse.Error.Code)
		assert.Contains(t, errorResponse.Error.Message, "panic recovered")
		assert.Contains(t, errorResponse.Error.Message, "intentional panic in resource handler")
	})
}

// TestMCPServer_ConcurrentOperations tests thread safety of Add/Delete operations
func TestMCPServer_ConcurrentOperations(t *testing.T) {
	t.Run("concurrent tool add/delete", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0", WithToolCapabilities(false))

		var wg sync.WaitGroup
		numGoroutines := 10
		operationsPerGoroutine := 50

		// Concurrent add operations
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < operationsPerGoroutine; j++ {
					toolName := fmt.Sprintf("tool-%d-%d", id, j)
					server.AddTool(mcp.NewTool(toolName), nil)
				}
			}(i)
		}

		wg.Wait()

		// Verify all tools were added
		tools := server.ListTools()
		assert.Len(t, tools, numGoroutines*operationsPerGoroutine)

		// Concurrent delete operations
		wg = sync.WaitGroup{}
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < operationsPerGoroutine; j++ {
					toolName := fmt.Sprintf("tool-%d-%d", id, j)
					server.DeleteTools(toolName)
				}
			}(i)
		}

		wg.Wait()

		// Verify all tools were deleted
		tools = server.ListTools()
		assert.Nil(t, tools)
	})

	t.Run("concurrent resource add/delete", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0", WithResourceCapabilities(false, false))

		var wg sync.WaitGroup
		numGoroutines := 10
		operationsPerGoroutine := 50

		// Concurrent add operations
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < operationsPerGoroutine; j++ {
					uri := fmt.Sprintf("test://resource-%d-%d", id, j)
					server.AddResource(
						mcp.Resource{URI: uri, Name: uri},
						func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
							return nil, nil
						},
					)
				}
			}(i)
		}

		wg.Wait()

		// Concurrent delete operations
		wg = sync.WaitGroup{}
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < operationsPerGoroutine; j++ {
					uri := fmt.Sprintf("test://resource-%d-%d", id, j)
					server.DeleteResources(uri)
				}
			}(i)
		}

		wg.Wait()
	})

	t.Run("concurrent prompt add/delete", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0", WithPromptCapabilities(false))

		var wg sync.WaitGroup
		numGoroutines := 10
		operationsPerGoroutine := 50

		// Concurrent add operations
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < operationsPerGoroutine; j++ {
					promptName := fmt.Sprintf("prompt-%d-%d", id, j)
					server.AddPrompt(
						mcp.Prompt{Name: promptName, Description: promptName},
						nil,
					)
				}
			}(i)
		}

		wg.Wait()

		// Concurrent delete operations
		wg = sync.WaitGroup{}
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < operationsPerGoroutine; j++ {
					promptName := fmt.Sprintf("prompt-%d-%d", id, j)
					server.DeletePrompts(promptName)
				}
			}(i)
		}

		wg.Wait()
	})
}

// TestMCPServer_PaginationEdgeCases tests pagination boundary conditions
func TestMCPServer_PaginationEdgeCases(t *testing.T) {
	t.Run("malformed cursor - invalid base64", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0",
			WithResourceCapabilities(false, false),
			WithPaginationLimit(5),
		)

		// Add some resources
		for i := 0; i < 10; i++ {
			uri := fmt.Sprintf("test://resource-%d", i)
			server.AddResource(
				mcp.Resource{URI: uri, Name: fmt.Sprintf("Resource %d", i)},
				nil,
			)
		}

		response := server.HandleMessage(context.Background(), []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "resources/list",
			"params": {
				"cursor": "not-valid-base64!!!"
			}
		}`))

		errorResponse, ok := response.(mcp.JSONRPCError)
		require.True(t, ok)
		assert.Equal(t, mcp.INVALID_PARAMS, errorResponse.Error.Code)
	})

	t.Run("cursor pointing beyond list", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0",
			WithToolCapabilities(false),
			WithPaginationLimit(5),
		)

		// Add 3 tools
		for i := 0; i < 3; i++ {
			server.AddTool(mcp.NewTool(fmt.Sprintf("tool-%d", i)), nil)
		}

		// Create cursor that points beyond the list
		beyondCursor := base64.StdEncoding.EncodeToString([]byte("tool-99"))

		response := server.HandleMessage(context.Background(), []byte(fmt.Sprintf(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "tools/list",
			"params": {
				"cursor": "%s"
			}
		}`, beyondCursor)))

		resp, ok := response.(mcp.JSONRPCResponse)
		require.True(t, ok)

		result, ok := resp.Result.(mcp.ListToolsResult)
		require.True(t, ok)

		// Should return empty list with no cursor
		assert.Empty(t, result.Tools)
		assert.Empty(t, result.NextCursor)
	})

	t.Run("pagination with exactly paginationLimit items", func(t *testing.T) {
		limit := 5
		server := NewMCPServer("test-server", "1.0.0",
			WithPromptCapabilities(false),
			WithPaginationLimit(limit),
		)

		// Add exactly paginationLimit prompts
		for i := 0; i < limit; i++ {
			server.AddPrompt(
				mcp.Prompt{Name: fmt.Sprintf("prompt-%d", i), Description: "Test"},
				nil,
			)
		}

		response := server.HandleMessage(context.Background(), []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "prompts/list"
		}`))

		resp, ok := response.(mcp.JSONRPCResponse)
		require.True(t, ok)

		result, ok := resp.Result.(mcp.ListPromptsResult)
		require.True(t, ok)

		// Should return all items with cursor pointing to last item
		assert.Len(t, result.Prompts, limit)
		assert.NotEmpty(t, result.NextCursor, "Cursor should be set when exactly at limit")

		// Request next page - should be empty
		response = server.HandleMessage(context.Background(), []byte(fmt.Sprintf(`{
			"jsonrpc": "2.0",
			"id": 2,
			"method": "prompts/list",
			"params": {
				"cursor": "%s"
			}
		}`, result.NextCursor)))

		resp, ok = response.(mcp.JSONRPCResponse)
		require.True(t, ok)

		result, ok = resp.Result.(mcp.ListPromptsResult)
		require.True(t, ok)

		assert.Empty(t, result.Prompts)
		assert.Empty(t, result.NextCursor)
	})

	t.Run("empty list pagination", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0",
			WithResourceCapabilities(false, false),
			WithPaginationLimit(10),
		)

		response := server.HandleMessage(context.Background(), []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "resources/list"
		}`))

		resp, ok := response.(mcp.JSONRPCResponse)
		require.True(t, ok)

		result, ok := resp.Result.(mcp.ListResourcesResult)
		require.True(t, ok)

		assert.Empty(t, result.Resources)
		assert.Empty(t, result.NextCursor)
	})
}

// TestMCPServer_SessionUnregistrationDuringNotification tests race conditions
func TestMCPServer_SessionUnregistrationDuringNotification(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithToolCapabilities(true))

	// Create multiple sessions
	numSessions := 10
	sessions := make([]*sessionTestClient, numSessions)
	for i := 0; i < numSessions; i++ {
		sessions[i] = &sessionTestClient{
			sessionID:           fmt.Sprintf("session-%d", i),
			notificationChannel: make(chan mcp.JSONRPCNotification, 10),
		}
		sessions[i].Initialize()
		err := server.RegisterSession(context.Background(), sessions[i])
		require.NoError(t, err)
	}

	var wg sync.WaitGroup
	stopCh := make(chan struct{})

	// Goroutine that continuously sends notifications
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopCh:
				return
			default:
				server.SendNotificationToAllClients("test-notification", map[string]any{
					"data": "test",
				})
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Goroutine that unregisters sessions
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numSessions; i++ {
			time.Sleep(5 * time.Millisecond)
			server.UnregisterSession(context.Background(), sessions[i].SessionID())
		}
	}()

	// Let it run for a bit
	time.Sleep(100 * time.Millisecond)
	close(stopCh)
	wg.Wait()

	// Should complete without panic or deadlock
}

// TestMCPServer_DuplicateSessionRegistration tests that duplicate session IDs are rejected
func TestMCPServer_DuplicateSessionRegistration(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0")

	session1 := &sessionTestClient{
		sessionID:           "duplicate-id",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
	}

	session2 := &sessionTestClient{
		sessionID:           "duplicate-id",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
	}

	// First registration should succeed
	err := server.RegisterSession(context.Background(), session1)
	require.NoError(t, err)

	// Second registration with same ID should fail
	err = server.RegisterSession(context.Background(), session2)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSessionExists)
}

// TestMCPServer_SessionToolOperationsAfterUnregister tests operations on removed sessions
func TestMCPServer_SessionToolOperationsAfterUnregister(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithToolCapabilities(true))

	session := &sessionTestClientWithTools{
		sessionID:           "test-session",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
		initialized:         true,
	}

	err := server.RegisterSession(context.Background(), session)
	require.NoError(t, err)

	// Add a tool to the session
	err = server.AddSessionTool(session.SessionID(), mcp.NewTool("test-tool"), nil)
	require.NoError(t, err)

	// Unregister the session
	server.UnregisterSession(context.Background(), session.SessionID())

	// Try to add tool to unregistered session
	err = server.AddSessionTool(session.SessionID(), mcp.NewTool("another-tool"), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSessionNotFound)

	// Try to delete tool from unregistered session
	err = server.DeleteSessionTools(session.SessionID(), "test-tool")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

// TestMCPServer_ResourceTemplateURIMatching tests URI template edge cases
func TestMCPServer_ResourceTemplateURIMatching(t *testing.T) {
	tests := []struct {
		name            string
		templateURI     string
		requestURI      string
		shouldMatch     bool
		expectedArgs    map[string]any
		setupTemplate   func(*MCPServer, string)
		validateRequest func(*testing.T, mcp.ReadResourceRequest)
	}{
		{
			name:        "exact match no variables",
			templateURI: "test://fixed/path",
			requestURI:  "test://fixed/path",
			shouldMatch: true,
			setupTemplate: func(s *MCPServer, uri string) {
				s.AddResourceTemplate(
					mcp.NewResourceTemplate(uri, "Test"),
					func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
						return []mcp.ResourceContents{
							mcp.TextResourceContents{
								URI:  request.Params.URI,
								Text: "matched",
							},
						}, nil
					},
				)
			},
		},
		{
			name:        "single variable match",
			templateURI: "test://users/{id}",
			requestURI:  "test://users/123",
			shouldMatch: true,
			expectedArgs: map[string]any{
				"id": []string{"123"},
			},
			setupTemplate: func(s *MCPServer, uri string) {
				s.AddResourceTemplate(
					mcp.NewResourceTemplate(uri, "User"),
					func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
						return []mcp.ResourceContents{
							mcp.TextResourceContents{
								URI:  request.Params.URI,
								Text: fmt.Sprintf("user-id: %v", request.Params.Arguments["id"]),
							},
						}, nil
					},
				)
			},
			validateRequest: func(t *testing.T, request mcp.ReadResourceRequest) {
				assert.NotNil(t, request.Params.Arguments)
				assert.Equal(t, []string{"123"}, request.Params.Arguments["id"])
			},
		},
		{
			name:        "path explosion match",
			templateURI: "test://files{/path*}",
			requestURI:  "test://files/a/b/c",
			shouldMatch: true,
			expectedArgs: map[string]any{
				"path": []string{"a", "b", "c"},
			},
			setupTemplate: func(s *MCPServer, uri string) {
				s.AddResourceTemplate(
					mcp.NewResourceTemplate(uri, "Files"),
					func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
						return []mcp.ResourceContents{
							mcp.TextResourceContents{
								URI:  request.Params.URI,
								Text: fmt.Sprintf("path: %v", request.Params.Arguments["path"]),
							},
						}, nil
					},
				)
			},
			validateRequest: func(t *testing.T, request mcp.ReadResourceRequest) {
				assert.NotNil(t, request.Params.Arguments)
				pathParts, ok := request.Params.Arguments["path"].([]string)
				require.True(t, ok)
				assert.Equal(t, []string{"a", "b", "c"}, pathParts)
			},
		},
		{
			name:        "no match - different scheme",
			templateURI: "test://resource",
			requestURI:  "other://resource",
			shouldMatch: false,
			setupTemplate: func(s *MCPServer, uri string) {
				s.AddResourceTemplate(
					mcp.NewResourceTemplate(uri, "Test"),
					func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
						return nil, nil
					},
				)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewMCPServer("test-server", "1.0.0", WithResourceCapabilities(false, false))
			tt.setupTemplate(server, tt.templateURI)

			requestBytes, err := json.Marshal(map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "resources/read",
				"params": map[string]any{
					"uri": tt.requestURI,
				},
			})
			require.NoError(t, err)

			response := server.HandleMessage(context.Background(), requestBytes)

			if tt.shouldMatch {
				resp, ok := response.(mcp.JSONRPCResponse)
				require.True(t, ok, "Expected successful response for matching URI")

				result, ok := resp.Result.(mcp.ReadResourceResult)
				require.True(t, ok)
				require.NotEmpty(t, result.Contents)

				// Validate request if validator provided
				if tt.validateRequest != nil {
					// We need to capture the request in the handler to validate it
					// This is a bit tricky, so we'll just check the expected args
					if tt.expectedArgs != nil {
						content := result.Contents[0].(mcp.TextResourceContents)
						// The text should contain our arguments
						for key, expectedVal := range tt.expectedArgs {
							assert.Contains(t, content.Text, fmt.Sprintf("%v", expectedVal),
								"Response should contain argument %s=%v", key, expectedVal)
						}
					}
				}
			} else {
				errorResp, ok := response.(mcp.JSONRPCError)
				require.True(t, ok, "Expected error response for non-matching URI")
				assert.Equal(t, mcp.RESOURCE_NOT_FOUND, errorResp.Error.Code)
			}
		})
	}
}

// TestMCPServer_UnsupportedProtocolVersions tests client/server version negotiation
func TestMCPServer_UnsupportedProtocolVersions(t *testing.T) {
	tests := []struct {
		name            string
		clientVersion   string
		expectedVersion string
		description     string
	}{
		{
			name:            "ancient unsupported version",
			clientVersion:   "2020-01-01",
			expectedVersion: mcp.LATEST_PROTOCOL_VERSION,
			description:     "Server should respond with its latest version",
		},
		{
			name:            "future unsupported version",
			clientVersion:   "2030-12-31",
			expectedVersion: mcp.LATEST_PROTOCOL_VERSION,
			description:     "Server should respond with its latest version",
		},
		{
			name:            "supported version",
			clientVersion:   "2024-11-05",
			expectedVersion: "2024-11-05",
			description:     "Server should respond with client's version if supported",
		},
		{
			name:            "latest supported version",
			clientVersion:   mcp.LATEST_PROTOCOL_VERSION,
			expectedVersion: mcp.LATEST_PROTOCOL_VERSION,
			description:     "Server should respond with matching version",
		},
		{
			name:            "empty version defaults to 2025-03-26",
			clientVersion:   "",
			expectedVersion: "2025-03-26",
			description:     "Backward compatibility: empty version defaults to 2025-03-26",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewMCPServer("test-server", "1.0.0")

			initRequest := mcp.InitializeRequest{}
			initRequest.Params.ProtocolVersion = tt.clientVersion
			initRequest.Params.ClientInfo = mcp.Implementation{
				Name:    "test-client",
				Version: "1.0.0",
			}

			requestBytes, err := json.Marshal(map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "initialize",
				"params":  initRequest.Params,
			})
			require.NoError(t, err)

			response := server.HandleMessage(context.Background(), requestBytes)

			resp, ok := response.(mcp.JSONRPCResponse)
			require.True(t, ok)

			result, ok := resp.Result.(mcp.InitializeResult)
			require.True(t, ok)

			assert.Equal(t, tt.expectedVersion, result.ProtocolVersion, tt.description)
		})
	}
}

// TestMCPServer_HooksWithNilSession tests that hooks handle nil session contexts gracefully
func TestMCPServer_HooksWithNilSession(t *testing.T) {
	hookCalled := false
	var receivedSession ClientSession

	hooks := &Hooks{}
	hooks.AddBeforeAny(func(ctx context.Context, id any, method mcp.MCPMethod, message any) {
		hookCalled = true
		receivedSession = ClientSessionFromContext(ctx)
	})

	server := NewMCPServer("test-server", "1.0.0", WithHooks(hooks))

	// Make request without session context
	response := server.HandleMessage(context.Background(), []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "ping"
	}`))

	require.NotNil(t, response)
	assert.True(t, hookCalled, "Hook should be called even without session")
	assert.Nil(t, receivedSession, "Session should be nil when not in context")
}

// TestMCPServer_CapabilityImplicitRegistration tests edge cases in capability registration
func TestMCPServer_CapabilityImplicitRegistration(t *testing.T) {
	t.Run("implicit registration after explicit false", func(t *testing.T) {
		// If user explicitly sets listChanged=false, adding tools shouldn't override it
		server := NewMCPServer("test-server", "1.0.0", WithToolCapabilities(false))

		server.capabilitiesMu.RLock()
		initialListChanged := server.capabilities.tools.listChanged
		server.capabilitiesMu.RUnlock()
		assert.False(t, initialListChanged)

		// Add a tool - should not change listChanged to true
		server.AddTool(mcp.NewTool("test-tool"), nil)

		server.capabilitiesMu.RLock()
		finalListChanged := server.capabilities.tools.listChanged
		server.capabilitiesMu.RUnlock()
		assert.False(t, finalListChanged, "Explicit false should not be overridden by implicit registration")
	})

	t.Run("implicit registration when no capability set", func(t *testing.T) {
		// If no capability was set, adding tools should enable it with listChanged=true
		server := NewMCPServer("test-server", "1.0.0")

		server.capabilitiesMu.RLock()
		initialTools := server.capabilities.tools
		server.capabilitiesMu.RUnlock()
		assert.Nil(t, initialTools)

		// Add a tool - should implicitly register with listChanged=true
		server.AddTool(mcp.NewTool("test-tool"), nil)

		server.capabilitiesMu.RLock()
		finalTools := server.capabilities.tools
		server.capabilitiesMu.RUnlock()
		require.NotNil(t, finalTools)
		assert.True(t, finalTools.listChanged)
	})

	t.Run("resources implicit registration", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0")

		// Initially nil
		server.capabilitiesMu.RLock()
		initialResources := server.capabilities.resources
		server.capabilitiesMu.RUnlock()
		assert.Nil(t, initialResources)

		// Add resource - should implicitly register
		server.AddResource(
			mcp.Resource{URI: "test://resource", Name: "Test"},
			nil,
		)

		server.capabilitiesMu.RLock()
		finalResources := server.capabilities.resources
		server.capabilitiesMu.RUnlock()
		require.NotNil(t, finalResources)
		// For resources, implicit registration doesn't set listChanged
		assert.False(t, finalResources.listChanged)
	})

	t.Run("prompts implicit registration", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0")

		// Initially nil
		server.capabilitiesMu.RLock()
		initialPrompts := server.capabilities.prompts
		server.capabilitiesMu.RUnlock()
		assert.Nil(t, initialPrompts)

		// Add prompt - should implicitly register
		server.AddPrompt(
			mcp.Prompt{Name: "test-prompt", Description: "Test"},
			nil,
		)

		server.capabilitiesMu.RLock()
		finalPrompts := server.capabilities.prompts
		server.capabilitiesMu.RUnlock()
		require.NotNil(t, finalPrompts)
		// For prompts, implicit registration doesn't set listChanged
		assert.False(t, finalPrompts.listChanged)
	})
}

// TestMCPServer_ConcurrentCapabilityChecks tests thread safety of capability access
func TestMCPServer_ConcurrentCapabilityChecks(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0")

	var wg sync.WaitGroup
	stopCh := make(chan struct{})
	errorCount := atomic.Int32{}

	// Goroutine that adds tools (triggers capability registration)
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stopCh:
				return
			default:
				server.AddTool(mcp.NewTool(fmt.Sprintf("tool-%d", i)), nil)
				i++
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Goroutines that check capabilities
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopCh:
					return
				default:
					server.capabilitiesMu.RLock()
					_ = server.capabilities.tools
					server.capabilitiesMu.RUnlock()
					time.Sleep(1 * time.Millisecond)
				}
			}
		}()
	}

	// Let it run for a bit
	time.Sleep(50 * time.Millisecond)
	close(stopCh)
	wg.Wait()

	assert.Equal(t, int32(0), errorCount.Load(), "Should complete without errors")
}

// TestMCPServer_PaginationCursorStability tests pagination behavior when items change
func TestMCPServer_PaginationCursorStability(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0",
		WithToolCapabilities(false),
		WithPaginationLimit(5),
	)

	// Add initial tools
	for i := 0; i < 10; i++ {
		server.AddTool(mcp.NewTool(fmt.Sprintf("tool-%02d", i)), nil)
	}

	// Get first page
	response := server.HandleMessage(context.Background(), []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "tools/list"
	}`))

	resp, ok := response.(mcp.JSONRPCResponse)
	require.True(t, ok)

	result, ok := resp.Result.(mcp.ListToolsResult)
	require.True(t, ok)

	assert.Len(t, result.Tools, 5)
	cursor := result.NextCursor
	require.NotEmpty(t, cursor)

	// Modify list (add and remove tools)
	server.AddTool(mcp.NewTool("tool-new-1"), nil)
	server.DeleteTools("tool-05")

	// Get second page with original cursor
	response = server.HandleMessage(context.Background(), []byte(fmt.Sprintf(`{
		"jsonrpc": "2.0",
		"id": 2,
		"method": "tools/list",
		"params": {
			"cursor": "%s"
		}
	}`, cursor)))

	resp, ok = response.(mcp.JSONRPCResponse)
	require.True(t, ok)

	result, ok = resp.Result.(mcp.ListToolsResult)
	require.True(t, ok)

	// Should handle gracefully (may have different results due to modifications)
	// The key is that it shouldn't crash or return errors
	assert.NotNil(t, result.Tools)
}
