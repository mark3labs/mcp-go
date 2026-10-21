package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestMiddlewareFactoryCanRegisterFutureMiddleware(t *testing.T) {
	for _, mode := range []string{"normal call", "regular tool as task"} {
		t.Run(mode, func(t *testing.T) {
			var s *MCPServer
			s = NewMCPServer("test", "1", WithToolHandlerMiddleware(func(next ToolHandlerFunc) ToolHandlerFunc {
				s.Use(func(handler ToolHandlerFunc) ToolHandlerFunc { return handler })
				return next
			}))
			tool := ServerTool{Tool: mcp.NewTool("tool"), Handler: func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return mcp.NewToolResultText("ok"), nil
			}}
			s.AddTool(tool.Tool, tool.Handler)
			done := make(chan *requestError, 1)
			go func() {
				request := mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "tool"}}
				if mode == "regular tool as task" {
					entry, err := s.createTask(t.Context(), "task", "tool", nil, nil)
					if err != nil {
						done <- &requestError{err: err}
						return
					}
					s.executeRegularToolAsTask(t.Context(), entry, tool, request)
					done <- nil
					return
				}
				_, err := s.handleToolCall(t.Context(), 1, request)
				done <- err
			}()
			select {
			case err := <-done:
				require.Nil(t, err)
			case <-time.After(time.Second):
				t.Fatal("middleware factory deadlocked registering middleware")
			}
		})
	}
}
