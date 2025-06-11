package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

func main() {
	// Create a client with sampling handler
	mcpClient := client.NewStdioMCPClient(
		"go", []string{"run", "../server/main.go"}, "",
		// Enable sampling with a custom handler
		client.WithSamplingHandler(client.SamplingHandlerFunc(handleSampling)),
	)
	defer mcpClient.Close()

	ctx := context.Background()

	// Initialize the client
	log.Println("Initializing MCP client with sampling support...")
	initResult, err := mcpClient.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "sampling-client-example",
				Version: "1.0.0",
			},
			Capabilities: mcp.ClientCapabilities{
				Sampling: &struct{}{}, // Enable sampling capability
			},
		},
	})
	if err != nil {
		log.Fatalf("Failed to initialize client: %v", err)
	}

	log.Printf("Connected to server: %s v%s", initResult.ServerInfo.Name, initResult.ServerInfo.Version)
	log.Printf("Server capabilities: %+v", initResult.Capabilities)
	log.Printf("Server instructions: %s", initResult.Instructions)
	log.Println()

	// List available tools
	toolsResult, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		log.Fatalf("Failed to list tools: %v", err)
	}

	log.Println("Available tools:")
	for _, tool := range toolsResult.Tools {
		log.Printf("  - %s: %s", tool.Name, tool.Description)
	}
	log.Println()

	// Demonstrate text analysis
	log.Println("=== Demonstrating text analysis ===")
	analyzeResult, err := mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "analyze_text",
			Arguments: map[string]any{
				"text":          "I love this new product! It's amazing and works perfectly.",
				"analysis_type": "sentiment",
			},
		},
	})
	if err != nil {
		log.Printf("Text analysis failed: %v", err)
	} else {
		log.Printf("Result: %s", getToolResultText(analyzeResult))
	}
	log.Println()

	// Demonstrate chat completion
	log.Println("=== Demonstrating chat completion ===")
	chatResult, err := mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "chat_completion",
			Arguments: map[string]any{
				"user_message":   "What are the benefits of renewable energy?",
				"system_context": "You are an environmental science expert.",
			},
		},
	})
	if err != nil {
		log.Printf("Chat completion failed: %v", err)
	} else {
		log.Printf("Result: %s", getToolResultText(chatResult))
	}
	log.Println()

	// Demonstrate advanced sampling
	log.Println("=== Demonstrating advanced sampling ===")
	advancedResult, err := mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "advanced_sampling",
			Arguments: map[string]any{
				"prompt":      "Write a haiku about programming",
				"temperature": 0.8,
				"max_tokens":  100,
			},
		},
	})
	if err != nil {
		log.Printf("Advanced sampling failed: %v", err)
	} else {
		log.Printf("Result: %s", getToolResultText(advancedResult))
	}

	log.Println("\n=== Example completed ===")
}

// handleSampling processes sampling requests from the server
func handleSampling(ctx context.Context, req *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	log.Printf("Received sampling request with %d messages", len(req.Messages))

	// Log the sampling request details
	if req.SystemPrompt != nil {
		log.Printf("System prompt: %s", *req.SystemPrompt)
	}
	if req.Temperature != nil {
		log.Printf("Temperature: %.2f", *req.Temperature)
	}
	if req.MaxTokens != nil {
		log.Printf("Max tokens: %d", *req.MaxTokens)
	}

	// Extract the messages for processing
	var conversationText strings.Builder
	for i, msg := range req.Messages {
		if textContent, ok := mcp.AsTextContent(msg.Content); ok {
			if i > 0 {
				conversationText.WriteString("\n")
			}
			conversationText.WriteString(fmt.Sprintf("%s: %s", msg.Role, textContent.Text))
		}
	}

	log.Printf("Conversation: %s", conversationText.String())

	// Simulate LLM processing (in a real implementation, this would call an actual LLM)
	response := simulateLLMResponse(conversationText.String(), req)

	log.Printf("Generated response: %s", response)

	// Return the simulated response
	return &mcp.CreateMessageResult{
		Role:    mcp.RoleAssistant,
		Content: mcp.NewTextContent(response),
		Model:   "simulated-llm-v1",
	}, nil
}

// simulateLLMResponse simulates an LLM response based on the input
func simulateLLMResponse(conversation string, req *mcp.CreateMessageRequest) string {
	// Simple rule-based responses for demonstration
	lowerConv := strings.ToLower(conversation)

	switch {
	case strings.Contains(lowerConv, "sentiment") && strings.Contains(lowerConv, "love") && strings.Contains(lowerConv, "amazing"):
		return "This text expresses very positive sentiment. The words 'love' and 'amazing' indicate strong positive emotions and satisfaction with the product."

	case strings.Contains(lowerConv, "renewable energy"):
		return "Renewable energy offers several key benefits: 1) Environmental protection by reducing greenhouse gas emissions, 2) Energy independence and security, 3) Long-term cost savings, 4) Job creation in green industries, and 5) Sustainable development for future generations."

	case strings.Contains(lowerConv, "haiku") && strings.Contains(lowerConv, "programming"):
		return `Code flows like water,
Logic branches through the night—
Bugs become features.`

	case strings.Contains(lowerConv, "analyze"):
		return "Based on the analysis, this appears to be a request for text processing. The content shows structured communication patterns typical of technical documentation."

	case strings.Contains(lowerConv, "summary"):
		return "Summary: The provided text contains information that can be condensed into key points while maintaining the essential meaning and context."

	case strings.Contains(lowerConv, "keywords"):
		return "Key topics identified: text analysis, natural language processing, content extraction, semantic understanding, information retrieval."

	default:
		// Generic response
		return fmt.Sprintf("I understand your request about: %s. This is a simulated response from the example client's LLM handler.", 
			truncateString(conversation, 50))
	}
}

// Helper function to get text content from tool result
func getToolResultText(result *mcp.CallToolResult) string {
	for _, content := range result.Content {
		if textContent, ok := mcp.AsTextContent(content); ok {
			return textContent.Text
		}
	}
	return "No text content found"
}

// Helper function to truncate strings
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}