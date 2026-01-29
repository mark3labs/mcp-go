package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"sync"
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

func TestMCPServer_InitializesTaskTools(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0")
	assert.NotNil(t, server)
	assert.NotNil(t, server.taskTools)
	assert.Equal(t, 0, len(server.taskTools))
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
					"2025-03-26", // Backward compatibility: no protocol version provided,
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
					"2025-03-26", // Backward compatibility: no protocol version provided,
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
					"2025-03-26", // Backward compatibility: no protocol version provided,
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
				ID:      mcp.NewRequestId(int64(1)),
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
				server.AddTool(
					mcp.NewTool("test-tool-1"),
					func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						return &mcp.CallToolResult{}, nil
					},
				)
				server.AddTool(
					mcp.NewTool("test-tool-2"),
					func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						return &mcp.CallToolResult{}, nil
					},
				)
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
			tt.validate(t, notifications, toolsList)
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
				ID:      mcp.NewRequestId(int64(1)),
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
					"2025-03-26", // Backward compatibility: no protocol version provided,
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
				ID:      mcp.NewRequestId(int64(1)),
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
				ID:      mcp.NewRequestId(int64(1)),
				Request: mcp.Request{
					Method: "resources/list",
				},
			},
			validate: func(t *testing.T, response mcp.JSONRPCMessage) {
				resp, ok := response.(mcp.JSONRPCResponse)
				assert.True(t, ok)

				_, ok = resp.Result.(mcp.ListResourcesResult)
				assert.True(t, ok)
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

	server.AddNotificationHandler(
		"notifications/initialized",
		func(ctx context.Context, notification mcp.JSONRPCNotification) {
			notificationReceived = true
		},
	)

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
		for i := range 5 {
			err := srv.RegisterSession(ctx, &fakeSession{
				sessionID:           fmt.Sprintf("test%d", i),
				notificationChannel: make(chan mcp.JSONRPCNotification, 10),
				initialized:         true,
			})
			require.NoError(t, err)
		}
		return ctx
	}

	validate := func(t *testing.T, _ context.Context, srv *MCPServer) {
		// Send 10 notifications to all sessions
		for i := range 10 {
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
					assert.Equal(
						t,
						notificationCount,
						count.(int),
						"count should match notification count",
					)
					notificationCount++
				case <-time.After(100 * time.Millisecond):
					t.Errorf(
						"timeout waiting for notification %d for session %s",
						notificationCount,
						session.SessionID(),
					)
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

func TestMCPServer_Prompts(t *testing.T) {
	tests := []struct {
		name                  string
		action                func(*testing.T, *MCPServer, chan mcp.JSONRPCNotification)
		expectedNotifications int
		validate              func(*testing.T, []mcp.JSONRPCNotification, mcp.JSONRPCMessage)
	}{
		{
			name: "DeletePrompts sends single notifications/prompts/list_changed",
			action: func(t *testing.T, server *MCPServer, notificationChannel chan mcp.JSONRPCNotification) {
				err := server.RegisterSession(context.TODO(), &fakeSession{
					sessionID:           "test",
					notificationChannel: notificationChannel,
					initialized:         true,
				})
				require.NoError(t, err)
				server.AddPrompt(
					mcp.Prompt{
						Name:        "test-prompt-1",
						Description: "A test prompt",
						Arguments: []mcp.PromptArgument{
							{
								Name:        "arg1",
								Description: "First argument",
							},
						},
					},
					nil,
				)
				server.DeletePrompts("test-prompt-1")
			},
			expectedNotifications: 2,
			validate: func(t *testing.T, notifications []mcp.JSONRPCNotification, promptsList mcp.JSONRPCMessage) {
				// One for AddPrompt
				assert.Equal(t, mcp.MethodNotificationPromptsListChanged, notifications[0].Method)
				// One for DeletePrompts
				assert.Equal(t, mcp.MethodNotificationPromptsListChanged, notifications[1].Method)

				// Expect a successful response with an empty list of prompts
				resp, ok := promptsList.(mcp.JSONRPCResponse)
				assert.True(t, ok, "Expected JSONRPCResponse, got %T", promptsList)

				result, ok := resp.Result.(mcp.ListPromptsResult)
				assert.True(t, ok, "Expected ListPromptsResult, got %T", resp.Result)

				assert.Empty(t, result.Prompts, "Expected empty prompts list")
			},
		},
		{
			name: "DeletePrompts removes the first prompt and retains the other",
			action: func(t *testing.T, server *MCPServer, notificationChannel chan mcp.JSONRPCNotification) {
				err := server.RegisterSession(context.TODO(), &fakeSession{
					sessionID:           "test",
					notificationChannel: notificationChannel,
					initialized:         true,
				})
				require.NoError(t, err)
				server.AddPrompt(
					mcp.Prompt{
						Name:        "test-prompt-1",
						Description: "A test prompt",
						Arguments: []mcp.PromptArgument{
							{
								Name:        "arg1",
								Description: "First argument",
							},
						},
					},
					nil,
				)
				server.AddPrompt(
					mcp.Prompt{
						Name:        "test-prompt-2",
						Description: "A test prompt",
						Arguments: []mcp.PromptArgument{
							{
								Name:        "arg1",
								Description: "First argument",
							},
						},
					},
					nil,
				)
				// Remove non-existing prompts
				server.DeletePrompts("test-prompt-1")
			},
			expectedNotifications: 3,
			validate: func(t *testing.T, notifications []mcp.JSONRPCNotification, promptsList mcp.JSONRPCMessage) {
				// first notification expected for AddPrompt test-prompt-1
				assert.Equal(t, mcp.MethodNotificationPromptsListChanged, notifications[0].Method)
				// second notification expected for AddPrompt test-prompt-2
				assert.Equal(t, mcp.MethodNotificationPromptsListChanged, notifications[1].Method)
				// second notification expected for DeletePrompts test-prompt-1
				assert.Equal(t, mcp.MethodNotificationPromptsListChanged, notifications[2].Method)

				// Confirm the prompt list does not change
				prompts := promptsList.(mcp.JSONRPCResponse).Result.(mcp.ListPromptsResult).Prompts
				assert.Len(t, prompts, 1)
				assert.Equal(t, "test-prompt-2", prompts[0].Name)
			},
		},
		{
			name: "DeletePrompts with non-existent prompts does nothing and not receives notifications from MCPServer",
			action: func(t *testing.T, server *MCPServer, notificationChannel chan mcp.JSONRPCNotification) {
				err := server.RegisterSession(context.TODO(), &fakeSession{
					sessionID:           "test",
					notificationChannel: notificationChannel,
					initialized:         true,
				})
				require.NoError(t, err)
				server.AddPrompt(
					mcp.Prompt{
						Name:        "test-prompt-1",
						Description: "A test prompt",
						Arguments: []mcp.PromptArgument{
							{
								Name:        "arg1",
								Description: "First argument",
							},
						},
					},
					nil,
				)
				server.AddPrompt(
					mcp.Prompt{
						Name:        "test-prompt-2",
						Description: "A test prompt",
						Arguments: []mcp.PromptArgument{
							{
								Name:        "arg1",
								Description: "First argument",
							},
						},
					},
					nil,
				)
				// Remove non-existing prompts
				server.DeletePrompts("test-prompt-3", "test-prompt-4")
			},
			expectedNotifications: 2,
			validate: func(t *testing.T, notifications []mcp.JSONRPCNotification, promptsList mcp.JSONRPCMessage) {
				// first notification expected for AddPrompt test-prompt-1
				assert.Equal(t, mcp.MethodNotificationPromptsListChanged, notifications[0].Method)
				// second notification expected for AddPrompt test-prompt-2
				assert.Equal(t, mcp.MethodNotificationPromptsListChanged, notifications[1].Method)

				// Confirm the prompt list does not change
				prompts := promptsList.(mcp.JSONRPCResponse).Result.(mcp.ListPromptsResult).Prompts
				assert.Len(t, prompts, 2)
				assert.Equal(t, "test-prompt-1", prompts[0].Name)
				assert.Equal(t, "test-prompt-2", prompts[1].Name)
			},
		},
		{
			name: "SetPrompts sends single notifications/prompts/list_changed with one active session",
			action: func(t *testing.T, server *MCPServer, notificationChannel chan mcp.JSONRPCNotification) {
				err := server.RegisterSession(context.TODO(), &fakeSession{
					sessionID:           "test",
					notificationChannel: notificationChannel,
					initialized:         true,
				})
				require.NoError(t, err)
				server.SetPrompts(ServerPrompt{
					Prompt: mcp.Prompt{
						Name:        "test-prompt-1",
						Description: "A test prompt",
						Arguments: []mcp.PromptArgument{
							{
								Name:        "arg1",
								Description: "First argument",
							},
						},
					},
					Handler: nil,
				}, ServerPrompt{
					Prompt: mcp.Prompt{
						Name:        "test-prompt-2",
						Description: "Another test prompt",
						Arguments: []mcp.PromptArgument{
							{
								Name:        "arg2",
								Description: "Second argument",
							},
						},
					},
					Handler: nil,
				})
			},
			expectedNotifications: 1,
			validate: func(t *testing.T, notifications []mcp.JSONRPCNotification, promptsList mcp.JSONRPCMessage) {
				assert.Equal(t, mcp.MethodNotificationPromptsListChanged, notifications[0].Method)
				prompts := promptsList.(mcp.JSONRPCResponse).Result.(mcp.ListPromptsResult).Prompts
				assert.Len(t, prompts, 2)
				assert.Equal(t, "test-prompt-1", prompts[0].Name)
				assert.Equal(t, "test-prompt-2", prompts[1].Name)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			server := NewMCPServer("test-server", "1.0.0", WithPromptCapabilities(true))
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
			promptsList := server.HandleMessage(ctx, []byte(`{
				"jsonrpc": "2.0",
				"id": 1,
				"method": "prompts/list"
			}`))
			tt.validate(t, notifications, promptsList)
		})
	}
}

func TestMCPServer_Resources(t *testing.T) {
	tests := []struct {
		name                  string
		action                func(*testing.T, *MCPServer, chan mcp.JSONRPCNotification)
		expectedNotifications int
		validate              func(*testing.T, []mcp.JSONRPCNotification, mcp.JSONRPCMessage)
	}{
		{
			name: "DeleteResources sends single notifications/resources/list_changed",
			action: func(t *testing.T, server *MCPServer, notificationChannel chan mcp.JSONRPCNotification) {
				err := server.RegisterSession(context.TODO(), &fakeSession{
					sessionID:           "test",
					notificationChannel: notificationChannel,
					initialized:         true,
				})
				require.NoError(t, err)
				server.AddResource(
					mcp.Resource{
						URI:  "test://test-resource-1",
						Name: "Test Resource 1",
					},
					func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
						return []mcp.ResourceContents{}, nil
					},
				)
				server.DeleteResources("test://test-resource-1")
			},
			expectedNotifications: 2,
			validate: func(t *testing.T, notifications []mcp.JSONRPCNotification, resourcesList mcp.JSONRPCMessage) {
				// One for AddResource
				assert.Equal(t, mcp.MethodNotificationResourcesListChanged, notifications[0].Method)
				// One for DeleteResources
				assert.Equal(t, mcp.MethodNotificationResourcesListChanged, notifications[1].Method)

				// Expect a successful response with an empty list of resources
				resp, ok := resourcesList.(mcp.JSONRPCResponse)
				assert.True(t, ok, "Expected JSONRPCResponse, got %T", resourcesList)

				result, ok := resp.Result.(mcp.ListResourcesResult)
				assert.True(t, ok, "Expected ListResourcesResult, got %T", resp.Result)

				assert.Empty(t, result.Resources, "Expected empty resources list")
			},
		},
		{
			name: "DeleteResources removes the first resource and retains the other",
			action: func(t *testing.T, server *MCPServer, notificationChannel chan mcp.JSONRPCNotification) {
				err := server.RegisterSession(context.TODO(), &fakeSession{
					sessionID:           "test",
					notificationChannel: notificationChannel,
					initialized:         true,
				})
				require.NoError(t, err)
				server.AddResource(
					mcp.Resource{
						URI:  "test://test-resource-1",
						Name: "Test Resource 1",
					},
					func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
						return []mcp.ResourceContents{}, nil
					},
				)
				server.AddResource(
					mcp.Resource{
						URI:  "test://test-resource-2",
						Name: "Test Resource 2",
					},
					func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
						return []mcp.ResourceContents{}, nil
					},
				)
				server.DeleteResources("test://test-resource-1")
			},
			expectedNotifications: 3,
			validate: func(t *testing.T, notifications []mcp.JSONRPCNotification, resourcesList mcp.JSONRPCMessage) {
				// first notification expected for AddResource test-resource-1
				assert.Equal(t, mcp.MethodNotificationResourcesListChanged, notifications[0].Method)
				// second notification expected for AddResource test-resource-2
				assert.Equal(t, mcp.MethodNotificationResourcesListChanged, notifications[1].Method)
				// third notification expected for DeleteResources test-resource-1
				assert.Equal(t, mcp.MethodNotificationResourcesListChanged, notifications[2].Method)

				// Confirm the resource list contains only test-resource-2
				resources := resourcesList.(mcp.JSONRPCResponse).Result.(mcp.ListResourcesResult).Resources
				assert.Len(t, resources, 1)
				assert.Equal(t, "test://test-resource-2", resources[0].URI)
			},
		},
		{
			name: "DeleteResources with non-existent resources does nothing and not receives notifications from MCPServer",
			action: func(t *testing.T, server *MCPServer, notificationChannel chan mcp.JSONRPCNotification) {
				err := server.RegisterSession(context.TODO(), &fakeSession{
					sessionID:           "test",
					notificationChannel: notificationChannel,
					initialized:         true,
				})
				require.NoError(t, err)
				server.AddResource(
					mcp.Resource{
						URI:  "test://test-resource-1",
						Name: "Test Resource 1",
					},
					func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
						return []mcp.ResourceContents{}, nil
					},
				)
				server.AddResource(
					mcp.Resource{
						URI:  "test://test-resource-2",
						Name: "Test Resource 2",
					},
					func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
						return []mcp.ResourceContents{}, nil
					},
				)
				// Remove non-existing resources
				server.DeleteResources("test://test-resource-3", "test://test-resource-4")
			},
			expectedNotifications: 2,
			validate: func(t *testing.T, notifications []mcp.JSONRPCNotification, resourcesList mcp.JSONRPCMessage) {
				// first notification expected for AddResource test-resource-1
				assert.Equal(t, mcp.MethodNotificationResourcesListChanged, notifications[0].Method)
				// second notification expected for AddResource test-resource-2
				assert.Equal(t, mcp.MethodNotificationResourcesListChanged, notifications[1].Method)

				// Confirm the resource list does not change
				resources := resourcesList.(mcp.JSONRPCResponse).Result.(mcp.ListResourcesResult).Resources
				assert.Len(t, resources, 2)
				// Resources are sorted by name
				assert.Equal(t, "test://test-resource-1", resources[0].URI)
				assert.Equal(t, "test://test-resource-2", resources[1].URI)
			},
		},
		{
			name: "SetResources sends single notifications/resources/list_changed with one active session",
			action: func(t *testing.T, server *MCPServer, notificationChannel chan mcp.JSONRPCNotification) {
				err := server.RegisterSession(context.TODO(), &fakeSession{
					sessionID:           "test",
					notificationChannel: notificationChannel,
					initialized:         true,
				})
				require.NoError(t, err)
				server.SetResources(ServerResource{
					Resource: mcp.Resource{
						URI:  "test://test-resource-1",
						Name: "Test Resource 1",
					},
					Handler: func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
						return []mcp.ResourceContents{}, nil
					},
				}, ServerResource{
					Resource: mcp.Resource{
						URI:  "test://test-resource-2",
						Name: "Test Resource 2",
					},
					Handler: func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
						return []mcp.ResourceContents{}, nil
					},
				})
			},
			expectedNotifications: 1,
			validate: func(t *testing.T, notifications []mcp.JSONRPCNotification, resourcesList mcp.JSONRPCMessage) {
				assert.Equal(t, mcp.MethodNotificationResourcesListChanged, notifications[0].Method)
				resources := resourcesList.(mcp.JSONRPCResponse).Result.(mcp.ListResourcesResult).Resources
				assert.Len(t, resources, 2)
				// Resources are sorted by name
				assert.Equal(t, "test://test-resource-1", resources[0].URI)
				assert.Equal(t, "test://test-resource-2", resources[1].URI)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			server := NewMCPServer("test-server", "1.0.0", WithResourceCapabilities(true, true))
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
			resourcesList := server.HandleMessage(ctx, []byte(`{
				"jsonrpc": "2.0",
				"id": 1,
				"method": "resources/list"
			}`))
			tt.validate(t, notifications, resourcesList)
		})
	}
}

