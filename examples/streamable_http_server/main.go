package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	// Create a new MCP server
	mcpServer := server.NewMCPServer("example-server", "1.0.0",
		server.WithResourceCapabilities(true, true),
		server.WithPromptCapabilities(true),
		server.WithToolCapabilities(true),
		server.WithLogging(),
		server.WithInstructions("This is an example Streamable HTTP server."),
	)

	// Add a simple echo tool
	mcpServer.AddTool(
		mcp.Tool{
			Name:        "echo",
			Description: "Echoes back the input",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"message": map[string]interface{}{
						"type":        "string",
						"description": "The message to echo",
					},
				},
				Required: []string{"message"},
			},
		},
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// Extract the message from the request
			message, ok := request.Params.Arguments["message"].(string)
			if !ok {
				return nil, fmt.Errorf("message must be a string")
			}

			// Create the result
			result := &mcp.CallToolResult{
				Result: mcp.Result{},
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: fmt.Sprintf("Message: %s\nTimestamp: %s", message, time.Now().Format(time.RFC3339)),
					},
				},
			}

			// Send a notification after a short delay
			go func() {
				time.Sleep(1 * time.Second)
				mcpServer.SendNotificationToClient(ctx, "echo/notification", map[string]interface{}{
					"message": "Echo notification: " + message,
				})
			}()

			return result, nil
		},
	)

	// Create a new Streamable HTTP server
	streamableServer := server.NewStreamableHTTPServer(mcpServer,
		server.WithEnableJSONResponse(true), // Use direct JSON responses for simplicity
	)

	// Start the server in a goroutine
	go func() {
		log.Println("Starting Streamable HTTP server on :8080...")
		if err := streamableServer.Start(":8080"); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := streamableServer.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}
	log.Println("Server exited properly")
}
