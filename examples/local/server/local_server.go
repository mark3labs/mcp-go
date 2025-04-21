package server

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type LocalServer struct {
	Server *server.MCPServer
}

func CreateLocalServer(ctx context.Context) *LocalServer {
	mcpServer := server.NewMCPServer("test", "1.0.0")

	mcpServer.AddTool(mcp.Tool{
		Name:        "test-tool",
		Description: "test tool",
		InputSchema: mcp.ToolInputSchema{},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "test tool result",
				},
			},
		}, nil
	})

	return &LocalServer{
		Server: mcpServer,
	}
}
