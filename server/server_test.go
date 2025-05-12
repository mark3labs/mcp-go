package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPServer_NewMCPServer(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0")
	assert.NotNil(t, server)
	assert.Equal(t, "test-server", server.name)
	assert.Equal(t, "1.0.0", server.version)
}

func TestMCPServer_Capabilities(t *testing.T) {
	tests := []struct {
		name     string
		options  []ServerOption
		validate func(t *testing.T, response mcp.JSONRPCMessage)
	}{
		{
			name:    "No capabilities",
			options: []ServerOption{},
			validate: func(t *testing.T, response mcp.JSONRPCMessage) {
				resp, ok := response.(mcp.JSONRPCResponse)
				assert.True(t, ok)

				initResult, ok := resp.Result.(mcp.InitializeResult)
				assert.True(t, ok)

				assert.Equal(
					t,
					mcp.LATEST_PROTOCOL_VERSION,
					initResult.ProtocolVersion,
				)
				assert.Equal(t, "test-server", initResult.ServerInfo.Name)
				assert.Equal(t, "1.0.0", initResult.ServerInfo.Version)
				assert.Nil(t, initResult.Capabilities.Resources)
				assert.Nil(t, initResult.Capabilities.Prompts)
				assert.Nil(t, initResult.Capabilities.Tools)
				assert.Nil(t, initResult.Capabilities.Logging)
			},
		},
		{
			name: "All capabilities",
			options: []ServerOption{
				WithResourceCapabilities(true, true),
				WithPromptCapabilities(true),
				WithToolCapabilities(true),
				WithLogging(),
			},
			validate: func(t *testing.T, response mcp.JSONRPCMessage) {
				resp, ok := response.(mcp.JSONRPCResponse)
				assert.True(t, ok)

				initResult, ok := resp.Result.(mcp.InitializeResult)
				assert.True(t, ok)

				assert.Equal(
					t,
					mcp.LATEST_PROTOCOL_VERSION,
					initResult.ProtocolVersion,
				)
				assert.Equal(t, "test-server", initResult.ServerInfo.Name)
				assert.Equal(t, "1.0.0", initResult.ServerInfo.Version)

				assert.NotNil(t, initResult.Capabilities.Resources)

				assert.True(t, initResult.Capabilities.Resources.Subscribe)
				assert.True(t, initResult.Capabilities.Resources.ListChanged)

				assert.NotNil(t, initResult.Capabilities.Prompts)
				assert.True(t, initResult.Capabilities.Prompts.ListChanged)

				assert.NotNil(t, initResult.Capabilities.Tools)
				assert.True(t, initResult.Capabilities.Tools.ListChanged)

				assert.NotNil(t, initResult.Capabilities.Logging)
			},
		},
		{
			name: "Specific capabilities",
			options: []ServerOption{
				WithResourceCapabilities(true, false),
				WithPromptCapabilities(true),
				WithToolCapabilities(false),
				WithLogging(),
			},
			validate: func(t *testing.T, response mcp.JSONRPCMessage) {
				resp, ok := response.(mcp.JSONRPCResponse)
				assert.True(t, ok)

				initResult, ok := resp.Result.(mcp.InitializeResult)
				assert.True(t, ok)

				assert.Equal(
					t,
					mcp.LATEST_PROTOCOL_VERSION,
					initResult.ProtocolVersion,
				)
				assert.Equal(t, "test-server", initResult.ServerInfo.Name)
				assert.Equal(t, "1.0.0", initResult.ServerInfo.Version)

				assert.NotNil(t, initResult.Capabilities.Resources)

				assert.True(t, initResult.Capabilities.Resources.Subscribe)
				assert.False(t, initResult.Capabilities.Resources.ListChanged)

				assert.NotNil(t, initResult.Capabilities.Prompts)
				assert.True(t, initResult.Capabilities.Prompts.ListChanged)

				// Tools capability should be non-nil even when WithToolCapabilities(false) is used
				assert.NotNil(t, initResult.Capabilities.Tools)
				assert.False(t, initResult.Capabilities.Tools.ListChanged)

				assert.NotNil(t, initResult.Capabilities.Logging)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewMCPServer("test-server", "1.0.0", tt.options...)
			message := mcp.JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Request: mcp.Request{
					Method: "initialize",
				},
			}
			messageBytes, err := json.Marshal(message)
			assert.NoError(t, err)

			response := server.HandleMessage(context.Background(), messageBytes)
			tt.validate(t, response)
		})
	}
}

