package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// MockRootsHandler implements client.RootsHandler for demonstration.
// In a real implementation, this would enumerate workspace/project roots.
type MockRootsHandler struct{}

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
				URI:  (&url.URL{Scheme: "file", Path: app}).String(),
			},
			{
				Name: "test-project",
				URI:  (&url.URL{Scheme: "file", Path: proj}).String(),
			},
		},
	}
	return result, nil
}

// main starts a mock MCP roots client over HTTP.
// The server tool triggers a roots/list request on the client.
// The client shuts down gracefully on SIGINT or SIGTERM.
func main() {
	// Create roots handler
	rootsHandler := &MockRootsHandler{}

	// Create HTTP transport directly
	httpTransport, err := transport.NewStreamableHTTP(
		"http://localhost:8080/mcp", // Replace with your MCP server URL
		transport.WithContinuousListening(),
	)
	if err != nil {
		log.Fatalf("Failed to create HTTP transport: %v", err)
	}
	defer httpTransport.Close()

	// Create client with roots support
	mcpClient := client.NewClient(
		httpTransport,
		client.WithRootsHandler(rootsHandler),
	)

	// Start the client
	ctx := context.Background()
	err = mcpClient.Start(ctx)
	if err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	defer func() {
		if cerr := mcpClient.Close(); cerr != nil {
			log.Printf("Error closing client: %v", cerr)
		}
	}()

	// Initialize the MCP session
	initRequest := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			Capabilities:    mcp.ClientCapabilities{
				// Roots capability will be automatically added by the client
			},
			ClientInfo: mcp.Implementation{
				Name:    "roots-http-client",
				Version: "1.0.0",
			},
		},
	}

	_, err = mcpClient.Initialize(ctx, initRequest)
	if err != nil {
		log.Fatalf("Failed to initialize MCP session: %v", err)
	}

	log.Println("HTTP MCP client with roots support started successfully!")
	log.Println("The client is now ready to handle roots requests from the server.")
	log.Println("When the server sends a roots request, the MockRootsHandler will process it.")

	// In a real application, you would keep the client running to handle roots requests
	// For this example, we'll just demonstrate that it's working

	// mock the root change
	if err := mcpClient.RootListChanges(ctx); err != nil {
		log.Printf("fail to notify root list change: %v", err)
	}

	// call server tool
	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "roots",
			Arguments: map[string]any{"testonly": "yes"},
		},
	}
	result, err := mcpClient.CallTool(ctx, request)
	if err != nil {
		log.Fatalf("failed to call tool roots: %v", err)
	} else if result.IsError {
		log.Printf("tool reported error")
	} else if len(result.Content) > 0 {
		resultStr := ""
		for _, content := range result.Content {
			if textContent, ok := content.(mcp.TextContent); ok {
				resultStr += fmt.Sprintf("%s\n", textContent.Text)
			}
		}
		fmt.Printf("client call tool result: %s\n", resultStr)
	}

	// Keep the client running (in a real app, you'd have your main application logic here)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	shutdownCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		<-sigChan
		log.Println("Received shutdown signal")
		cancel()
	}()
	<-shutdownCtx.Done()
}
