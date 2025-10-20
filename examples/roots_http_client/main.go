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

	// Keep the client running (in a real app, you'd have your main application logic here)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-ctx.Done():
		log.Println("Client context cancelled")
	case <-sigChan:
		log.Println("Received shutdown signal")
	}
}