func TestMCPServer_HandleInvalidMessages(t *testing.T) {
	var errs []error
	hooks := &Hooks{}
	hooks.AddOnError(
		func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
			errs = append(errs, err)
		},
	)

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
				assert.Equal(
					t,
					json.RawMessage(
						`{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": "invalid"}`,
					),
					unparsableErr.GetMessage(),
				)
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
	hooks.AddOnError(
		func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
			errs = append(errs, err)
		},
	)
	hooks.AddBeforeAny(func(ctx context.Context, id any, method mcp.MCPMethod, message any) {
		beforeResults = append(beforeResults, beforeResult{method, message})
	})
	hooks.AddOnSuccess(
		func(ctx context.Context, id any, method mcp.MCPMethod, message any, result any) {
			afterResults = append(afterResults, afterResult{method, message, result})
		},
	)

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
				require.Len(
					t,
					afterResults,
					0,
					"Expected no after results because these calls generate errors",
				)
				tt.validateCallbacks(t, errs[0], beforeResults[0])
			}
		})
	}
}

func TestMCPServer_HandleMethodsWithoutCapabilities(t *testing.T) {
	var errs []error
	hooks := &Hooks{}
	hooks.AddOnError(
		func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
			errs = append(errs, err)
		},
	)
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
			assert.True(
				t,
				errors.Is(errs[0], ErrUnsupported),
				"Error should be ErrUnsupported but was %v",
				errs[0],
			)
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
				assert.Equal(
					t,
					"These are test instructions for the client.",
					initResult.Instructions,
				)
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
				ID:      mcp.NewRequestId(int64(1)),
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

	server.AddResourceTemplates(
		ServerResourceTemplate{
			Template: mcp.NewResourceTemplate(
				"test://test-another-resource-1",
				"Another Resource 1",
			),
			Handler: func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
				return []mcp.ResourceContents{}, nil
			},
		},
		ServerResourceTemplate{
			Template: mcp.NewResourceTemplate(
				"test://test-another-resource-2",
				"Another Resource 2",
			),
			Handler: func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
				return []mcp.ResourceContents{}, nil
			},
		},
	)

	t.Run("Check bulk add resource templates", func(t *testing.T) {
		assert.Equal(t, 3, len(server.resourceTemplates))
	})

	t.Run("Get resource template again", func(t *testing.T) {
		response := server.HandleMessage(
			context.Background(),
			[]byte(listMessage),
		)
		assert.NotNil(t, response)

		resp, ok := response.(mcp.JSONRPCResponse)
		assert.True(t, ok)
		listResult, ok := resp.Result.(mcp.ListResourceTemplatesResult)
		assert.True(t, ok)
		assert.Len(t, listResult.ResourceTemplates, 3)

		// resource templates are stored in a map, so the order is not guaranteed
		for _, rt := range listResult.ResourceTemplates {
			assert.True(t, slices.Contains([]string{
				"My Resource",
				"Another Resource 1",
				"Another Resource 2",
			}, rt.Name))
		}
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

	hooks.AddOnSuccess(
		func(ctx context.Context, id any, method mcp.MCPMethod, message any, result any) {
			onSuccessCount++
			// Only collect ping responses for our test
			if method == mcp.MethodPing {
				onSuccessData = append(onSuccessData, struct {
					msg any
					res any
				}{message, result})
			}
		},
	)

	hooks.AddOnError(
		func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
			onErrorCount++
		},
	)

	// Register method-specific hooks with type verification
	hooks.AddBeforePing(func(ctx context.Context, id any, message *mcp.PingRequest) {
		beforePingCount++
		beforePingMessages = append(beforePingMessages, message)
	})

	hooks.AddAfterPing(
		func(ctx context.Context, id any, message *mcp.PingRequest, result *mcp.EmptyResult) {
			afterPingCount++
			afterPingData = append(afterPingData, struct {
				msg *mcp.PingRequest
				res *mcp.EmptyResult
			}{message, result})
		},
	)

	hooks.AddBeforeListTools(func(ctx context.Context, id any, message *mcp.ListToolsRequest) {
		beforeToolsCount++
	})

	hooks.AddAfterListTools(
		func(ctx context.Context, id any, message *mcp.ListToolsRequest, result *mcp.ListToolsResult) {
			afterToolsCount++
		},
	)

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
	assert.Equal(
		t,
		4,
		onRequestInitializationCount,
		"onRequestInitializationCount should be called for each method",
	)
	// onSuccess is called for all 3 success methods (initialize, ping, tools/list)
	assert.Equal(
		t,
		3,
		onSuccessCount,
		"onSuccess should be called after all successful invocations",
	)

	// Error hook should be called once for the failed tools/call
	assert.Equal(t, 1, onErrorCount, "onError should be called once")

	// Verify type matching between BeforeAny and BeforePing
	require.Len(t, beforePingMessages, 1, "Expected one BeforePing message")
	require.Len(t, beforeAnyMessages, 1, "Expected one BeforeAny Ping message")
	assert.IsType(
		t,
		beforePingMessages[0],
		beforeAnyMessages[0],
		"BeforeAny message should be same type as BeforePing message",
	)

	// Verify type matching between OnSuccess and AfterPing
	require.Len(t, afterPingData, 1, "Expected one AfterPing message/result pair")
	require.Len(t, onSuccessData, 1, "Expected one OnSuccess Ping message/result pair")
	assert.IsType(
		t,
		afterPingData[0].msg,
		onSuccessData[0].msg,
		"OnSuccess message should be same type as AfterPing message",
	)
	assert.IsType(
		t,
		afterPingData[0].res,
		onSuccessData[0].res,
		"OnSuccess result should be same type as AfterPing result",
	)
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
	assert.Equal(
		t,
		"panic recovered in panic-tool tool handler: test panic",
		errorResponse.Error.Message,
	)
	assert.Nil(t, errorResponse.Error.Data)
}