func TestMCPServer_Tools(t *testing.T) {
	tests := []struct {
		name                  string
		action                func(*testing.T, *MCPServer, chan mcp.JSONRPCNotification)
		expectedNotifications int
		validate              func(*testing.T, []mcp.JSONRPCNotification, mcp.JSONRPCMessage)
	}{
		{
			name: "SetTools sends no notifications/tools/list_changed without active sessions",
			action: func(t *testing.T, server *MCPServer, notificationChannel chan mcp.JSONRPCNotification) {
				server.SetTools(ServerTool{
					Tool: mcp.NewTool("test-tool-1"),
					Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						return &mcp.CallToolResult{}, nil
					},
				}, ServerTool{
					Tool: mcp.NewTool("test-tool-2"),
					Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						return &mcp.CallToolResult{}, nil
					},
				})
			},
			expectedNotifications: 0,
			validate: func(t *testing.T, notifications []mcp.JSONRPCNotification, toolsList mcp.JSONRPCMessage) {
				tools := toolsList.(mcp.JSONRPCResponse).Result.(mcp.ListToolsResult).Tools
				assert.Len(t, tools, 2)
				assert.Equal(t, "test-tool-1", tools[0].Name)
				assert.Equal(t, "test-tool-2", tools[1].Name)
			},
		},
		{
			name: "SetTools sends single notifications/tools/list_changed with one active session",
			action: func(t *testing.T, server *MCPServer, notificationChannel chan mcp.JSONRPCNotification) {
				err := server.RegisterSession(context.TODO(), &fakeSession{
					sessionID:           "test",
					notificationChannel: notificationChannel,
					initialized:         true,
				})
				require.NoError(t, err)
				server.SetTools(ServerTool{
					Tool: mcp.NewTool("test-tool-1"),
					Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						return &mcp.CallToolResult{}, nil
					},
				}, ServerTool{
					Tool: mcp.NewTool("test-tool-2"),
					Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						return &mcp.CallToolResult{}, nil
					},
				})
			},
			expectedNotifications: 1,
			validate: func(t *testing.T, notifications []mcp.JSONRPCNotification, toolsList mcp.JSONRPCMessage) {
				assert.Equal(t, mcp.MethodNotificationToolsListChanged, notifications[0].Method)
				tools := toolsList.(mcp.JSONRPCResponse).Result.(mcp.ListToolsResult).Tools
				assert.Len(t, tools, 2)
				assert.Equal(t, "test-tool-1", tools[0].Name)
				assert.Equal(t, "test-tool-2", tools[1].Name)
			},
		},
		{
			name: "SetTools sends single notifications/tools/list_changed per each active session",
			action: func(t *testing.T, server *MCPServer, notificationChannel chan mcp.JSONRPCNotification) {
				for i := range 5 {
					err := server.RegisterSession(context.TODO(), &fakeSession{
						sessionID:           fmt.Sprintf("test%d", i),
						notificationChannel: notificationChannel,
						initialized:         true,
					})
					require.NoError(t, err)
				}
				// also let's register inactive sessions
				for i := range 5 {
					err := server.RegisterSession(context.TODO(), &fakeSession{
						sessionID:           fmt.Sprintf("test%d", i+5),
						notificationChannel: notificationChannel,
						initialized:         false,
					})
					require.NoError(t, err)
				}
				server.SetTools(ServerTool{
					Tool: mcp.NewTool("test-tool-1"),
					Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						return &mcp.CallToolResult{}, nil
					},
				}, ServerTool{
					Tool: mcp.NewTool("test-tool-2"),
					Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						return &mcp.CallToolResult{}, nil
					},
				})
			},
			expectedNotifications: 5,
			validate: func(t *testing.T, notifications []mcp.JSONRPCNotification, toolsList mcp.JSONRPCMessage) {
				for _, notification := range notifications {
					assert.Equal(t, mcp.MethodNotificationToolsListChanged, notification.Method)
				}
				tools := toolsList.(mcp.JSONRPCResponse).Result.(mcp.ListToolsResult).Tools
				assert.Len(t, tools, 2)
				assert.Equal(t, "test-tool-1", tools[0].Name)
				assert.Equal(t, "test-tool-2", tools[1].Name)
			},
		},
		{
			name: "AddTool sends multiple notifications/tools/list_changed",
			action: func(t *testing.T, server *MCPServer, notificationChannel chan mcp.JSONRPCNotification) {
				err := server.RegisterSession(context.TODO(), &fakeSession{
					sessionID:           "test",
					notificationChannel: notificationChannel,
					initialized:         true,
				})
				require.NoError(t, err)
				server.AddTool(mcp.NewTool("test-tool-1"),
					func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						return &mcp.CallToolResult{}, nil
					})
				server.AddTool(mcp.NewTool("test-tool-2"),
					func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						return &mcp.CallToolResult{}, nil
					})
			},
			expectedNotifications: 2,
			validate: func(t *testing.T, notifications []mcp.JSONRPCNotification, toolsList mcp.JSONRPCMessage) {
				assert.Equal(t, mcp.MethodNotificationToolsListChanged, notifications[0].Method)
				assert.Equal(t, mcp.MethodNotificationToolsListChanged, notifications[1].Method)
				tools := toolsList.(mcp.JSONRPCResponse).Result.(mcp.ListToolsResult).Tools
				assert.Len(t, tools, 2)
				assert.Equal(t, "test-tool-1", tools[0].Name)
				assert.Equal(t, "test-tool-2", tools[1].Name)
			},
		},
		{
			name: "DeleteTools sends single notifications/tools/list_changed",
			action: func(t *testing.T, server *MCPServer, notificationChannel chan mcp.JSONRPCNotification) {
				err := server.RegisterSession(context.TODO(), &fakeSession{
					sessionID:           "test",
					notificationChannel: notificationChannel,
					initialized:         true,
				})
				require.NoError(t, err)
				server.SetTools(
					ServerTool{Tool: mcp.NewTool("test-tool-1")},
					ServerTool{Tool: mcp.NewTool("test-tool-2")})
				server.DeleteTools("test-tool-1", "test-tool-2")
			},
			expectedNotifications: 2,
			validate: func(t *testing.T, notifications []mcp.JSONRPCNotification, toolsList mcp.JSONRPCMessage) {
				// One for SetTools
				assert.Equal(t, mcp.MethodNotificationToolsListChanged, notifications[0].Method)
				// One for DeleteTools
				assert.Equal(t, mcp.MethodNotificationToolsListChanged, notifications[1].Method)

				// Expect a successful response with an empty list of tools
				resp, ok := toolsList.(mcp.JSONRPCResponse)
				assert.True(t, ok, "Expected JSONRPCResponse, got %T", toolsList)

				result, ok := resp.Result.(mcp.ListToolsResult)
				assert.True(t, ok, "Expected ListToolsResult, got %T", resp.Result)

				assert.Empty(t, result.Tools, "Expected empty tools list")
			},
		},
		{
			name: "DeleteTools with non-existent tools does nothing and not receives notifications from MCPServer",
			action: func(t *testing.T, server *MCPServer, notificationChannel chan mcp.JSONRPCNotification) {
				err := server.RegisterSession(context.TODO(), &fakeSession{
					sessionID:           "test",
					notificationChannel: notificationChannel,
					initialized:         true,
				})
				require.NoError(t, err)
				server.SetTools(
					ServerTool{Tool: mcp.NewTool("test-tool-1")},
					ServerTool{Tool: mcp.NewTool("test-tool-2")})

				// Remove non-existing tools
				server.DeleteTools("test-tool-3", "test-tool-4")
			},
			expectedNotifications: 1,
			validate: func(t *testing.T, notifications []mcp.JSONRPCNotification, toolsList mcp.JSONRPCMessage) {
				// Only one notification expected for SetTools
				assert.Equal(t, mcp.MethodNotificationToolsListChanged, notifications[0].Method)

				// Confirm the tool list does not change
				tools := toolsList.(mcp.JSONRPCResponse).Result.(mcp.ListToolsResult).Tools
				assert.Len(t, tools, 2)
				assert.Equal(t, "test-tool-1", tools[0].Name)
				assert.Equal(t, "test-tool-2", tools[1].Name)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			server := NewMCPServer("test-server", "1.0.0", WithToolCapabilities(true))
			_ = server.HandleMessage(ctx, []byte(`{
				"jsonrpc": "2.0",
				"id": 1,
				"method": "initialize"
			}`))
			notificationChannel := make(chan mcp.JSONRPCNotification, 100)
			notifications := make([]mcp.JSONRPCNotification, 0)
			tt.action(t, server, notificationChannel)
			for done := false; !done; {
				select {
				case serverNotification := <-notificationChannel:
					notifications = append(notifications, serverNotification)
					if len(notifications) == tt.expectedNotifications {
						done = true
					}
				case <-time.After(1 * time.Second):
					done = true
				}
			}
			assert.Len(t, notifications, tt.expectedNotifications)
			toolsList := server.HandleMessage(ctx, []byte(`{
				"jsonrpc": "2.0",
				"id": 1,
				"method": "tools/list"
			}`))
			tt.validate(t, notifications, toolsList.(mcp.JSONRPCMessage))
		})
	}
}

