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

// MockSamplingHandler implements the SamplingHandler interface for demonstration.
// In a real implementation, this would integrate with an actual LLM API.
type MockSamplingHandler struct{}

func (h *MockSamplingHandler) CreateMessage(ctx context.Context, request mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	// Extract the user's message
	if len(request.Messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}

	userMessage := request.Messages[0]
	var userText string

	// Extract text from the content
	switch content := userMessage.Content.(type) {
	case mcp.TextContent:
		userText = content.Text
	case map[string]any:
		// Handle case where content is unmarshaled as a map
		if text, ok := content["text"].(string); ok {
			userText = text
		} else {
			userText = fmt.Sprintf("%v", content)
		}
	default:
		userText = fmt.Sprintf("%v", content)
	}

	// Simulate LLM processing
	log.Printf("Mock LLM received: %s", userText)
	log.Printf("System prompt: %s", request.SystemPrompt)
	log.Printf("Max tokens: %d", request.MaxTokens)
	log.Printf("Temperature: %f", request.Temperature)

	// Generate a mock response
	responseText := fmt.Sprintf("Mock LLM response to: '%s'. This is a simulated response from a mock LLM handler.", userText)

	log.Printf("Mock LLM generating response: %s", responseText)

	result := &mcp.CreateMessageResult{
		SamplingMessage: mcp.SamplingMessage{
			Role: mcp.RoleAssistant,
			Content: mcp.TextContent{
				Type: "text",
				Text: responseText,
			},
		},
		Model:      "mock-llm-v1",
		StopReason: "endTurn",
	}

	log.Printf("Mock LLM returning result: %+v", result)
	return result, nil
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: sampling_client <server_command>")
	}

	serverCommand := os.Args[1]
	serverArgs := os.Args[2:]

	// Create stdio transport to communicate with the server
	stdio := transport.NewStdio(serverCommand, nil, serverArgs...)

	// Create sampling handler
	samplingHandler := &MockSamplingHandler{}

	// Create client with sampling capability
	mcpClient := client.NewClient(stdio, client.WithSamplingHandler(samplingHandler))

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
				Name:    "sampling-example-client",
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

	// List available tools
	toolsResult, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		log.Fatalf("Failed to list tools: %v", err)
	}

	log.Printf("Available tools:")
	for _, tool := range toolsResult.Tools {
		log.Printf("  - %s: %s", tool.Name, tool.Description)
	}

	// Test the greeting tool first
	log.Println("\n--- Testing greet tool ---")
	greetResult, err := mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "greet",
			Arguments: map[string]any{
				"name": "Sampling Demo User",
			},
		},
	})
	if err != nil {
		log.Printf("Error calling greet tool: %v", err)
	} else {
		log.Printf("Greet result: %+v", greetResult)
		for _, content := range greetResult.Content {
			if textContent, ok := content.(mcp.TextContent); ok {
				log.Printf("  %s", textContent.Text)
			}
		}
	}

	// Test the sampling tool
	log.Println("\n--- Testing ask_llm tool (with sampling) ---")
	askResult, err := mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "ask_llm",
			Arguments: map[string]any{
				"question":      "What is the capital of France?",
				"system_prompt": "You are a helpful geography assistant.",
			},
		},
	})
	if err != nil {
		log.Printf("Error calling ask_llm tool: %v", err)
	} else {
		log.Printf("Ask LLM result: %+v", askResult)
		for _, content := range askResult.Content {
			if textContent, ok := content.(mcp.TextContent); ok {
				log.Printf("  %s", textContent.Text)
			}
		}
	}

	// Test another sampling request
	log.Println("\n--- Testing ask_llm tool with different question ---")
	askResult2, err := mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "ask_llm",
			Arguments: map[string]any{
				"question": "Explain quantum computing in simple terms.",
			},
		},
	})
	if err != nil {
		log.Printf("Error calling ask_llm tool: %v", err)
	} else {
		log.Printf("Ask LLM result 2: %+v", askResult2)
		for _, content := range askResult2.Content {
			if textContent, ok := content.(mcp.TextContent); ok {
				log.Printf("  %s", textContent.Text)
			}
		}
	}

	log.Println("\n--- Sampling demo completed ---")
}