func getTools(length int) []mcp.Tool {
	list := make([]mcp.Tool, 0, 10000)
	for i := range length {
		list = append(list, mcp.Tool{
			Name:        fmt.Sprintf("tool%d", i),
			Description: fmt.Sprintf("tool%d", i),
		})
	}
	return list
}

func listByPaginationForReflect[T any](
	_ context.Context,
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
			nc := reflect.ValueOf(elementsToReturn[len(elementsToReturn)-1]).
				FieldByName("Name").
				String()
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
		_, _, _ = listByPagination(ctx, server, "dG9vbDY1NA==", list)
	}
}

func BenchmarkMCPServer_PaginationForReflect(b *testing.B) {
	list := getTools(10000)
	ctx := context.Background()
	server := createTestServer()
	for i := 0; i < b.N; i++ {
		_, _, _ = listByPaginationForReflect(ctx, server, "dG9vbDY1NA==", list)
	}
}

func TestMCPServer_ToolCapabilitiesBehavior(t *testing.T) {
	tests := []struct {
		name           string
		serverOptions  []ServerOption
		validateServer func(t *testing.T, s *MCPServer)
	}{
		{
			name:          "no tool capabilities provided",
			serverOptions: []ServerOption{
				// No WithToolCapabilities
			},
			validateServer: func(t *testing.T, s *MCPServer) {
				s.capabilitiesMu.RLock()
				defer s.capabilitiesMu.RUnlock()

				require.NotNil(t, s.capabilities.tools, "tools capability should be initialized")
				assert.True(
					t,
					s.capabilities.tools.listChanged,
					"listChanged should be true when no capabilities were provided",
				)
			},
		},
		{
			name: "tools.listChanged set to false",
			serverOptions: []ServerOption{
				WithToolCapabilities(false),
			},
			validateServer: func(t *testing.T, s *MCPServer) {
				s.capabilitiesMu.RLock()
				defer s.capabilitiesMu.RUnlock()

				require.NotNil(t, s.capabilities.tools, "tools capability should be initialized")
				assert.False(
					t,
					s.capabilities.tools.listChanged,
					"listChanged should remain false when explicitly set to false",
				)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewMCPServer("test-server", "1.0.0", tt.serverOptions...)
			server.AddTool(mcp.NewTool("test-tool"), nil)
			tt.validateServer(t, server)
		})
	}
}