func TestMCPServer_HandleValidMessages(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0",
		WithResourceCapabilities(true, true),
		WithPromptCapabilities(true),
	)

	tests := []struct {
		name     string
		message  any
		validate func(t *testing.T, response mcp.JSONRPCMessage)
	}{
		{
			name: "Initialize request",
			message: mcp.JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Request: mcp.Request{
					Method: "initialize",
				},
			},
			validate: func(t *testing.T, response mcp.JSONRPCMessage) {
				resp, ok := response.(mcp.JSONRPCResponse)
				assert.True(t, ok)

				initResult, ok := resp.Result.(mcp.InitializeResult)
				assert.True(t, ok)

				assert.Equal(
					t,
					mcp.LATEST_PROTOCOL_VERSION,
					initResult.ProtocolVersion,
				)
				assert.Equal(t, "test-server", initResult.ServerInfo.Name)
				assert.Equal(t, "1.0.0", initResult.ServerInfo.Version)
			},
		},
		{
			name: "Ping request",
			message: mcp.JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Request: mcp.Request{
					Method: "ping",
				},
			},
			validate: func(t *testing.T, response mcp.JSONRPCMessage) {
				resp, ok := response.(mcp.JSONRPCResponse)
				assert.True(t, ok)

				_, ok = resp.Result.(mcp.EmptyResult)
				assert.True(t, ok)
			},
		},
		{
			name: "List resources",
			message: mcp.JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Request: mcp.Request{
					Method: "resources/list",
				},
			},
			validate: func(t *testing.T, response mcp.JSONRPCMessage) {
				resp, ok := response.(mcp.JSONRPCResponse)
				assert.True(t, ok)

				listResult, ok := resp.Result.(mcp.ListResourcesResult)
				assert.True(t, ok)
				assert.NotNil(t, listResult.Resources)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messageBytes, err := json.Marshal(tt.message)
			assert.NoError(t, err)

			response := server.HandleMessage(context.Background(), messageBytes)
			assert.NotNil(t, response)
			tt.validate(t, response)
		})
	}
}

func TestMCPServer_HandlePagination(t *testing.T) {
	server := createTestServer()
	cursor := base64.StdEncoding.EncodeToString([]byte("My Resource"))
	tests := []struct {
		name     string
		message  string
		validate func(t *testing.T, response mcp.JSONRPCMessage)
	}{
		{
			name: "List resources with cursor",
			message: fmt.Sprintf(`{
                    "jsonrpc": "2.0",
                    "id": 1,
                    "method": "resources/list",
                    "params": {
                        "cursor": "%s"
                    }
                }`, cursor),
			validate: func(t *testing.T, response mcp.JSONRPCMessage) {
				resp, ok := response.(mcp.JSONRPCResponse)
				assert.True(t, ok)

				listResult, ok := resp.Result.(mcp.ListResourcesResult)
				assert.True(t, ok)
				assert.NotNil(t, listResult.Resources)
				assert.Equal(t, mcp.Cursor(""), listResult.NextCursor)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := server.HandleMessage(
				context.Background(),
				[]byte(tt.message),
			)
			tt.validate(t, response)
		})
	}
}

func TestMCPServer_HandleNotifications(t *testing.T) {
	server := createTestServer()
	notificationReceived := false

	server.AddNotificationHandler("notifications/initialized", func(ctx context.Context, notification mcp.JSONRPCNotification) {
		notificationReceived = true
	})

	message := `{
            "jsonrpc": "2.0",
            "method": "notifications/initialized"
        }`

	response := server.HandleMessage(context.Background(), []byte(message))
	assert.Nil(t, response)
	assert.True(t, notificationReceived)
}

func TestMCPServer_SendNotificationToClient(t *testing.T) {
	tests := []struct {
		name           string
		contextPrepare func(context.Context, *MCPServer) context.Context
		validate       func(*testing.T, context.Context, *MCPServer)
	}{
		{
			name: "no active session",
			contextPrepare: func(ctx context.Context, srv *MCPServer) context.Context {
				return ctx
			},
			validate: func(t *testing.T, ctx context.Context, srv *MCPServer) {
				require.Error(t, srv.SendNotificationToClient(ctx, "method", nil))
			},
		},
		{
			name: "uninit session",
			contextPrepare: func(ctx context.Context, srv *MCPServer) context.Context {
				return srv.WithContext(ctx, fakeSession{
					sessionID:           "test",
					notificationChannel: make(chan mcp.JSONRPCNotification, 10),
					initialized:         false,
				})
			},
			validate: func(t *testing.T, ctx context.Context, srv *MCPServer) {
				require.Error(t, srv.SendNotificationToClient(ctx, "method", nil))
				_, ok := ClientSessionFromContext(ctx).(fakeSession)
				require.True(t, ok, "session not found or of incorrect type")
			},
		},
		{
			name: "active session",
			contextPrepare: func(ctx context.Context, srv *MCPServer) context.Context {
				return srv.WithContext(ctx, fakeSession{
					sessionID:           "test",
					notificationChannel: make(chan mcp.JSONRPCNotification, 10),
					initialized:         true,
				})
			},
			validate: func(t *testing.T, ctx context.Context, srv *MCPServer) {
				for range 10 {
					require.NoError(t, srv.SendNotificationToClient(ctx, "method", nil))
				}
				session, ok := ClientSessionFromContext(ctx).(fakeSession)
				require.True(t, ok, "session not found or of incorrect type")
				for range 10 {
					select {
					case record := <-session.notificationChannel:
						assert.Equal(t, "method", record.Method)
					default:
						t.Errorf("notification not sent")
					}
				}
			},
		},
		{
			name: "session with blocked channel",
			contextPrepare: func(ctx context.Context, srv *MCPServer) context.Context {
				return srv.WithContext(ctx, fakeSession{
					sessionID:           "test",
					notificationChannel: make(chan mcp.JSONRPCNotification, 1),
					initialized:         true,
				})
			},
			validate: func(t *testing.T, ctx context.Context, srv *MCPServer) {
				require.NoError(t, srv.SendNotificationToClient(ctx, "method", nil))
				require.Error(t, srv.SendNotificationToClient(ctx, "method", nil))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewMCPServer("test-server", "1.0.0")
			ctx := tt.contextPrepare(context.Background(), server)
			_ = server.HandleMessage(ctx, []byte(`{
				"jsonrpc": "2.0",
				"id": 1,
				"method": "initialize"
			}`))

			tt.validate(t, ctx, server)
		})
	}
}

