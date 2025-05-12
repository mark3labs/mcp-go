package server

import (
	"context"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPServer_RemoveResource(t *testing.T) {
	tests := []struct {
		name                  string
		action                func(*testing.T, *MCPServer, chan mcp.JSONRPCNotification)
		expectedNotifications int
		validate              func(*testing.T, []mcp.JSONRPCNotification, mcp.JSONRPCMessage)
	}{
		{
			name: "RemoveResource removes the resource from the server",
			action: func(t *testing.T, server *MCPServer, notificationChannel chan mcp.JSONRPCNotification) {
				// Add a test resource
				server.AddResource(
					mcp.NewResource(
						"test://resource1",
						"Resource 1",
						mcp.WithResourceDescription("Test resource 1"),
						mcp.WithMIMEType("text/plain"),
					),
					func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
						return []mcp.ResourceContents{
							mcp.TextResourceContents{
								URI:      "test://resource1",
								MIMEType: "text/plain",
								Text:     "test content 1",
							},
						}, nil
					},
				)

				// Add a second resource
				server.AddResource(
					mcp.NewResource(
						"test://resource2",
						"Resource 2",
						mcp.WithResourceDescription("Test resource 2"),
						mcp.WithMIMEType("text/plain"),
					),
					func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
						return []mcp.ResourceContents{
							mcp.TextResourceContents{
								URI:      "test://resource2",
								MIMEType: "text/plain",
								Text:     "test content 2",
							},
						}, nil
					},
				)

				// First, verify we have two resources
				response := server.HandleMessage(context.Background(), []byte(`{
					"jsonrpc": "2.0",
					"id": 1,
					"method": "resources/list"
				}`))
				resp, ok := response.(mcp.JSONRPCResponse)
				assert.True(t, ok)
				result, ok := resp.Result.(mcp.ListResourcesResult)
				assert.True(t, ok)
				assert.Len(t, result.Resources, 2)

				// Now register session to receive notifications
				err := server.RegisterSession(context.TODO(), &fakeSession{
					sessionID:           "test",
					notificationChannel: notificationChannel,
					initialized:         true,
				})
				require.NoError(t, err)

				// Now remove one resource
				server.RemoveResource("test://resource1")
			},
			expectedNotifications: 1,
			validate: func(t *testing.T, notifications []mcp.JSONRPCNotification, resourcesList mcp.JSONRPCMessage) {
				// Check that we received a list_changed notification
				assert.Equal(t, mcp.MethodNotificationResourcesListChanged, notifications[0].Method)

				// Verify we now have only one resource
				resp, ok := resourcesList.(mcp.JSONRPCResponse)
				assert.True(t, ok, "Expected JSONRPCResponse, got %T", resourcesList)

				result, ok := resp.Result.(mcp.ListResourcesResult)
				assert.True(t, ok, "Expected ListResourcesResult, got %T", resp.Result)

				assert.Len(t, result.Resources, 1)
				assert.Equal(t, "Resource 2", result.Resources[0].Name)
			},
		},
		{
			name: "RemoveResource with non-existent resource does nothing and not receives notifications from MCPServer",
			action: func(t *testing.T, server *MCPServer, notificationChannel chan mcp.JSONRPCNotification) {
				// Add a test resource
				server.AddResource(
					mcp.NewResource(
						"test://resource1",
						"Resource 1",
						mcp.WithResourceDescription("Test resource 1"),
						mcp.WithMIMEType("text/plain"),
					),
					func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
						return []mcp.ResourceContents{
							mcp.TextResourceContents{
								URI:      "test://resource1",
								MIMEType: "text/plain",
								Text:     "test content 1",
							},
						}, nil
					},
				)

				// Register session to receive notifications
				err := server.RegisterSession(context.TODO(), &fakeSession{
					sessionID:           "test",
					notificationChannel: notificationChannel,
					initialized:         true,
				})
				require.NoError(t, err)

				// Remove a non-existent resource
				server.RemoveResource("test://nonexistent")
			},
			expectedNotifications: 0, // No notifications expected
			validate: func(t *testing.T, notifications []mcp.JSONRPCNotification, resourcesList mcp.JSONRPCMessage) {
				// verify that no notifications were sent
				assert.Empty(t, notifications)

				// The original resource should still be there
				resp, ok := resourcesList.(mcp.JSONRPCResponse)
				assert.True(t, ok)

				result, ok := resp.Result.(mcp.ListResourcesResult)
				assert.True(t, ok)

				assert.Len(t, result.Resources, 1)
				assert.Equal(t, "Resource 1", result.Resources[0].Name)
			},
		},
		{
			name: "RemoveResource with no listChanged capability doesn't send notification",
			action: func(t *testing.T, server *MCPServer, notificationChannel chan mcp.JSONRPCNotification) {
				// Create a new server without listChanged capability
				noListChangedServer := NewMCPServer(
					"test-server",
					"1.0.0",
					WithResourceCapabilities(true, false), // Subscribe but not listChanged
				)

				// Add a resource
				noListChangedServer.AddResource(
					mcp.NewResource(
						"test://resource1",
						"Resource 1",
						mcp.WithResourceDescription("Test resource 1"),
						mcp.WithMIMEType("text/plain"),
					),
					func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
						return []mcp.ResourceContents{
							mcp.TextResourceContents{
								URI:      "test://resource1",
								MIMEType: "text/plain",
								Text:     "test content 1",
							},
						}, nil
					},
				)

				// Register session to receive notifications
				err := noListChangedServer.RegisterSession(context.TODO(), &fakeSession{
					sessionID:           "test",
					notificationChannel: notificationChannel,
					initialized:         true,
				})
				require.NoError(t, err)

				// Remove the resource
				noListChangedServer.RemoveResource("test://resource1")

				// The test can now proceed without waiting for notifications
				// since we don't expect any
			},
			expectedNotifications: 0, // No notifications expected
			validate: func(t *testing.T, notifications []mcp.JSONRPCNotification, resourcesList mcp.JSONRPCMessage) {
				// Nothing to do here, we're just verifying that no notifications were sent
				assert.Empty(t, notifications)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			server := NewMCPServer(
				"test-server",
				"1.0.0",
				WithResourceCapabilities(true, true),
			)

			// Initialize the server
			_ = server.HandleMessage(ctx, []byte(`{
				"jsonrpc": "2.0",
				"id": 1,
				"method": "initialize"
			}`))

			notificationChannel := make(chan mcp.JSONRPCNotification, 100)
			notifications := make([]mcp.JSONRPCNotification, 0)

			tt.action(t, server, notificationChannel)

			// Collect notifications with a timeout
			if tt.expectedNotifications > 0 {
				for i := 0; i < tt.expectedNotifications; i++ {
					select {
					case notification := <-notificationChannel:
						notifications = append(notifications, notification)
					case <-time.After(1 * time.Second):
						t.Fatalf("Expected %d notifications but only received %d", tt.expectedNotifications, len(notifications))
					}
				}
			} else {
				// If no notifications expected, wait a brief period to ensure none are sent
				select {
				case notification := <-notificationChannel:
					notifications = append(notifications, notification)
				case <-time.After(100 * time.Millisecond):
					// This is the expected path - no notifications
				}
			}

			// Get final resources list
			listMessage := `{
				"jsonrpc": "2.0",
				"id": 1,
				"method": "resources/list"
			}`
			resourcesList := server.HandleMessage(ctx, []byte(listMessage))

			// Validate the results
			tt.validate(t, notifications, resourcesList)
		})
	}
}