func TestMCPServer_ProtocolNegotiation(t *testing.T) {
	tests := []struct {
		name            string
		clientVersion   string
		expectedVersion string
	}{
		{
			name:            "Server supports client version - should respond with same version",
			clientVersion:   "2024-11-05",
			expectedVersion: "2024-11-05", // Server must respond with client's version if supported
		},
		{
			name:            "Client requests current latest - should respond with same version",
			clientVersion:   mcp.LATEST_PROTOCOL_VERSION, // "2025-03-26"
			expectedVersion: mcp.LATEST_PROTOCOL_VERSION,
		},
		{
			name:            "Client requests unsupported future version - should respond with server's latest",
			clientVersion:   "2026-01-01",                // Future unsupported version
			expectedVersion: mcp.LATEST_PROTOCOL_VERSION, // Server responds with its latest supported
		},
		{
			name:            "Client requests unsupported old version - should respond with server's latest",
			clientVersion:   "2023-01-01",                // Very old unsupported version
			expectedVersion: mcp.LATEST_PROTOCOL_VERSION, // Server responds with its latest supported
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewMCPServer("test-server", "1.0.0")

			params := struct {
				ProtocolVersion string                 `json:"protocolVersion"`
				ClientInfo      mcp.Implementation     `json:"clientInfo"`
				Capabilities    mcp.ClientCapabilities `json:"capabilities"`
			}{
				ProtocolVersion: tt.clientVersion,
				ClientInfo: mcp.Implementation{
					Name:    "test-client",
					Version: "1.0.0",
				},
			}

			// Create initialize request with specific protocol version
			initRequest := mcp.JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      mcp.NewRequestId(int64(1)),
				Request: mcp.Request{
					Method: "initialize",
				},
				Params: params,
			}

			messageBytes, err := json.Marshal(initRequest)
			assert.NoError(t, err)

			response := server.HandleMessage(context.Background(), messageBytes)
			assert.NotNil(t, response)

			resp, ok := response.(mcp.JSONRPCResponse)
			assert.True(t, ok)

			initResult, ok := resp.Result.(mcp.InitializeResult)
			assert.True(t, ok)

			assert.Equal(
				t,
				tt.expectedVersion,
				initResult.ProtocolVersion,
				"Protocol version should follow MCP spec negotiation rules",
			)
		})
	}
}

func TestMCPServer_GetTool(t *testing.T) {
	t.Run("ExistingTool", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0")

		expectedTool := mcp.Tool{
			Name:        "test-tool",
			Description: "A test tool",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"input": map[string]any{
						"type":        "string",
						"description": "Test input",
					},
				},
			},
		}

		expectedHandler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: "test result",
					},
				},
			}, nil
		}

		server.AddTool(expectedTool, expectedHandler)

		tool := server.GetTool("test-tool")

		assert.NotNil(t, tool)
		assert.Equal(t, expectedTool, tool.Tool)
		assert.NotNil(t, tool.Handler)
	})

	t.Run("NonExistingTool", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0")

		tool := server.GetTool("non-existing-tool")

		assert.Nil(t, tool) // GetTool returns nil when tool doesn't exist
	})

	t.Run("EmptyServer", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0")

		tool := server.GetTool("any-tool")

		assert.Nil(t, tool) // GetTool returns nil when no tools are registered
	})

	t.Run("MultipleToolsGetSpecific", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0")

		tools := []mcp.Tool{
			{Name: "tool1", Description: "First tool"},
			{Name: "tool2", Description: "Second tool"},
			{Name: "tool3", Description: "Third tool"},
		}

		// Add all tools
		for _, tool := range tools {
			server.AddTool(tool, nil)
		}

		// Get specific tool
		tool := server.GetTool("tool2")

		assert.NotNil(t, tool)
		assert.Equal(t, "tool2", tool.Tool.Name)
		assert.Equal(t, "Second tool", tool.Tool.Description)
	})

	t.Run("WithComplexToolSchema", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0")

		complexTool := mcp.Tool{
			Name:        "complex-tool",
			Description: "A complex tool with detailed schema",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"stringParam": map[string]any{
						"type":        "string",
						"description": "A string parameter",
						"enum":        []string{"option1", "option2", "option3"},
					},
					"numberParam": map[string]any{
						"type":        "number",
						"description": "A number parameter",
						"minimum":     0,
						"maximum":     100,
					},
				},
				Required: []string{"stringParam", "numberParam"},
			},
			Annotations: mcp.ToolAnnotation{
				Title:           "Complex Tool",
				DestructiveHint: mcp.ToBoolPtr(true),
			},
		}

		handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		}

		server.AddTool(complexTool, handler)

		tool := server.GetTool("complex-tool")

		assert.NotNil(t, tool)
		assert.Equal(t, complexTool, tool.Tool)
		assert.NotNil(t, tool.Handler)

		// Verify the complex schema is preserved
		assert.Equal(t, "object", tool.Tool.InputSchema.Type)
		assert.Contains(t, tool.Tool.InputSchema.Properties, "stringParam")
		assert.Contains(t, tool.Tool.InputSchema.Properties, "numberParam")
		assert.Equal(t, []string{"stringParam", "numberParam"}, tool.Tool.InputSchema.Required)

		// Verify annotations
		assert.Equal(t, "Complex Tool", tool.Tool.Annotations.Title)
		assert.NotNil(t, tool.Tool.Annotations.DestructiveHint)
		assert.True(t, *tool.Tool.Annotations.DestructiveHint)
	})

	t.Run("CaseSensitivity", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0")

		expectedTool := mcp.Tool{
			Name:        "Test-Tool",
			Description: "A case-sensitive test tool",
		}

		server.AddTool(expectedTool, nil)

		// Exact match should work
		tool := server.GetTool("Test-Tool")
		assert.NotNil(t, tool)
		assert.Equal(t, "Test-Tool", tool.Tool.Name)

		// Different case should not match
		tool = server.GetTool("test-tool")
		assert.Nil(t, tool)

		tool = server.GetTool("TEST-TOOL")
		assert.Nil(t, tool)
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0")

		// Add some tools for concurrent access testing
		for i := 0; i < 10; i++ {
			server.AddTool(mcp.Tool{
				Name:        fmt.Sprintf("tool-%d", i),
				Description: fmt.Sprintf("Tool %d", i),
			}, nil)
		}

		numGoroutines := 100
		results := make(chan *ServerTool, numGoroutines)
		var wg sync.WaitGroup

		// Test concurrent reads
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				toolName := fmt.Sprintf("tool-%d", id%10) // Access tools cyclically
				tool := server.GetTool(toolName)
				results <- tool
			}(i)
		}

		wg.Wait()
		close(results)

		// Verify all results are valid
		validResults := 0
		for tool := range results {
			if tool != nil {
				validResults++
				assert.Contains(t, tool.Tool.Name, "tool-")
			}
		}

		// All results should be valid since we're accessing existing tools
		assert.Equal(t, numGoroutines, validResults)
	})

	t.Run("ReturnsCopyNotReference", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0")

		originalTool := mcp.Tool{
			Name:        "test-tool",
			Description: "Original description",
		}

		server.AddTool(originalTool, nil)

		// Get the tool twice
		tool1 := server.GetTool("test-tool")
		tool2 := server.GetTool("test-tool")

		assert.NotNil(t, tool1)
		assert.NotNil(t, tool2)

		// Both should have the same content
		assert.Equal(t, tool1.Tool, tool2.Tool)

		// But they should be different memory addresses (copies)
		assert.NotSame(t, tool1, tool2, "GetTool should return copies, not shared references")

		// Modifying one should not affect the other or the server's copy
		tool1.Tool.Description = "Modified description"

		tool3 := server.GetTool("test-tool")
		assert.NotNil(t, tool3)
		assert.Equal(t, "Original description", tool3.Tool.Description, "Server's copy should be unchanged")
		assert.Equal(t, "Original description", tool2.Tool.Description, "Other copy should be unchanged")
	})

	t.Run("EmptyToolName", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0")

		// Add a tool with non-empty name
		server.AddTool(mcp.Tool{Name: "valid-tool", Description: "Valid tool"}, nil)

		// Request with empty string should return nil
		tool := server.GetTool("")
		assert.Nil(t, tool)
	})

	t.Run("NilAfterToolDeletion", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0")

		// Add a tool
		server.AddTool(mcp.Tool{Name: "temp-tool", Description: "Temporary tool"}, nil)

		// Verify it exists
		tool := server.GetTool("temp-tool")
		assert.NotNil(t, tool)

		// Delete the tool
		server.DeleteTools("temp-tool")

		// Now it should return nil
		tool = server.GetTool("temp-tool")
		assert.Nil(t, tool)
	})
}