func TestMCPServer_SendNotificationToAllClients(t *testing.T) {

	contextPrepare := func(ctx context.Context, srv *MCPServer) context.Context {
		// Create 5 active sessions
		for i := 0; i < 5; i++ {
			err := srv.RegisterSession(ctx, &fakeSession{
				sessionID:           fmt.Sprintf("test%d", i),
				notificationChannel: make(chan mcp.JSONRPCNotification, 10),
				initialized:         true,
			})
			require.NoError(t, err)
		}
		return ctx
	}

	validate := func(t *testing.T, ctx context.Context, srv *MCPServer) {
		// Send 10 notifications to all sessions
		for i := 0; i < 10; i++ {
			srv.SendNotificationToAllClients("method", map[string]any{
				"count": i,
			})
		}

		// Verify each session received all 10 notifications
		srv.sessions.Range(func(k, v any) bool {
			session := v.(ClientSession)
			fakeSess := session.(*fakeSession)
			notificationCount := 0

			// Read all notifications from the channel
			for notificationCount < 10 {
				select {
				case notification := <-fakeSess.notificationChannel:
					// Verify notification method
					assert.Equal(t, "method", notification.Method)
					// Verify count parameter
					count, ok := notification.Params.AdditionalFields["count"]
					assert.True(t, ok, "count parameter not found")
					assert.Equal(t, notificationCount, count.(int), "count should match notification count")
					notificationCount++
				case <-time.After(100 * time.Millisecond):
					t.Errorf("timeout waiting for notification %d for session %s", notificationCount, session.SessionID())
					return false
				}
			}

			// Verify no more notifications
			select {
			case notification := <-fakeSess.notificationChannel:
				t.Errorf("unexpected notification received: %v", notification)
			default:
				// Channel empty as expected
			}
			return true
		})
	}

	t.Run("all sessions", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0")
		ctx := contextPrepare(context.Background(), server)
		_ = server.HandleMessage(ctx, []byte(`{
				"jsonrpc": "2.0",
				"id": 1,
				"method": "initialize"
			}`))
		validate(t, ctx, server)
	})
}

func TestMCPServer_PromptHandling(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0",
		WithPromptCapabilities(true),
	)

	// Add a test prompt
	testPrompt := mcp.Prompt{
		Name:        "test-prompt",
		Description: "A test prompt",
		Arguments: []mcp.PromptArgument{
			{
				Name:        "arg1",
				Description: "First argument",
			},
		},
	}

	server.AddPrompt(
		testPrompt,
		func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return &mcp.GetPromptResult{
				Messages: []mcp.PromptMessage{
					{
						Role: mcp.RoleAssistant,
						Content: mcp.TextContent{
							Type: "text",
							Text: "Test prompt with arg1: " + request.Params.Arguments["arg1"],
						},
					},
				},
			}, nil
		},
	)

	tests := []struct {
		name     string
		message  string
		validate func(t *testing.T, response mcp.JSONRPCMessage)
	}{
		{
			name: "List prompts",
			message: `{
                "jsonrpc": "2.0",
                "id": 1,
                "method": "prompts/list"
            }`,
			validate: func(t *testing.T, response mcp.JSONRPCMessage) {
				resp, ok := response.(mcp.JSONRPCResponse)
				assert.True(t, ok)

				result, ok := resp.Result.(mcp.ListPromptsResult)
				assert.True(t, ok)
				assert.Len(t, result.Prompts, 1)
				assert.Equal(t, "test-prompt", result.Prompts[0].Name)
				assert.Equal(t, "A test prompt", result.Prompts[0].Description)
			},
		},
		{
			name: "Get prompt",
			message: `{
                "jsonrpc": "2.0",
                "id": 1,
                "method": "prompts/get",
                "params": {
                    "name": "test-prompt",
                    "arguments": {
                        "arg1": "test-value"
                    }
                }
            }`,
			validate: func(t *testing.T, response mcp.JSONRPCMessage) {
				resp, ok := response.(mcp.JSONRPCResponse)
				assert.True(t, ok)

				result, ok := resp.Result.(mcp.GetPromptResult)
				assert.True(t, ok)
				assert.Len(t, result.Messages, 1)
				textContent, ok := result.Messages[0].Content.(mcp.TextContent)
				assert.True(t, ok)
				assert.Equal(
					t,
					"Test prompt with arg1: test-value",
					textContent.Text,
				)
			},
		},
		{
			name: "Get prompt with missing argument",
			message: `{
                "jsonrpc": "2.0",
                "id": 1,
                "method": "prompts/get",
                "params": {
                    "name": "test-prompt",
                    "arguments": {}
                }
            }`,
			validate: func(t *testing.T, response mcp.JSONRPCMessage) {
				resp, ok := response.(mcp.JSONRPCResponse)
				assert.True(t, ok)

				result, ok := resp.Result.(mcp.GetPromptResult)
				assert.True(t, ok)
				assert.Len(t, result.Messages, 1)
				textContent, ok := result.Messages[0].Content.(mcp.TextContent)
				assert.True(t, ok)
				assert.Equal(t, "Test prompt with arg1: ", textContent.Text)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := server.HandleMessage(
				context.Background(),
				[]byte(tt.message),
			)
			tt.validate(t, response)
		})
	}
}

