package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// fileURI returns a file:// URI for both Unix and Windows absolute paths.
func fileURI(p string) string {
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") { // e.g., "C:/Users/..." on Windows
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// MockRootsHandler implements client.RootsHandler for demonstration.
// In a real implementation, this would enumerate workspace/project roots.
type MockRootsHandler struct{}

// ListRoots implements client.RootsHandler by returning example workspace roots.
func (h *MockRootsHandler) ListRoots(ctx context.Context, request mcp.ListRootsRequest) (*mcp.ListRootsResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("Warning: failed to get home directory: %v", err)
		home = "/tmp" // fallback for demonstration
	}
	app := filepath.ToSlash(filepath.Join(home, "app"))
	proj := filepath.ToSlash(filepath.Join(home, "projects", "test-project"))
	result := &mcp.ListRootsResult{
		Roots: []mcp.Root{
			{
				Name: "app",
				URI:  fileURI(app),
			},
			{
				Name: "test-project",
				URI:  fileURI(proj),
			},
		},
	}
	return result, nil
}

// main starts a mock MCP roots client that communicates with a subprocess over stdio.
// It expects the server command as the first command-line argument, creates a stdio
// transport and an MCP client with a MockRootsHandler, starts and initializes the
// client, logs server info and available tools, notifies the server of root list
// changes, invokes the "roots" tool and prints any text content returned, and
// shuts down the client gracefully on SIGINT or SIGTERM.
func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: roots_client <server_command>")
	}

	serverCommand := os.Args[1]
	serverArgs := os.Args[2:]

	// Create stdio transport to communicate with the server
	stdio := transport.NewStdio(serverCommand, nil, serverArgs...)

	// Create roots handler
	rootsHandler := &MockRootsHandler{}

	// Create client with roots capability
	mcpClient := client.NewClient(stdio, client.WithRootsHandler(rootsHandler))

	ctx := context.Background()

	// Start the client
	if err := mcpClient.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create a context that cancels on signal
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		<-sigChan
		log.Println("Received shutdown signal, closing client...")
		cancel()
	}()

	// Move defer after error checking
	defer func() {
		if err := mcpClient.Close(); err != nil {
			log.Printf("Error closing client: %v", err)
		}
	}()

	// Initialize the connection
	initResult, err := mcpClient.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "roots-stdio-client",
				Version: "1.0.0",
			},
			Capabilities: mcp.ClientCapabilities{
				// Roots capability will be automatically added by WithRootsHandler
			},
		},
	})
	if err != nil {
		log.Fatalf("Failed to initialize: %v", err)
	}

	log.Printf("Connected to server: %s v%s", initResult.ServerInfo.Name, initResult.ServerInfo.Version)
	log.Printf("Server capabilities: %+v", initResult.Capabilities)

	// list tools
	toolsResult, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		log.Fatalf("Failed to list tools: %v", err)
	}
	log.Printf("Available tools:")
	for _, tool := range toolsResult.Tools {
		log.Printf("  - %s: %s", tool.Name, tool.Description)
	}

	// call server tool
	request := mcp.CallToolRequest{}
	request.Params.Name = "roots"
	request.Params.Arguments = map[string]any{"testonly": "yes"}
	result, err := mcpClient.CallTool(ctx, request)
	if err != nil {
		log.Fatalf("failed to call tool roots: %v", err)
	} else if result.IsError {
		log.Printf("tool reported error")
	} else if len(result.Content) > 0 {
		resultStr := ""
		for _, content := range result.Content {
			switch tc := content.(type) {
			case mcp.TextContent:
				resultStr += fmt.Sprintf("%s\n", tc.Text)
			}
		}
		fmt.Printf("client call tool result: %s\n", resultStr)
	}

	// mock the root change
	if err := mcpClient.RootListChanges(ctx); err != nil {
		log.Printf("failed to notify root list change: %v", err)
	}

	// Keep running until cancelled by signal
	<-ctx.Done()
	log.Println("Client context cancelled")
}