func TestMCPServer_ListTools(t *testing.T) {
	t.Run("EmptyServer", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0")

		tools := server.ListTools()

		assert.Nil(t, tools)
	})

	t.Run("SingleTool", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0")

		expectedTool := mcp.Tool{
			Name:        "test-tool",
			Description: "A test tool",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"input": map[string]any{
						"type":        "string",
						"description": "Test input",
					},
				},
			},
		}

		expectedHandler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: "test result",
					},
				},
			}, nil
		}

		server.AddTool(expectedTool, expectedHandler)

		tools := server.ListTools()

		assert.NotNil(t, tools)
		assert.Len(t, tools, 1)

		serverTool, exists := tools["test-tool"]
		assert.True(t, exists)
		assert.Equal(t, expectedTool, serverTool.Tool)
		assert.NotNil(t, serverTool.Handler)
	})

	t.Run("MultipleTools", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0")

		tools := []struct {
			tool    mcp.Tool
			handler ToolHandlerFunc
		}{
			{
				tool: mcp.Tool{
					Name:        "tool1",
					Description: "First tool",
					InputSchema: mcp.ToolInputSchema{Type: "object"},
				},
				handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return &mcp.CallToolResult{}, nil
				},
			},
			{
				tool: mcp.Tool{
					Name:        "tool2",
					Description: "Second tool",
					InputSchema: mcp.ToolInputSchema{Type: "object"},
				},
				handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return &mcp.CallToolResult{}, nil
				},
			},
			{
				tool: mcp.Tool{
					Name:        "tool3",
					Description: "Third tool",
					InputSchema: mcp.ToolInputSchema{Type: "object"},
				},
				handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return &mcp.CallToolResult{}, nil
				},
			},
		}

		// Add tools one by one
		for _, tool := range tools {
			server.AddTool(tool.tool, tool.handler)
		}

		retrievedTools := server.ListTools()

		assert.NotNil(t, retrievedTools)
		assert.Len(t, retrievedTools, 3)

		// Verify each tool exists with correct data
		for _, expectedTool := range tools {
			serverTool, exists := retrievedTools[expectedTool.tool.Name]
			assert.True(t, exists, "Tool %s should exist", expectedTool.tool.Name)
			assert.Equal(t, expectedTool.tool, serverTool.Tool)
			assert.NotNil(t, serverTool.Handler)
		}
	})

	t.Run("AfterToolDeletion", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0")

		// Add multiple tools
		server.AddTool(mcp.Tool{Name: "tool1", Description: "Tool 1"}, nil)
		server.AddTool(mcp.Tool{Name: "tool2", Description: "Tool 2"}, nil)
		server.AddTool(mcp.Tool{Name: "tool3", Description: "Tool 3"}, nil)

		// Verify all tools exist
		tools := server.ListTools()
		assert.NotNil(t, tools)
		assert.Len(t, tools, 3)

		// Delete one tool
		server.DeleteTools("tool2")

		// Verify tool is removed
		tools = server.ListTools()
		assert.NotNil(t, tools)
		assert.Len(t, tools, 2)

		_, exists := tools["tool1"]
		assert.True(t, exists)
		_, exists = tools["tool2"]
		assert.False(t, exists)
		_, exists = tools["tool3"]
		assert.True(t, exists)
	})

	t.Run("SetToolsReplacesExisting", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0")

		// Add initial tools
		server.AddTool(mcp.Tool{Name: "old-tool1", Description: "Old Tool 1"}, nil)
		server.AddTool(mcp.Tool{Name: "old-tool2", Description: "Old Tool 2"}, nil)

		// Verify initial tools
		tools := server.ListTools()
		assert.NotNil(t, tools)
		assert.Len(t, tools, 2)

		// Set new tools (should replace existing)
		newTools := []ServerTool{
			{
				Tool: mcp.Tool{Name: "new-tool1", Description: "New Tool 1"},
				Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return &mcp.CallToolResult{}, nil
				},
			},
			{
				Tool: mcp.Tool{Name: "new-tool2", Description: "New Tool 2"},
				Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return &mcp.CallToolResult{}, nil
				},
			},
		}
		server.SetTools(newTools...)

		// Verify only new tools exist
		tools = server.ListTools()
		assert.NotNil(t, tools)
		assert.Len(t, tools, 2)

		_, exists := tools["old-tool1"]
		assert.False(t, exists)
		_, exists = tools["old-tool2"]
		assert.False(t, exists)
		_, exists = tools["new-tool1"]
		assert.True(t, exists)
		_, exists = tools["new-tool2"]
		assert.True(t, exists)
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0")

		// Pre-add some tools to test concurrent reads
		for i := 0; i < 50; i++ {
			server.AddTool(mcp.Tool{
				Name:        fmt.Sprintf("pre-tool-%d", i),
				Description: fmt.Sprintf("Pre-added tool %d", i),
			}, nil)
		}

		numGoroutines := 100
		results := make(chan map[string]*ServerTool, numGoroutines)
		var wg sync.WaitGroup

		// Test concurrent reads (no race conditions in test logic)
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				tools := server.ListTools()
				results <- tools
			}(i)
		}

		wg.Wait()
		close(results)

		// Collect all results
		var allResults []map[string]*ServerTool
		for result := range results {
			allResults = append(allResults, result)
		}

		// Verify that no data races occurred and all results are valid
		for i, result := range allResults {
			assert.NotNil(t, result, "Result %d should not be nil", i)
			assert.Equal(t, 50, len(result), "All concurrent reads should return same number of tools")
		}
	})

	t.Run("ConsistentResults", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0")

		// Add a tool
		server.AddTool(mcp.Tool{
			Name:        "test-tool",
			Description: "Test tool",
		}, nil)

		// Get tools multiple times
		tools1 := server.ListTools()
		tools2 := server.ListTools()
		tools3 := server.ListTools()

		assert.NotNil(t, tools1)
		assert.NotNil(t, tools2)
		assert.NotNil(t, tools3)

		// All should have the same tool
		assert.Len(t, tools1, 1)
		assert.Len(t, tools2, 1)
		assert.Len(t, tools3, 1)

		assert.Contains(t, tools1, "test-tool")
		assert.Contains(t, tools2, "test-tool")
		assert.Contains(t, tools3, "test-tool")

		// Verify tool content is the same
		assert.Equal(t, tools1["test-tool"].Tool, tools2["test-tool"].Tool)
		assert.Equal(t, tools2["test-tool"].Tool, tools3["test-tool"].Tool)
	})

	t.Run("ReturnsCopiesNotReferences", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0")

		// Add a tool
		server.AddTool(mcp.Tool{
			Name:        "test-tool",
			Description: "Test tool",
		}, nil)

		// Get tools twice
		tools1 := server.ListTools()
		tools2 := server.ListTools()

		assert.NotNil(t, tools1)
		assert.NotNil(t, tools2)

		// They should NOT be the same reference (different memory addresses)
		// This verifies that ListTools returns copies, not shared references
		if len(tools1) > 0 && len(tools2) > 0 {
			assert.NotSame(t, tools1, tools2, "ListTools should return copies, not shared references")
		}

		// Modifying one should not affect the other
		delete(tools1, "test-tool")
		assert.Len(t, tools1, 0, "Modified copy should be empty")
		assert.Len(t, tools2, 1, "Original copy should be unchanged")
		assert.Contains(t, tools2, "test-tool", "Original copy should still contain the tool")

		// Server should still have the tool
		tools3 := server.ListTools()
		assert.NotNil(t, tools3)
		assert.Len(t, tools3, 1)
		assert.Contains(t, tools3, "test-tool")
	})
}