func TestMCPServer_HandleInvalidMessages(t *testing.T) {
	var errs []error
	hooks := &Hooks{}
	hooks.AddOnError(func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
		errs = append(errs, err)
	})

	server := NewMCPServer("test-server", "1.0.0", WithHooks(hooks))

	tests := []struct {
		name        string
		message     string
		expectedErr int
		validateErr func(t *testing.T, err error)
	}{
		{
			name:        "Invalid JSON",
			message:     `{"jsonrpc": "2.0", "id": 1, "method": "initialize"`,
			expectedErr: mcp.PARSE_ERROR,
		},
		{
			name:        "Invalid method",
			message:     `{"jsonrpc": "2.0", "id": 1, "method": "nonexistent"}`,
			expectedErr: mcp.METHOD_NOT_FOUND,
		},
		{
			name:        "Invalid parameters",
			message:     `{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": "invalid"}`,
			expectedErr: mcp.INVALID_REQUEST,
			validateErr: func(t *testing.T, err error) {
				unparsableErr := &UnparsableMessageError{}
				ok := errors.As(err, &unparsableErr)
				assert.True(t, ok, "Error should be UnparsableMessageError")
				assert.Equal(t, mcp.MethodInitialize, unparsableErr.GetMethod())
				assert.Equal(t, json.RawMessage(`{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": "invalid"}`), unparsableErr.GetMessage())
			},
		},
		{
			name:        "Missing JSONRPC version",
			message:     `{"id": 1, "method": "initialize"}`,
			expectedErr: mcp.INVALID_REQUEST,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs = nil // Reset errors for each test case

			response := server.HandleMessage(
				context.Background(),
				[]byte(tt.message),
			)
			assert.NotNil(t, response)

			errorResponse, ok := response.(mcp.JSONRPCError)
			assert.True(t, ok)
			assert.Equal(t, tt.expectedErr, errorResponse.Error.Code)

			if tt.validateErr != nil {
				require.Len(t, errs, 1, "Expected exactly one error")
				tt.validateErr(t, errs[0])
			}
		})
	}
}

func TestMCPServer_HandleUndefinedHandlers(t *testing.T) {
	var errs []error
	type beforeResult struct {
		method  mcp.MCPMethod
		message any
	}
	type afterResult struct {
		method  mcp.MCPMethod
		message any
		result  any
	}
	var beforeResults []beforeResult
	var afterResults []afterResult
	hooks := &Hooks{}
	hooks.AddOnError(func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
		errs = append(errs, err)
	})
	hooks.AddBeforeAny(func(ctx context.Context, id any, method mcp.MCPMethod, message any) {
		beforeResults = append(beforeResults, beforeResult{method, message})
	})
	hooks.AddOnSuccess(func(ctx context.Context, id any, method mcp.MCPMethod, message any, result any) {
		afterResults = append(afterResults, afterResult{method, message, result})
	})

	server := NewMCPServer("test-server", "1.0.0",
		WithResourceCapabilities(true, true),
		WithPromptCapabilities(true),
		WithToolCapabilities(true),
		WithHooks(hooks),
	)

	// Add a test tool to enable tool capabilities
	server.AddTool(mcp.Tool{
		Name:        "test-tool",
		Description: "Test tool",
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: map[string]any{},
		},
		Annotations: mcp.ToolAnnotation{
			Title:           "test-tool",
			ReadOnlyHint:    mcp.ToBoolPtr(true),
			DestructiveHint: mcp.ToBoolPtr(false),
			IdempotentHint:  mcp.ToBoolPtr(false),
			OpenWorldHint:   mcp.ToBoolPtr(false),
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})

	tests := []struct {
		name              string
		message           string
		expectedErr       int
		validateCallbacks func(t *testing.T, err error, beforeResults beforeResult)
	}{
		{
			name: "Undefined tool",
			message: `{
                    "jsonrpc": "2.0",
                    "id": 1,
                    "method": "tools/call",
                    "params": {
                        "name": "undefined-tool",
                        "arguments": {}
                    }
                }`,
			expectedErr: mcp.INVALID_PARAMS,
			validateCallbacks: func(t *testing.T, err error, beforeResults beforeResult) {
				assert.Equal(t, mcp.MethodToolsCall, beforeResults.method)
				assert.True(t, errors.Is(err, ErrToolNotFound))
			},
		},
		{
			name: "Undefined prompt",
			message: `{
                    "jsonrpc": "2.0",
                    "id": 1,
                    "method": "prompts/get",
                    "params": {
                        "name": "undefined-prompt",
                        "arguments": {}
                    }
                }`,
			expectedErr: mcp.INVALID_PARAMS,
			validateCallbacks: func(t *testing.T, err error, beforeResults beforeResult) {
				assert.Equal(t, mcp.MethodPromptsGet, beforeResults.method)
				assert.True(t, errors.Is(err, ErrPromptNotFound))
			},
		},
		{
			name: "Undefined resource",
			message: `{
                    "jsonrpc": "2.0",
                    "id": 1,
                    "method": "resources/read",
                    "params": {
                        "uri": "undefined-resource"
                    }
                }`,
			expectedErr: mcp.RESOURCE_NOT_FOUND,
			validateCallbacks: func(t *testing.T, err error, beforeResults beforeResult) {
				assert.Equal(t, mcp.MethodResourcesRead, beforeResults.method)
				assert.True(t, errors.Is(err, ErrResourceNotFound))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs = nil // Reset errors for each test case
			beforeResults = nil
			response := server.HandleMessage(
				context.Background(),
				[]byte(tt.message),
			)
			assert.NotNil(t, response)

			errorResponse, ok := response.(mcp.JSONRPCError)
			assert.True(t, ok)
			assert.Equal(t, tt.expectedErr, errorResponse.Error.Code)

			if tt.validateCallbacks != nil {
				require.Len(t, errs, 1, "Expected exactly one error")
				require.Len(t, beforeResults, 1, "Expected exactly one before result")
				require.Len(t, afterResults, 0, "Expected no after results because these calls generate errors")
				tt.validateCallbacks(t, errs[0], beforeResults[0])
			}
		})
	}
}

