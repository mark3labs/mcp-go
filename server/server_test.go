package server

import (
	"context"
	"encoding/json"
	"fmt"
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
				err := server.RegisterSession(&fakeSession{
					sessionID:           "test",
					notificationChannel: notificationChannel,
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
				assert.Equal(t, "notifications/tools/list_changed", notifications[0].Method)
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
					err := server.RegisterSession(&fakeSession{
						sessionID:           fmt.Sprintf("test%d", i),
						notificationChannel: notificationChannel,
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
					assert.Equal(t, "notifications/tools/list_changed", notification.Method)
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
				err := server.RegisterSession(&fakeSession{
					sessionID:           "test",
					notificationChannel: notificationChannel,
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
				assert.Equal(t, "notifications/tools/list_changed", notifications[0].Method)
				assert.Equal(t, "notifications/tools/list_changed", notifications[1].Method)
				tools := toolsList.(mcp.JSONRPCResponse).Result.(mcp.ListToolsResult).Tools
				assert.Len(t, tools, 2)
				assert.Equal(t, "test-tool-1", tools[0].Name)
				assert.Equal(t, "test-tool-2", tools[1].Name)
			},
		},
		{
			name: "DeleteTools sends single notifications/tools/list_changed",
			action: func(t *testing.T, server *MCPServer, notificationChannel chan mcp.JSONRPCNotification) {
				err := server.RegisterSession(&fakeSession{
					sessionID:           "test",
					notificationChannel: notificationChannel,
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
				assert.Equal(t, "notifications/tools/list_changed", notifications[0].Method)
				// One for DeleteTools
				assert.Equal(t, "notifications/tools/list_changed", notifications[1].Method)

				// Expect a successful response with an empty list of tools
				resp, ok := toolsList.(mcp.JSONRPCResponse)
				assert.True(t, ok, "Expected JSONRPCResponse, got %T", toolsList)

				result, ok := resp.Result.(mcp.ListToolsResult)
				assert.True(t, ok, "Expected ListToolsResult, got %T", resp.Result)

				assert.Empty(t, result.Tools, "Expected empty tools list")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			server := NewMCPServer("test-server", "1.0.0")
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
		message  interface{}
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

	tests := []struct {
		name     string
		message  string
		validate func(t *testing.T, response mcp.JSONRPCMessage)
	}{
		{
			name: "List resources with cursor",
			message: `{
                    "jsonrpc": "2.0",
                    "id": 1,
                    "method": "resources/list",
                    "params": {
                        "cursor": "test-cursor"
                    }
                }`,
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
			name: "active session",
			contextPrepare: func(ctx context.Context, srv *MCPServer) context.Context {
				return srv.WithContext(ctx, fakeSession{
					sessionID:           "test",
					notificationChannel: make(chan mcp.JSONRPCNotification, 10),
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

				result, ok := resp.Result.(*mcp.GetPromptResult)
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

				result, ok := resp.Result.(*mcp.GetPromptResult)
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
	server := NewMCPServer("test-server", "1.0.0")

	tests := []struct {
		name        string
		message     string
		expectedErr int
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
		},
		{
			name:        "Missing JSONRPC version",
			message:     `{"id": 1, "method": "initialize"}`,
			expectedErr: mcp.INVALID_REQUEST,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := server.HandleMessage(
				context.Background(),
				[]byte(tt.message),
			)
			assert.NotNil(t, response)

			errorResponse, ok := response.(mcp.JSONRPCError)
			assert.True(t, ok)
			assert.Equal(t, tt.expectedErr, errorResponse.Error.Code)
		})
	}
}

func TestMCPServer_HandleUndefinedHandlers(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0",
		WithResourceCapabilities(true, true),
		WithPromptCapabilities(true),
		WithToolCapabilities(true),
	)

	// Add a test tool to enable tool capabilities
	server.AddTool(mcp.Tool{
		Name:        "test-tool",
		Description: "Test tool",
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: map[string]interface{}{},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})

	tests := []struct {
		name        string
		message     string
		expectedErr int
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
			expectedErr: mcp.INVALID_PARAMS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := server.HandleMessage(
				context.Background(),
				[]byte(tt.message),
			)
			assert.NotNil(t, response)

			errorResponse, ok := response.(mcp.JSONRPCError)
			assert.True(t, ok)
			assert.Equal(t, tt.expectedErr, errorResponse.Error.Code)
		})
	}
}

func TestMCPServer_HandleMethodsWithoutCapabilities(t *testing.T) {
	tests := []struct {
		name        string
		message     string
		options     []ServerOption
		expectedErr int
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
			options:     []ServerOption{}, // No capabilities at all
			expectedErr: mcp.METHOD_NOT_FOUND,
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
			options:     []ServerOption{}, // No capabilities at all
			expectedErr: mcp.METHOD_NOT_FOUND,
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
			options:     []ServerOption{}, // No capabilities at all
			expectedErr: mcp.METHOD_NOT_FOUND,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewMCPServer("test-server", "1.0.0", tt.options...)
			response := server.HandleMessage(
				context.Background(),
				[]byte(tt.message),
			)
			assert.NotNil(t, response)

			errorResponse, ok := response.(mcp.JSONRPCError)
			assert.True(t, ok)
			assert.Equal(t, tt.expectedErr, errorResponse.Error.Code)
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

func createTestServer() *MCPServer {
	server := NewMCPServer("test-server", "1.0.0",
		WithResourceCapabilities(true, true),
		WithPromptCapabilities(true),
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
}

func (f fakeSession) SessionID() string {
	return f.sessionID
}

func (f fakeSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return f.notificationChannel
}

var _ ClientSession = fakeSession{}