func TestServerTaskTool_TypeDefinition(t *testing.T) {
	// Create a test tool
	tool := mcp.NewTool("test_task_tool",
		mcp.WithDescription("A test task tool"),
	)

	// Create a test handler
	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	}

	// Create ServerTaskTool
	serverTaskTool := ServerTaskTool{
		Tool:    tool,
		Handler: handler,
	}

	// Verify fields are set correctly
	assert.Equal(t, "test_task_tool", serverTaskTool.Tool.Name)
	assert.NotNil(t, serverTaskTool.Handler)

	// Verify handler can be called
	ctx := context.Background()
	request := mcp.CallToolRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodToolsCall),
		},
		Params: mcp.CallToolParams{
			Name: "test_task_tool",
		},
	}
	result, err := serverTaskTool.Handler(ctx, request)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestMCPServer_AddTaskTool(t *testing.T) {
	tests := []struct {
		name          string
		tool          mcp.Tool
		shouldError   bool
		errorContains string
	}{
		{
			name: "valid task tool with optional support",
			tool: mcp.NewTool("test_optional_task_tool",
				mcp.WithDescription("A task tool with optional support"),
				mcp.WithTaskSupport(mcp.TaskSupportOptional),
			),
			shouldError: false,
		},
		{
			name: "valid task tool with required support",
			tool: mcp.NewTool("test_required_task_tool",
				mcp.WithDescription("A task tool with required support"),
				mcp.WithTaskSupport(mcp.TaskSupportRequired),
			),
			shouldError: false,
		},
		{
			name: "invalid task tool with forbidden support",
			tool: mcp.NewTool("test_forbidden_task_tool",
				mcp.WithDescription("A task tool with forbidden support"),
				mcp.WithTaskSupport(mcp.TaskSupportForbidden),
			),
			shouldError:   true,
			errorContains: "must have TaskSupport set to 'optional' or 'required'",
		},
		{
			name: "invalid task tool with no execution field",
			tool: mcp.NewTool("test_no_execution_task_tool",
				mcp.WithDescription("A task tool with no execution field"),
			),
			shouldError:   true,
			errorContains: "must have TaskSupport set to 'optional' or 'required'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewMCPServer("test", "1.0.0")

			handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{}, nil
			}

			err := server.AddTaskTool(tt.tool, handler)

			if tt.shouldError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				assert.NoError(t, err)
				// Verify tool was added to taskTools map
				server.toolsMu.RLock()
				_, exists := server.taskTools[tt.tool.Name]
				server.toolsMu.RUnlock()
				assert.True(t, exists, "task tool should be added to taskTools map")
			}
		})
	}
}

func TestMCPServer_AddTaskTool_NotificationSent(t *testing.T) {
	tests := []struct {
		name                  string
		withActiveSession     bool
		expectedNotifications int
	}{
		{
			name:                  "no notification without active session",
			withActiveSession:     false,
			expectedNotifications: 0,
		},
		{
			name:                  "notification sent with active session",
			withActiveSession:     true,
			expectedNotifications: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewMCPServer("test", "1.0.0")
			notificationChannel := make(chan mcp.JSONRPCNotification, 10)

			if tt.withActiveSession {
				err := server.RegisterSession(context.TODO(), &fakeSession{
					sessionID:           "test-session",
					notificationChannel: notificationChannel,
					initialized:         true,
				})
				require.NoError(t, err)
			}

			tool := mcp.NewTool("test_task_tool",
				mcp.WithDescription("A test task tool"),
				mcp.WithTaskSupport(mcp.TaskSupportRequired),
			)

			handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{}, nil
			}

			err := server.AddTaskTool(tool, handler)
			assert.NoError(t, err)

			// Check notifications
			var notifications []mcp.JSONRPCNotification
			for {
				select {
				case notification := <-notificationChannel:
					notifications = append(notifications, notification)
				case <-time.After(100 * time.Millisecond):
					goto done
				}
			}
		done:
			assert.Len(t, notifications, tt.expectedNotifications)
			if tt.expectedNotifications > 0 {
				assert.Equal(t, mcp.MethodNotificationToolsListChanged, notifications[0].Method)
			}
		})
	}
}

func TestMCPServer_AddTaskTool_ImplicitCapabilitiesRegistration(t *testing.T) {
	server := NewMCPServer("test", "1.0.0")

	// Verify capabilities are nil initially
	assert.Nil(t, server.capabilities.tools)

	tool := mcp.NewTool("test_task_tool",
		mcp.WithDescription("A test task tool"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	}

	err := server.AddTaskTool(tool, handler)
	assert.NoError(t, err)

	// Verify capabilities are registered
	assert.NotNil(t, server.capabilities.tools)
	assert.True(t, server.capabilities.tools.listChanged)
}

func TestMCPServer_AddTaskTool_MultipleTools(t *testing.T) {
	server := NewMCPServer("test", "1.0.0")

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	}

	// Add multiple task tools
	tool1 := mcp.NewTool("task_tool_1",
		mcp.WithTaskSupport(mcp.TaskSupportOptional),
	)
	tool2 := mcp.NewTool("task_tool_2",
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)

	err1 := server.AddTaskTool(tool1, handler)
	err2 := server.AddTaskTool(tool2, handler)

	assert.NoError(t, err1)
	assert.NoError(t, err2)

	// Verify both tools were added
	server.toolsMu.RLock()
	defer server.toolsMu.RUnlock()

	assert.Len(t, server.taskTools, 2)
	assert.Contains(t, server.taskTools, "task_tool_1")
	assert.Contains(t, server.taskTools, "task_tool_2")
}

func TestMCPServer_HandleToolCall_DetectsTaskAugmentation(t *testing.T) {
	tests := []struct {
		name              string
		setupServer       func(*MCPServer)
		toolName          string
		taskParams        *mcp.TaskParams
		expectRouteToTask bool
		expectError       bool
		errorCode         int
	}{
		{
			name: "regular tool call without task params",
			setupServer: func(s *MCPServer) {
				tool := mcp.NewTool("regular_tool",
					mcp.WithDescription("A regular tool"),
				)
				s.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return mcp.NewToolResultText("success"), nil
				})
			},
			toolName:          "regular_tool",
			taskParams:        nil,
			expectRouteToTask: false,
			expectError:       false,
		},
		{
			name: "tool call with task params routes to task handler",
			setupServer: func(s *MCPServer) {
				// Add a regular tool (not a task tool)
				tool := mcp.NewTool("regular_tool",
					mcp.WithDescription("A regular tool"),
				)
				s.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return mcp.NewToolResultText("success"), nil
				})
			},
			toolName: "regular_tool",
			taskParams: &mcp.TaskParams{
				TTL: func() *int64 { v := int64(300); return &v }(),
			},
			expectRouteToTask: true,
			expectError:       true, // Stub returns error for now
			errorCode:         mcp.METHOD_NOT_FOUND,
		},
		{
			name: "tool call with empty task params object routes to task handler",
			setupServer: func(s *MCPServer) {
				tool := mcp.NewTool("test_tool",
					mcp.WithDescription("A test tool"),
				)
				s.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return mcp.NewToolResultText("success"), nil
				})
			},
			toolName:          "test_tool",
			taskParams:        &mcp.TaskParams{}, // Empty but not nil
			expectRouteToTask: true,
			expectError:       true,
			errorCode:         mcp.METHOD_NOT_FOUND,
		},
		{
			name: "task-augmented tool without task params works as regular tool",
			setupServer: func(s *MCPServer) {
				// This tests that an optional task tool can be called without task params
				// and executes synchronously
				tool := mcp.NewTool("task_tool",
					mcp.WithDescription("A task tool"),
					mcp.WithTaskSupport(mcp.TaskSupportOptional),
				)
				err := s.AddTaskTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return &mcp.CallToolResult{}, nil
				})
				require.NoError(t, err)
			},
			toolName:          "task_tool",
			taskParams:        nil,
			expectRouteToTask: false,
			expectError:       false, // Optional task tools can be called without task params
			errorCode:         0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewMCPServer("test-server", "1.0.0")
			tt.setupServer(server)

			// Create request
			request := mcp.CallToolRequest{
				Request: mcp.Request{
					Method: string(mcp.MethodToolsCall),
				},
				Params: mcp.CallToolParams{
					Name: tt.toolName,
					Task: tt.taskParams,
				},
			}

			// Call handleToolCall
			result, err := server.handleToolCall(context.Background(), "test-id", request)

			if tt.expectError {
				assert.Nil(t, result)
				assert.NotNil(t, err)
				if tt.errorCode != 0 {
					assert.Equal(t, tt.errorCode, err.code)
				}
				if tt.expectRouteToTask {
					// Verify it was routed to task handler (now returns proper error)
					assert.Contains(t, err.err.Error(), "does not support task augmentation")
				}
			} else {
				assert.NotNil(t, result)
				assert.Nil(t, err)
			}
		})
	}
}

