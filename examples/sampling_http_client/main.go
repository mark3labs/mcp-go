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

// MockSamplingHandler implements client.SamplingHandler for demonstration.
// In a real implementation, this would integrate with an actual LLM API.
type MockSamplingHandler struct{}

func (h *MockSamplingHandler) CreateMessage(ctx context.Context, request mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	// Extract the user's message
	if len(request.Messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}

	// Get the last user message
	lastMessage := request.Messages[len(request.Messages)-1]
	userText := ""
	if textContent, ok := lastMessage.Content.(mcp.TextContent); ok {
		userText = textContent.Text
	}

	// Generate a mock response
	responseText := fmt.Sprintf("Mock LLM response to: '%s'", userText)

	log.Printf("Mock LLM generating response: %s", responseText)

	result := &mcp.CreateMessageResult{
		SamplingMessage: mcp.SamplingMessage{
			Role: mcp.RoleAssistant,
			Content: mcp.TextContent{
				Type: "text",
				Text: responseText,
			},
		},
		Model:      "mock-model-v1",
		StopReason: "endTurn",
	}

	return result, nil
}

func main() {
	// Create sampling handler
	samplingHandler := &MockSamplingHandler{}

	// Create HTTP transport directly
	httpTransport, err := transport.NewStreamableHTTP(
		"http://localhost:8080", // Replace with your MCP server URL
		// You can add HTTP-specific options here like headers, OAuth, etc.
	)
	if err != nil {
		log.Fatalf("Failed to create HTTP transport: %v", err)
	}
	defer httpTransport.Close()
	
	// Create client with sampling support
	mcpClient := client.NewClient(
		httpTransport,
		client.WithSamplingHandler(samplingHandler),
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
			Capabilities: mcp.ClientCapabilities{
				// Sampling capability will be automatically added by the client
			},
			ClientInfo: mcp.Implementation{
				Name:    "sampling-http-client",
				Version: "1.0.0",
			},
		},
	}
	
	_, err = mcpClient.Initialize(ctx, initRequest)
	if err != nil {
		log.Fatalf("Failed to initialize MCP session: %v", err)
	}

	log.Println("HTTP MCP client with sampling support started successfully!")
	log.Println("The client is now ready to handle sampling requests from the server.")
	log.Println("When the server sends a sampling request, the MockSamplingHandler will process it.")

	// In a real application, you would keep the client running to handle sampling requests
	// For this example, we'll just demonstrate that it's working
	
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