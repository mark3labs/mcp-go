package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Custom context function for SSE connections
func customContextFunc(ctx context.Context, r *http.Request) context.Context {
	params := server.GetRouteParams(ctx)
	log.Printf("SSE Connection Established - Route Parameters: %+v", params)
	log.Printf("Request Path: %s", r.URL.Path)
	return ctx
}

// Message handler for simulating message sending
func messageHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Get channel parameter from context
	channel := server.GetRouteParam(ctx, "channel")
	log.Printf("Processing Message - Channel Parameter: %s", channel)

	if channel == "" {
		return mcp.NewToolResultText("Failed to get channel parameter"), nil
	}

	message := fmt.Sprintf("Message sent to channel: %s", channel)
	return mcp.NewToolResultText(message), nil
}

func main() {
	// Create MCP Server
	mcpServer := server.NewMCPServer("test-server", "1.0.0")

	// Register test tool
	mcpServer.AddTool(mcp.NewTool("send_message"), messageHandler)

	// Create SSE Server with custom route pattern
	sseServer := server.NewSSEServer(mcpServer,
		server.WithBaseURL("http://localhost:8080"),
		server.WithSSEPattern("/:channel/sse"),
		server.WithSSEContextFunc(customContextFunc),
	)

	// Start server
	log.Printf("Server started on port :8080")
	log.Printf("Test URL: http://localhost:8080/test/sse")
	log.Printf("Test URL: http://localhost:8080/news/sse")

	if err := sseServer.Start(":8080"); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