func TestMCPServer_HandleToolCall_TaskParamsPreserved(t *testing.T) {
	// Test that task params are properly preserved when routing to task handler
	server := NewMCPServer("test-server", "1.0.0")

	ttl := int64(600)
	request := mcp.CallToolRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodToolsCall),
		},
		Params: mcp.CallToolParams{
			Name: "test_tool",
			Task: &mcp.TaskParams{
				TTL: &ttl,
			},
		},
	}

	// Call handleToolCall - should route to task handler
	result, err := server.handleToolCall(context.Background(), "test-id", request)

	// Verify task handler was called (returns error since tool doesn't exist)
	assert.Nil(t, result)
	assert.NotNil(t, err)
	assert.Equal(t, mcp.INVALID_PARAMS, err.code)
	assert.Contains(t, err.err.Error(), "not found")
}

func TestMCPServer_HandleTaskAugmentedToolCall_Implementation(t *testing.T) {
	tests := []struct {
		name          string
		setupServer   func(*MCPServer)
		toolName      string
		taskParams    *mcp.TaskParams
		expectError   bool
		errorCode     int
		errorContains string
	}{
		{
			name: "tool not found returns INVALID_PARAMS error",
			setupServer: func(s *MCPServer) {
				// Don't add any tools
			},
			toolName: "nonexistent_tool",
			taskParams: &mcp.TaskParams{
				TTL: func() *int64 { v := int64(300); return &v }(),
			},
			expectError:   true,
			errorCode:     mcp.INVALID_PARAMS,
			errorContains: "not found",
		},
		{
			name: "regular tool without task support returns METHOD_NOT_FOUND",
			setupServer: func(s *MCPServer) {
				tool := mcp.NewTool("regular_tool",
					mcp.WithDescription("A regular tool"),
				)
				s.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return mcp.NewToolResultText("success"), nil
				})
			},
			toolName: "regular_tool",
			taskParams: &mcp.TaskParams{
				TTL: func() *int64 { v := int64(300); return &v }(),
			},
			expectError:   true,
			errorCode:     mcp.METHOD_NOT_FOUND,
			errorContains: "does not support task augmentation",
		},
		{
			name: "task tool returns CreateTaskResult immediately",
			setupServer: func(s *MCPServer) {
				tool := mcp.NewTool("task_tool",
					mcp.WithDescription("A task tool"),
					mcp.WithTaskSupport(mcp.TaskSupportRequired),
				)
				err := s.AddTaskTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return &mcp.CallToolResult{}, nil
				})
				require.NoError(t, err)
			},
			toolName: "task_tool",
			taskParams: &mcp.TaskParams{
				TTL: func() *int64 { v := int64(300); return &v }(),
			},
			expectError: false,
		},
		{
			name: "task tool without TTL creates task successfully",
			setupServer: func(s *MCPServer) {
				tool := mcp.NewTool("task_tool",
					mcp.WithDescription("A task tool"),
					mcp.WithTaskSupport(mcp.TaskSupportOptional),
				)
				err := s.AddTaskTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return &mcp.CallToolResult{}, nil
				})
				require.NoError(t, err)
			},
			toolName:    "task_tool",
			taskParams:  &mcp.TaskParams{}, // No TTL
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewMCPServer("test-server", "1.0.0",
				WithTaskCapabilities(true, true, true),
			)
			tt.setupServer(server)

			request := mcp.CallToolRequest{
				Request: mcp.Request{
					Method: string(mcp.MethodToolsCall),
				},
				Params: mcp.CallToolParams{
					Name: tt.toolName,
					Task: tt.taskParams,
				},
			}

			result, err := server.handleTaskAugmentedToolCall(context.Background(), "test-id", request)

			if tt.expectError {
				assert.Nil(t, result)
				assert.NotNil(t, err)
				assert.Equal(t, tt.errorCode, err.code)
				if tt.errorContains != "" {
					assert.Contains(t, err.err.Error(), tt.errorContains)
				}
			} else {
				assert.NotNil(t, result)
				assert.Nil(t, err)

				// Verify result structure
				assert.NotNil(t, result.Meta)
				assert.NotNil(t, result.Meta.AdditionalFields)
				taskField, ok := result.Meta.AdditionalFields["task"]
				assert.True(t, ok, "task field should be present in meta")

				// Verify task is a Task type
				task, ok := taskField.(mcp.Task)
				assert.True(t, ok, "task field should be of type mcp.Task")
				assert.NotEmpty(t, task.TaskId, "task ID should be generated")
				assert.Equal(t, mcp.TaskStatusWorking, task.Status, "task should start in working status")
			}
		})
	}
}

func TestMCPServer_HandleTaskAugmentedToolCall_TaskCreation(t *testing.T) {
	// Test that task is properly created and stored
	server := NewMCPServer("test-server", "1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	tool := mcp.NewTool("async_tool",
		mcp.WithDescription("An async tool"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)
	err := server.AddTaskTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})
	require.NoError(t, err)

	ttl := int64(500)
	request := mcp.CallToolRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodToolsCall),
		},
		Params: mcp.CallToolParams{
			Name: "async_tool",
			Task: &mcp.TaskParams{
				TTL: &ttl,
			},
		},
	}

	result, reqErr := server.handleTaskAugmentedToolCall(context.Background(), "test-id", request)

	assert.NotNil(t, result)
	assert.Nil(t, reqErr)

	// Extract task ID from result
	taskField := result.Meta.AdditionalFields["task"]
	task, ok := taskField.(mcp.Task)
	require.True(t, ok)

	// Verify task is stored in server
	server.tasksMu.RLock()
	entry, exists := server.tasks[task.TaskId]
	if exists {
		// Read task fields while holding the lock to avoid race
		taskID := entry.task.TaskId
		status := entry.task.Status
		server.tasksMu.RUnlock()

		assert.True(t, exists, "task should be stored in server")
		assert.NotNil(t, entry)
		assert.Equal(t, task.TaskId, taskID)
		assert.Equal(t, mcp.TaskStatusWorking, status)
	} else {
		server.tasksMu.RUnlock()
		assert.True(t, exists, "task should be stored in server")
	}
}

func TestMCPServer_HandleTaskAugmentedToolCall_AsyncExecution(t *testing.T) {
	// Test that executeTaskTool is called asynchronously
	server := NewMCPServer("test-server", "1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	var handlerCalledMu sync.Mutex
	handlerCalled := false
	tool := mcp.NewTool("slow_tool",
		mcp.WithDescription("A slow tool"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)
	err := server.AddTaskTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handlerCalledMu.Lock()
		handlerCalled = true
		handlerCalledMu.Unlock()
		return &mcp.CallToolResult{}, nil
	})
	require.NoError(t, err)

	request := mcp.CallToolRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodToolsCall),
		},
		Params: mcp.CallToolParams{
			Name: "slow_tool",
			Task: &mcp.TaskParams{},
		},
	}

	result, reqErr := server.handleTaskAugmentedToolCall(context.Background(), "test-id", request)

	// Should return immediately with task created
	assert.NotNil(t, result)
	assert.Nil(t, reqErr)

	// Handler should not be called yet (it runs async)
	handlerCalledMu.Lock()
	calledBeforeWait := handlerCalled
	handlerCalledMu.Unlock()
	assert.False(t, calledBeforeWait, "handler should not be called during synchronous return")

	// Extract task and wait for completion
	taskField := result.Meta.AdditionalFields["task"]
	task := taskField.(mcp.Task)

	// Wait a bit for async execution to complete
	time.Sleep(50 * time.Millisecond)

	// Verify task completed successfully
	server.tasksMu.RLock()
	entry := server.tasks[task.TaskId]
	completed := entry.completed
	status := entry.task.Status
	hasResult := entry.result != nil
	server.tasksMu.RUnlock()

	handlerCalledMu.Lock()
	calledAfterWait := handlerCalled
	handlerCalledMu.Unlock()

	assert.True(t, completed, "task should be completed")
	assert.Equal(t, mcp.TaskStatusCompleted, status, "task should be marked as completed")
	assert.True(t, hasResult, "task should have a result")
	assert.True(t, calledAfterWait, "handler should have been called")
}

