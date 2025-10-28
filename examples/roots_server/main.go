package main

import (
	"context"
	"fmt"
	"log"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// handleNotification handles JSON-RPC notifications by printing the notification method to standard output.
func handleNotification(ctx context.Context, notification mcp.JSONRPCNotification) {
	fmt.Printf("notification received: %v\n", notification.Method)
}

// main sets up and runs an MCP stdio server named "roots-stdio-server" with tool and roots capabilities.
// It registers a handler for RootsListChanged notifications and adds a "roots" tool
// that requests and returns the current roots list. The program serves the MCP server over stdio and
// logs a fatal error if the server fails to start.
func main() {
	// Enable roots capability
	opts := []server.ServerOption{
		server.WithToolCapabilities(true),
		server.WithRoots(),
	}
	// Create MCP server with roots capability
	mcpServer := server.NewMCPServer("roots-stdio-server", "1.0.0", opts...)

	// Register roots list-change notification handler
	mcpServer.AddNotificationHandler(mcp.MethodNotificationRootsListChanged, handleNotification)

	// Add a simple tool to test roots list
	mcpServer.AddTool(mcp.Tool{
		Name:        "roots",
		Description: "Requests and returns the current list of roots from the connected client",
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: map[string]any{},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		rootRequest := mcp.ListRootsRequest{}

		if result, err := mcpServer.RequestRoots(ctx, rootRequest); err == nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: fmt.Sprintf("Root list: %v", result.Roots),
					},
				},
				StructuredContent: map[string]any{
					"roots": result.Roots,
				},
			}, nil

		} else {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: fmt.Sprintf("Failed to list roots: %v", err),
					},
				},
				IsError: true,
			}, nil
		}
	})

	// Create stdio server
	if err := server.ServeStdio(mcpServer); err != nil {
		log.Fatalf("Server Stdio error: %v\n", err)
	}
}