func TestMCPServer_HandleMethodsWithoutCapabilities(t *testing.T) {
	var errs []error
	hooks := &Hooks{}
	hooks.AddOnError(func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
		errs = append(errs, err)
	})
	hooksOption := WithHooks(hooks)

	tests := []struct {
		name        string
		message     string
		options     []ServerOption
		expectedErr int
		errString   string
	}{
		{
			name: "Tools without capabilities",
			message: `{
                    "jsonrpc": "2.0",
                    "id": 1,
                    "method": "tools/call",
                    "params": {
                        "name": "test-tool"
                    }
                }`,
			options:     []ServerOption{hooksOption}, // No capabilities at all
			expectedErr: mcp.METHOD_NOT_FOUND,
			errString:   "tools",
		},
		{
			name: "Prompts without capabilities",
			message: `{
                    "jsonrpc": "2.0",
                    "id": 1,
                    "method": "prompts/get",
                    "params": {
                        "name": "test-prompt"
                    }
                }`,
			options:     []ServerOption{hooksOption}, // No capabilities at all
			expectedErr: mcp.METHOD_NOT_FOUND,
			errString:   "prompts",
		},
		{
			name: "Resources without capabilities",
			message: `{
                    "jsonrpc": "2.0",
                    "id": 1,
                    "method": "resources/read",
                    "params": {
                        "uri": "test-resource"
                    }
                }`,
			options:     []ServerOption{hooksOption}, // No capabilities at all
			expectedErr: mcp.METHOD_NOT_FOUND,
			errString:   "resources",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs = nil // Reset errors for each test case

			server := NewMCPServer("test-server", "1.0.0", tt.options...)
			response := server.HandleMessage(
				context.Background(),
				[]byte(tt.message),
			)
			assert.NotNil(t, response)

			errorResponse, ok := response.(mcp.JSONRPCError)
			assert.True(t, ok)
			assert.Equal(t, tt.expectedErr, errorResponse.Error.Code)

			require.Len(t, errs, 1, "Expected exactly one error")
			assert.True(t, errors.Is(errs[0], ErrUnsupported), "Error should be ErrUnsupported but was %v", errs[0])
			assert.Contains(t, errs[0].Error(), tt.errString)
		})
	}
}

func TestMCPServer_Instructions(t *testing.T) {
	tests := []struct {
		name         string
		instructions string
		validate     func(t *testing.T, response mcp.JSONRPCMessage)
	}{
		{
			name:         "No instructions",
			instructions: "",
			validate: func(t *testing.T, response mcp.JSONRPCMessage) {
				resp, ok := response.(mcp.JSONRPCResponse)
				assert.True(t, ok)

				initResult, ok := resp.Result.(mcp.InitializeResult)
				assert.True(t, ok)
				assert.Equal(t, "", initResult.Instructions)
			},
		},
		{
			name:         "With instructions",
			instructions: "These are test instructions for the client.",
			validate: func(t *testing.T, response mcp.JSONRPCMessage) {
				resp, ok := response.(mcp.JSONRPCResponse)
				assert.True(t, ok)

				initResult, ok := resp.Result.(mcp.InitializeResult)
				assert.True(t, ok)
				assert.Equal(t, "These are test instructions for the client.", initResult.Instructions)
			},
		},
		{
			name:         "With multiline instructions",
			instructions: "Line 1\nLine 2\nLine 3",
			validate: func(t *testing.T, response mcp.JSONRPCMessage) {
				resp, ok := response.(mcp.JSONRPCResponse)
				assert.True(t, ok)

				initResult, ok := resp.Result.(mcp.InitializeResult)
				assert.True(t, ok)
				assert.Equal(t, "Line 1\nLine 2\nLine 3", initResult.Instructions)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var server *MCPServer
			if tt.instructions == "" {
				server = NewMCPServer("test-server", "1.0.0")
			} else {
				server = NewMCPServer("test-server", "1.0.0", WithInstructions(tt.instructions))
			}

			message := mcp.JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Request: mcp.Request{
					Method: "initialize",
				},
			}
			messageBytes, err := json.Marshal(message)
			assert.NoError(t, err)

			response := server.HandleMessage(context.Background(), messageBytes)
			tt.validate(t, response)
		})
	}
}

func TestMCPServer_ResourceTemplates(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0",
		WithResourceCapabilities(true, true),
	)

	server.AddResourceTemplate(
		mcp.NewResourceTemplate(
			"test://{a}/test-resource{/b*}",
			"My Resource",
		),
		func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			a := request.Params.Arguments["a"].([]string)
			b := request.Params.Arguments["b"].([]string)
			// Validate that the template arguments are passed correctly to the handler
			assert.Equal(t, []string{"something"}, a)
			assert.Equal(t, []string{"a", "b", "c"}, b)
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      "test://something/test-resource/a/b/c",
					MIMEType: "text/plain",
					Text:     "test content: " + a[0],
				},
			}, nil
		},
	)

	listMessage := `{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "resources/templates/list"
	}`

	message := `{
		"jsonrpc": "2.0",
		"id": 2,
		"method": "resources/read",
		"params": {
			"uri": "test://something/test-resource/a/b/c"
		}
	}`

	t.Run("Get resource template", func(t *testing.T) {
		response := server.HandleMessage(
			context.Background(),
			[]byte(listMessage),
		)
		assert.NotNil(t, response)

		resp, ok := response.(mcp.JSONRPCResponse)
		assert.True(t, ok)
		listResult, ok := resp.Result.(mcp.ListResourceTemplatesResult)
		assert.True(t, ok)
		assert.Len(t, listResult.ResourceTemplates, 1)
		assert.Equal(t, "My Resource", listResult.ResourceTemplates[0].Name)
		template, err := json.Marshal(listResult.ResourceTemplates[0])
		assert.NoError(t, err)

		// Need to serialize the json to map[string]string to validate the URITemplate is correctly marshalled
		var resourceTemplate map[string]string
		err = json.Unmarshal(template, &resourceTemplate)
		assert.NoError(t, err)

		assert.Equal(t, "test://{a}/test-resource{/b*}", resourceTemplate["uriTemplate"])

		response = server.HandleMessage(
			context.Background(),
			[]byte(message),
		)

		assert.NotNil(t, response)

		resp, ok = response.(mcp.JSONRPCResponse)
		assert.True(t, ok)
		// Validate that the resource values are returned correctly
		result, ok := resp.Result.(mcp.ReadResourceResult)
		assert.True(t, ok)
		assert.Len(t, result.Contents, 1)
		resultContent, ok := result.Contents[0].(mcp.TextResourceContents)
		assert.True(t, ok)
		assert.Equal(t, "test://something/test-resource/a/b/c", resultContent.URI)
		assert.Equal(t, "text/plain", resultContent.MIMEType)
		assert.Equal(t, "test content: something", resultContent.Text)
	})
}

