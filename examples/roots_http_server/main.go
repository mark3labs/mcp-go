package main

import (
	"context"
	"fmt"
	"log"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// handleNotification prints the method name of the received MCP JSON-RPC notification to standard output.
func handleNotification(ctx context.Context, notification mcp.JSONRPCNotification) {
	fmt.Printf("notification received: %v", notification.Method)
}

// main starts an MCP HTTP server named "roots-http-server" with tool capabilities and roots support.
// It registers a notification handler for ToolsListChanged, adds a "roots" tool that queries the server's roots and returns a textual result,
// logs startup and usage instructions, and launches a streamable HTTP server on port 8080.
func main() {
	// Enable roots capability
	opts := []server.ServerOption{
		server.WithToolCapabilities(true),
		server.WithRoots(),
	}
	// Create MCP server with roots capability
	mcpServer := server.NewMCPServer("roots-http-server", "1.0.0", opts...)

	// Add list root list change notification
	mcpServer.AddNotificationHandler(mcp.MethodNotificationRootsListChanged, handleNotification)

	// Add a simple tool to test roots list
	mcpServer.AddTool(mcp.Tool{
		Name:        "roots",
		Description: "list root result",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"testonly": map[string]any{
					"type":        "string",
					"description": "is this test only?",
				},
			},
			Required: []string{"testonly"},
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
			}, nil

		} else {
			return nil, err
		}
	})

	log.Println("Starting MCP Http server with roots support")
	log.Println("Http Endpoint: http://localhost:8080/mcp")
	log.Println("")
	log.Println("This server supports roots over HTTP transport.")
	log.Println("Clients must:")
	log.Println("1. Initialize with roots capability")
	log.Println("2. Establish SSE connection for bidirectional communication")
	log.Println("3. Handle incoming roots requests from the server")
	log.Println("4. Send responses back via HTTP POST")
	log.Println("")
	log.Println("Available tools:")
	log.Println("- roots: Send back the list root request)")

	// Create HTTP server
	httpOpts := []server.StreamableHTTPOption{}
	httpServer := server.NewStreamableHTTPServer(mcpServer, httpOpts...)
	fmt.Printf("Starting HTTP server\n")
	if err := httpServer.Start(":8080"); err != nil {
		fmt.Printf("HTTP server failed: %v\n", err)
	}
}
