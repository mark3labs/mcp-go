package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	mode := flag.String("mode", "stdio", "server mode (http, sse, stdio)")
	addr := flag.String("addr", ":8080", "server address")
	flag.Parse()

	mcpServer := server.NewMCPServer("lys-simple-demo-server", "1.0.0")

	mcpServer.AddTool(mcp.Tool{
		Name:        "say_hi",
		Description: "Responds with a simple greeting",
		InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]any{}},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		log.Println("Executing say_hi tool")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{Type: "text", Text: "hi from server"},
			},
		}, nil
	})

	log.Printf("Starting server in %s mode on %s", *mode, *addr)

	switch *mode {
	case "http":
		log.Printf("Starting server in http mode on %s", *addr)
		httpServer := server.NewStreamableHTTPServer(mcpServer)
		if err := httpServer.Start(*addr); err != nil {
			log.Fatalf("Failed to start http server: %v", err)
		}
	case "sse":
		log.Printf("Starting server in sse mode on %s", *addr)
		sseServer := server.NewSSEServer(mcpServer, server.WithBaseURL("http://"+*addr))
		if err := sseServer.Start(*addr); err != nil {
			log.Fatalf("Failed to start sse server: %v", err)
		}
	case "stdio":
		log.Printf("Starting server in stdio mode")
		if err := server.ServeStdio(mcpServer); err != nil {
			log.Fatalf("Failed to start stdio server: %v", err)
		}
		return // Stdio mode exits when the stream closes
	default:
		log.Fatalf("Unknown mode: %s", *mode)
	}

	// For http and sse, wait for a signal to exit
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit
	log.Println("Shutting down server...")
}