func createTestServer() *MCPServer {
	server := NewMCPServer("test-server", "1.0.0",
		WithResourceCapabilities(true, true),
		WithPromptCapabilities(true),
		WithPaginationLimit(2),
	)

	server.AddResource(
		mcp.Resource{
			URI:  "resource://testresource",
			Name: "My Resource",
		},
		func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      "resource://testresource",
					MIMEType: "text/plain",
					Text:     "test content",
				},
			}, nil
		},
	)

	server.AddTool(
		mcp.Tool{
			Name:        "test-tool",
			Description: "Test tool",
		},
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: "test result",
					},
				},
			}, nil
		},
	)

	return server
}

type fakeSession struct {
	sessionID           string
	notificationChannel chan mcp.JSONRPCNotification
	initialized         bool
}

func (f fakeSession) SessionID() string {
	return f.sessionID
}

func (f fakeSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return f.notificationChannel
}

func (f fakeSession) Initialize() {
}

func (f fakeSession) Initialized() bool {
	return f.initialized
}

var _ ClientSession = fakeSession{}

func TestMCPServer_WithHooks(t *testing.T) {
	// Create hook counters to verify calls
	var (
		beforeAnyCount               int
		onSuccessCount               int
		onErrorCount                 int
		beforePingCount              int
		afterPingCount               int
		beforeToolsCount             int
		afterToolsCount              int
		onRequestInitializationCount int
	)

	// Collectors for message and result types
	var beforeAnyMessages []any
	var onSuccessData []struct {
		msg any
		res any
	}
	var beforePingMessages []*mcp.PingRequest
	var afterPingData []struct {
		msg *mcp.PingRequest
		res *mcp.EmptyResult
	}

	// Initialize hook handlers
	hooks := &Hooks{}

	// Register "any" hooks with type verification
	hooks.AddBeforeAny(func(ctx context.Context, id any, method mcp.MCPMethod, message any) {
		beforeAnyCount++
		// Only collect ping messages for our test
		if method == mcp.MethodPing {
			beforeAnyMessages = append(beforeAnyMessages, message)
		}
	})

	hooks.AddOnSuccess(func(ctx context.Context, id any, method mcp.MCPMethod, message any, result any) {
		onSuccessCount++
		// Only collect ping responses for our test
		if method == mcp.MethodPing {
			onSuccessData = append(onSuccessData, struct {
				msg any
				res any
			}{message, result})
		}
	})

	hooks.AddOnError(func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
		onErrorCount++
	})

	// Register method-specific hooks with type verification
	hooks.AddBeforePing(func(ctx context.Context, id any, message *mcp.PingRequest) {
		beforePingCount++
		beforePingMessages = append(beforePingMessages, message)
	})

	hooks.AddAfterPing(func(ctx context.Context, id any, message *mcp.PingRequest, result *mcp.EmptyResult) {
		afterPingCount++
		afterPingData = append(afterPingData, struct {
			msg *mcp.PingRequest
			res *mcp.EmptyResult
		}{message, result})
	})

	hooks.AddBeforeListTools(func(ctx context.Context, id any, message *mcp.ListToolsRequest) {
		beforeToolsCount++
	})

	hooks.AddAfterListTools(func(ctx context.Context, id any, message *mcp.ListToolsRequest, result *mcp.ListToolsResult) {
		afterToolsCount++
	})

	hooks.AddOnRequestInitialization(func(ctx context.Context, id any, message any) error {
		onRequestInitializationCount++
		return nil
	})

	// Create a server with the hooks
	server := NewMCPServer(
		"test-server",
		"1.0.0",
		WithHooks(hooks),
		WithToolCapabilities(true),
	)

	// Add a test tool
	server.AddTool(
		mcp.NewTool("test-tool"),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		},
	)

	// Initialize the server
	_ = server.HandleMessage(context.Background(), []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "initialize"
	}`))

	// Test 1: Verify ping method hooks
	pingResponse := server.HandleMessage(context.Background(), []byte(`{
		"jsonrpc": "2.0",
		"id": 2,
		"method": "ping"
	}`))

	// Verify success response
	assert.IsType(t, mcp.JSONRPCResponse{}, pingResponse)

	// Test 2: Verify tools/list method hooks
	toolsListResponse := server.HandleMessage(context.Background(), []byte(`{
		"jsonrpc": "2.0",
		"id": 3,
		"method": "tools/list"
	}`))

	// Verify success response
	assert.IsType(t, mcp.JSONRPCResponse{}, toolsListResponse)

	// Test 3: Verify error hooks with invalid tool
	errorResponse := server.HandleMessage(context.Background(), []byte(`{
		"jsonrpc": "2.0",
		"id": 4,
		"method": "tools/call",
		"params": {
			"name": "non-existent-tool"
		}
	}`))

	// Verify error response
	assert.IsType(t, mcp.JSONRPCError{}, errorResponse)

	// Verify hook counts

	// Method-specific hooks should be called exactly once
	assert.Equal(t, 1, beforePingCount, "beforePing should be called once")
	assert.Equal(t, 1, afterPingCount, "afterPing should be called once")
	assert.Equal(t, 1, beforeToolsCount, "beforeListTools should be called once")
	assert.Equal(t, 1, afterToolsCount, "afterListTools should be called once")
	// General hooks should be called for all methods
	// beforeAny is called for all 4 methods (initialize, ping, tools/list, tools/call)
	assert.Equal(t, 4, beforeAnyCount, "beforeAny should be called for each method")
	// onRequestInitialization is called for all 4 methods (initialize, ping, tools/list, tools/call)
	assert.Equal(t, 4, onRequestInitializationCount, "onRequestInitializationCount should be called for each method")
	// onSuccess is called for all 3 success methods (initialize, ping, tools/list)
	assert.Equal(t, 3, onSuccessCount, "onSuccess should be called after all successful invocations")

	// Error hook should be called once for the failed tools/call
	assert.Equal(t, 1, onErrorCount, "onError should be called once")

	// Verify type matching between BeforeAny and BeforePing
	require.Len(t, beforePingMessages, 1, "Expected one BeforePing message")
	require.Len(t, beforeAnyMessages, 1, "Expected one BeforeAny Ping message")
	assert.IsType(t, beforePingMessages[0], beforeAnyMessages[0], "BeforeAny message should be same type as BeforePing message")

	// Verify type matching between OnSuccess and AfterPing
	require.Len(t, afterPingData, 1, "Expected one AfterPing message/result pair")
	require.Len(t, onSuccessData, 1, "Expected one OnSuccess Ping message/result pair")
	assert.IsType(t, afterPingData[0].msg, onSuccessData[0].msg, "OnSuccess message should be same type as AfterPing message")
	assert.IsType(t, afterPingData[0].res, onSuccessData[0].res, "OnSuccess result should be same type as AfterPing result")
}

func TestMCPServer_SessionHooks(t *testing.T) {
	var (
		registerCalled   bool
		unregisterCalled bool

		registeredContext   context.Context
		unregisteredContext context.Context

		registeredSession   ClientSession
		unregisteredSession ClientSession
	)

	hooks := &Hooks{}
	hooks.AddOnRegisterSession(func(ctx context.Context, session ClientSession) {
		registerCalled = true
		registeredContext = ctx
		registeredSession = session
	})
	hooks.AddOnUnregisterSession(func(ctx context.Context, session ClientSession) {
		unregisterCalled = true
		unregisteredContext = ctx
		unregisteredSession = session
	})

	server := NewMCPServer(
		"test-server",
		"1.0.0",
		WithHooks(hooks),
	)

	testSession := &fakeSession{
		sessionID:           "test-session-id",
		notificationChannel: make(chan mcp.JSONRPCNotification, 5),
		initialized:         false,
	}

	ctx := context.WithoutCancel(context.Background())
	err := server.RegisterSession(ctx, testSession)
	require.NoError(t, err)

	assert.True(t, registerCalled, "Register session hook was not called")
	assert.Equal(t, testSession.SessionID(), registeredSession.SessionID(),
		"Register hook received wrong session")

	server.UnregisterSession(ctx, testSession.SessionID())

	assert.True(t, unregisterCalled, "Unregister session hook was not called")
	assert.Equal(t, testSession.SessionID(), unregisteredSession.SessionID(),
		"Unregister hook received wrong session")

	assert.Equal(t, ctx, unregisteredContext, "Unregister hook received wrong context")
	assert.Equal(t, ctx, registeredContext, "Register hook received wrong context")
}

func TestMCPServer_SessionHooks_NilHooks(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0")

	testSession := &fakeSession{
		sessionID:           "test-session-id",
		notificationChannel: make(chan mcp.JSONRPCNotification, 5),
		initialized:         false,
	}

	ctx := context.WithoutCancel(context.Background())
	err := server.RegisterSession(ctx, testSession)
	require.NoError(t, err)

	server.UnregisterSession(ctx, testSession.SessionID())
}

func TestMCPServer_WithRecover(t *testing.T) {
	panicToolHandler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		panic("test panic")
	}

	server := NewMCPServer(
		"test-server",
		"1.0.0",
		WithRecovery(),
	)

	server.AddTool(
		mcp.NewTool("panic-tool"),
		panicToolHandler,
	)

	response := server.HandleMessage(context.Background(), []byte(`{
		"jsonrpc": "2.0",
		"id": 4,
		"method": "tools/call",
		"params": {
			"name": "panic-tool"
		}
	}`))

	errorResponse, ok := response.(mcp.JSONRPCError)

	require.True(t, ok)
	assert.Equal(t, mcp.INTERNAL_ERROR, errorResponse.Error.Code)
	assert.Equal(t, "panic recovered in panic-tool tool handler: test panic", errorResponse.Error.Message)
	assert.Nil(t, errorResponse.Error.Data)
}

func getTools(length int) []mcp.Tool {
	list := make([]mcp.Tool, 0, 10000)
	for i := 0; i < length; i++ {
		list = append(list, mcp.Tool{
			Name:        fmt.Sprintf("tool%d", i),
			Description: fmt.Sprintf("tool%d", i),
		})
	}
	return list
}

func listByPaginationForReflect[T any](
	ctx context.Context,
	s *MCPServer,
	cursor mcp.Cursor,
	allElements []T,
) ([]T, mcp.Cursor, error) {
	startPos := 0
	if cursor != "" {
		c, err := base64.StdEncoding.DecodeString(string(cursor))
		if err != nil {
			return nil, "", err
		}
		cString := string(c)
		startPos = sort.Search(len(allElements), func(i int) bool {
			return reflect.ValueOf(allElements[i]).FieldByName("Name").String() > cString
		})
	}
	endPos := len(allElements)
	if s.paginationLimit != nil {
		if len(allElements) > startPos+*s.paginationLimit {
			endPos = startPos + *s.paginationLimit
		}
	}
	elementsToReturn := allElements[startPos:endPos]
	// set the next cursor
	nextCursor := func() mcp.Cursor {
		if s.paginationLimit != nil && len(elementsToReturn) >= *s.paginationLimit {
			nc := reflect.ValueOf(elementsToReturn[len(elementsToReturn)-1]).FieldByName("Name").String()
			toString := base64.StdEncoding.EncodeToString([]byte(nc))
			return mcp.Cursor(toString)
		}
		return ""
	}()
	return elementsToReturn, nextCursor, nil
}

func BenchmarkMCPServer_Pagination(b *testing.B) {
	list := getTools(10000)
	ctx := context.Background()
	server := createTestServer()
	for i := 0; i < b.N; i++ {
		_, _, _ = listByPagination[mcp.Tool](ctx, server, "dG9vbDY1NA==", list)
	}
}

func BenchmarkMCPServer_PaginationForReflect(b *testing.B) {
	list := getTools(10000)
	ctx := context.Background()
	server := createTestServer()
	for i := 0; i < b.N; i++ {
		_, _, _ = listByPaginationForReflect[mcp.Tool](ctx, server, "dG9vbDY1NA==", list)
	}
}
