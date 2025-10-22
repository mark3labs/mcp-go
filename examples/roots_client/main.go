package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// MockRootsHandler implements client.RootsHandler for demonstration.
// In a real implementation, this would integrate with an actual LLM API.
type MockRootsHandler struct{}

func (h *MockRootsHandler) ListRoots(ctx context.Context, request mcp.ListRootsRequest) (*mcp.ListRootsResult, error) {
	result := &mcp.ListRootsResult{
		Roots: []mcp.Root{
			{
				Name: "app",
				URI:  "file:///User/haxxx/app",
			},
			{
				Name: "test-project",
				URI:  "file:///User/haxxx/projects/test-project",
			},
		},
	}
	return result, nil
}

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
				Name:    "roots-stdio-server",
				Version: "1.0.0",
			},
			Capabilities: mcp.ClientCapabilities{
				// Sampling capability will be automatically added by WithSamplingHandler
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
	request.Params.Arguments = "{\"testonly\": \"yes\"}"
	result, err := mcpClient.CallTool(ctx, request)
	if err != nil {
		log.Fatalf("failed to call tool roots: %v", err)
	} else if len(result.Content) > 0 {
		resultStr := ""
		for _, content := range result.Content {
			if textContent, ok := content.(mcp.TextContent); ok {
				resultStr += fmt.Sprintf("%s\n", textContent.Text)
			}
		}
		fmt.Printf("client call tool result: %s", resultStr)
	}

	// mock the root change
	if err := mcpClient.RootListChanges(ctx); err != nil {
		log.Printf("fail to notify root list change: %v", err)
	}
}
