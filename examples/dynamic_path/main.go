package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	var addr string
	flag.StringVar(&addr, "addr", ":8080", "address to listen on")
	flag.Parse()

	mcpServer := server.NewMCPServer("dynamic-path-example", "1.0.0")

	// Add a trivial tool for demonstration
	mcpServer.AddTool(mcp.NewTool("echo"), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText(fmt.Sprintf("Echo: %v", req.GetArguments()["message"])), nil
	})

	// Use a dynamic base path based on a path parameter (Go 1.22+)
	sseServer := server.NewSSEServer(
		mcpServer,
		server.WithDynamicBasePath(func(r *http.Request, sessionID string) string {
			tenant := r.PathValue("tenant")
			return "/api/" + tenant
		}),
		server.WithBaseURL(fmt.Sprintf("http://localhost%s", addr)),
		server.WithUseFullURLForMessageEndpoint(true),
	)

	mux := http.NewServeMux()
	mux.Handle("/api/{tenant}/sse", sseServer.SSEHandler())
	mux.Handle("/api/{tenant}/message", sseServer.MessageHandler())

	log.Printf("Dynamic SSE server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
