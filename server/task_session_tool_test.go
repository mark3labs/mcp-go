package server

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// handleToolCall finds a session's own tools before the server's, and a call
// with task params goes on to handleTaskAugmentedToolCall. That used to look
// the tool up again in the server's tools only, so a session tool that
// supports tasks couldn't run as one, and a session tool shadowing a server
// tool of the same name ran the server tool's handler, or failed if the
// server tool doesn't support tasks.
func TestTaskAugmentedCallUsesSessionTools(t *testing.T) {
	tests := []struct {
		name       string
		serverTool func(srv *MCPServer)
	}{
		{name: "session tool", serverTool: func(*MCPServer) {}},
		{name: "shadowing a server tool", serverTool: func(srv *MCPServer) {
			srv.AddTool(mcp.NewTool("work", mcp.WithTaskSupport(mcp.TaskSupportOptional)), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return mcp.NewToolResultText("server tool"), nil
			})
		}},
		{name: "shadowing a server tool without task support", serverTool: func(srv *MCPServer) {
			srv.AddTool(mcp.NewTool("work"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return mcp.NewToolResultText("server tool"), nil
			})
		}},
		{name: "shadowing a server task tool", serverTool: func(srv *MCPServer) {
			srv.AddTaskTool(mcp.NewTool("work", mcp.WithTaskSupport(mcp.TaskSupportRequired)), func(context.Context, mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
				return &mcp.CreateTaskResult{Content: []mcp.Content{mcp.NewTextContent("server task tool")}}, nil
			})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewMCPServer("test-server", "1.0.0", WithToolCapabilities(true), WithTaskCapabilities(true, true, true))
			tt.serverTool(srv)
			tool := mcp.NewTool("work", mcp.WithTaskSupport(mcp.TaskSupportOptional))
			session := &sessionTestClientWithTools{
				sessionID:           "task-session",
				notificationChannel: make(chan mcp.JSONRPCNotification, 10),
				initialized:         true,
			}
			require.NoError(t, srv.RegisterSession(t.Context(), session))
			require.NoError(t, srv.AddSessionTool(session.SessionID(), tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return mcp.NewToolResultText("session tool"), nil
			}))
			ctx := srv.WithContext(t.Context(), session)

			created, reqErr := srv.handleToolCall(ctx, 1, mcp.CallToolRequest{
				Params: mcp.CallToolParams{Name: "work", Task: &mcp.TaskParams{}},
			})
			require.Nil(t, reqErr)
			taskID := created.(*mcp.CreateTaskResult).Task.TaskId

			result, reqErr := srv.handleTaskResult(ctx, 2, mcp.TaskResultRequest{
				Params: mcp.TaskResultParams{TaskId: taskID},
			})
			require.Nil(t, reqErr)
			require.Len(t, result.Content, 1)
			assert.Equal(t, "session tool", result.Content[0].(mcp.TextContent).Text)
		})
	}
}
