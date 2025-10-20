package main

import (
	"context"
	"fmt"
	"log"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func handleNotification(ctx context.Context, notification mcp.JSONRPCNotification) {
	fmt.Printf("notification received: %v", notification.Notification.Method)
}

func main() {
	// Enable roots capability
	opts := []server.ServerOption{
		server.WithToolCapabilities(true),
		server.WithRoots(),
	}
	// Create MCP server with roots capability
	mcpServer := server.NewMCPServer("roots-http-server", "1.0.0", opts...)

	// Add list root list change notification
	mcpServer.AddNotificationHandler(mcp.MethodNotificationToolsListChanged, handleNotification)

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
		rootRequest := mcp.ListRootsRequest{
			Request: mcp.Request{
				Method: string(mcp.MethodListRoots),
			},
		}

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
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: fmt.Sprintf("Fail to list root, %v", err),
					},
				},
			}, err
		}
	})

	log.Println("Starting MCP server with roots support")
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