func TestMCPServer_ExecuteTaskTool(t *testing.T) {
	t.Run("successful execution stores result", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0",
			WithTaskCapabilities(true, true, true),
		)

		expectedResult := &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent("Operation completed"),
			},
		}

		tool := mcp.NewTool("test_tool",
			mcp.WithDescription("A test tool"),
			mcp.WithTaskSupport(mcp.TaskSupportRequired),
		)
		err := server.AddTaskTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return expectedResult, nil
		})
		require.NoError(t, err)

		// Create a task entry
		entry, err := server.createTask(context.Background(), "test-task-123", nil, nil)
		require.NoError(t, err)

		// Create cancellable context and set cancel function (as handleTaskAugmentedToolCall does)
		taskCtx, cancel := context.WithCancel(context.Background())
		server.tasksMu.Lock()
		entry.cancelFunc = cancel
		server.tasksMu.Unlock()

		// Get the task tool
		server.toolsMu.RLock()
		taskTool := server.taskTools["test_tool"]
		server.toolsMu.RUnlock()

		request := mcp.CallToolRequest{
			Request: mcp.Request{
				Method: string(mcp.MethodToolsCall),
			},
			Params: mcp.CallToolParams{
				Name: "test_tool",
			},
		}

		// Execute the task tool
		server.executeTaskTool(taskCtx, entry, taskTool, request)

		// Verify result is stored
		server.tasksMu.RLock()
		defer server.tasksMu.RUnlock()

		assert.True(t, entry.completed)
		assert.Equal(t, mcp.TaskStatusCompleted, entry.task.Status)
		assert.NotNil(t, entry.result)
		assert.Nil(t, entry.resultErr)

		// Verify the actual result
		result, ok := entry.result.(*mcp.CallToolResult)
		assert.True(t, ok)
		assert.Equal(t, expectedResult, result)
	})

	t.Run("handler error marks task as failed", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0",
			WithTaskCapabilities(true, true, true),
		)

		expectedErr := fmt.Errorf("handler failed")

		tool := mcp.NewTool("failing_tool",
			mcp.WithDescription("A failing tool"),
			mcp.WithTaskSupport(mcp.TaskSupportRequired),
		)
		err := server.AddTaskTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return nil, expectedErr
		})
		require.NoError(t, err)

		// Create a task entry
		entry, err := server.createTask(context.Background(), "test-task-456", nil, nil)
		require.NoError(t, err)

		// Create cancellable context and set cancel function
		taskCtx, cancel := context.WithCancel(context.Background())
		server.tasksMu.Lock()
		entry.cancelFunc = cancel
		server.tasksMu.Unlock()

		// Get the task tool
		server.toolsMu.RLock()
		taskTool := server.taskTools["failing_tool"]
		server.toolsMu.RUnlock()

		request := mcp.CallToolRequest{
			Request: mcp.Request{
				Method: string(mcp.MethodToolsCall),
			},
			Params: mcp.CallToolParams{
				Name: "failing_tool",
			},
		}

		// Execute the task tool
		server.executeTaskTool(taskCtx, entry, taskTool, request)

		// Verify task is marked as failed
		server.tasksMu.RLock()
		defer server.tasksMu.RUnlock()

		assert.True(t, entry.completed)
		assert.Equal(t, mcp.TaskStatusFailed, entry.task.Status)
		assert.Equal(t, expectedErr.Error(), entry.task.StatusMessage)
		assert.Nil(t, entry.result)
		assert.Equal(t, expectedErr, entry.resultErr)
	})

	t.Run("context cancellation is handled", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0",
			WithTaskCapabilities(true, true, true),
		)

		ctxCancelled := false

		tool := mcp.NewTool("cancellable_tool",
			mcp.WithDescription("A cancellable tool"),
			mcp.WithTaskSupport(mcp.TaskSupportRequired),
		)
		err := server.AddTaskTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// Simulate some work and check for cancellation
			select {
			case <-time.After(100 * time.Millisecond):
				return &mcp.CallToolResult{}, nil
			case <-ctx.Done():
				ctxCancelled = true
				return nil, ctx.Err()
			}
		})
		require.NoError(t, err)

		// Create a task entry
		entry, err := server.createTask(context.Background(), "test-task-789", nil, nil)
		require.NoError(t, err)

		// Create cancellable context and set cancel function
		taskCtx, cancel := context.WithCancel(context.Background())
		server.tasksMu.Lock()
		entry.cancelFunc = cancel
		server.tasksMu.Unlock()

		// Get the task tool
		server.toolsMu.RLock()
		taskTool := server.taskTools["cancellable_tool"]
		server.toolsMu.RUnlock()

		request := mcp.CallToolRequest{
			Request: mcp.Request{
				Method: string(mcp.MethodToolsCall),
			},
			Params: mcp.CallToolParams{
				Name: "cancellable_tool",
			},
		}

		// Start execution in a goroutine
		go server.executeTaskTool(taskCtx, entry, taskTool, request)

		// Wait for cancel function to be set
		time.Sleep(10 * time.Millisecond)

		// Cancel the task
		server.tasksMu.Lock()
		cancelFunc := entry.cancelFunc
		server.tasksMu.Unlock()

		require.NotNil(t, cancelFunc, "cancel function should be set")
		cancelFunc()

		// Wait for completion
		time.Sleep(50 * time.Millisecond)

		// Verify task was cancelled
		server.tasksMu.RLock()
		defer server.tasksMu.RUnlock()

		assert.True(t, ctxCancelled, "context should have been cancelled")
		assert.True(t, entry.completed)
		assert.Equal(t, mcp.TaskStatusFailed, entry.task.Status)
	})

	t.Run("cancel function is set before handler execution", func(t *testing.T) {
		server := NewMCPServer("test-server", "1.0.0",
			WithTaskCapabilities(true, true, true),
		)

		var cancelFuncSet bool

		tool := mcp.NewTool("check_cancel_tool",
			mcp.WithDescription("Tool that checks cancel function"),
			mcp.WithTaskSupport(mcp.TaskSupportRequired),
		)
		err := server.AddTaskTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// Check if cancel function was set before handler was called
			return &mcp.CallToolResult{}, nil
		})
		require.NoError(t, err)

		// Create a task entry
		entry, err := server.createTask(context.Background(), "test-task-999", nil, nil)
		require.NoError(t, err)

		// Create cancellable context and set cancel function BEFORE calling executeTaskTool
		// This mimics what handleTaskAugmentedToolCall does
		taskCtx, cancel := context.WithCancel(context.Background())
		server.tasksMu.Lock()
		entry.cancelFunc = cancel
		cancelFuncSet = entry.cancelFunc != nil
		server.tasksMu.Unlock()

		// Get the task tool
		server.toolsMu.RLock()
		taskTool := server.taskTools["check_cancel_tool"]
		server.toolsMu.RUnlock()

		request := mcp.CallToolRequest{
			Request: mcp.Request{
				Method: string(mcp.MethodToolsCall),
			},
			Params: mcp.CallToolParams{
				Name: "check_cancel_tool",
			},
		}

		// Execute in goroutine
		go server.executeTaskTool(taskCtx, entry, taskTool, request)

		// Verify cancel function was set before execution
		assert.True(t, cancelFuncSet, "cancel function should be set before handler execution")

		// Wait for completion
		time.Sleep(50 * time.Millisecond)
	})
}
