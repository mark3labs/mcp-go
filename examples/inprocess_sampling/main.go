package main

import (
	"context"
	"fmt"
	"log"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// MockSamplingHandler implements client.SamplingHandler for demonstration
type MockSamplingHandler struct{}

func (h *MockSamplingHandler) CreateMessage(ctx context.Context, request mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	// Extract the user's message
	var userMessage string
	for _, msg := range request.Messages {
		if msg.Role == mcp.RoleUser {
			if textContent, ok := msg.Content.(mcp.TextContent); ok {
				userMessage = textContent.Text
				break
			}
		}
	}

	// Generate a mock response
	mockResponse := fmt.Sprintf("Mock LLM response to: '%s'", userMessage)

	return &mcp.CreateMessageResult{
		SamplingMessage: mcp.SamplingMessage{
			Role: mcp.RoleAssistant,
			Content: mcp.TextContent{
				Type: "text",
				Text: mockResponse,
			},
		},
		Model:      "mock-llm-v1",
		StopReason: "endTurn",
	}, nil
}

func main() {
	// Create server with sampling enabled
	mcpServer := server.NewMCPServer("inprocess-sampling-example", "1.0.0")
	mcpServer.EnableSampling()

	// Add a tool that uses sampling
	mcpServer.AddTool(mcp.Tool{
		Name:        "ask_llm",
		Description: "Ask the LLM a question using sampling",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"question": map[string]any{
					"type":        "string",
					"description": "The question to ask the LLM",
				},
				"system_prompt": map[string]any{
					"type":        "string",
					"description": "Optional system prompt",
				},
			},
			Required: []string{"question"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		question, err := request.RequireString("question")
		if err != nil {
			return nil, err
		}

		systemPrompt := request.GetString("system_prompt", "You are a helpful assistant.")

		// Create sampling request
		samplingRequest := mcp.CreateMessageRequest{
			CreateMessageParams: mcp.CreateMessageParams{
				Messages: []mcp.SamplingMessage{
					{
						Role: mcp.RoleUser,
						Content: mcp.TextContent{
							Type: "text",
							Text: question,
						},
					},
				},
				SystemPrompt: systemPrompt,
				MaxTokens:    1000,
				Temperature:  0.7,
			},
		}

		// Request sampling from client
		result, err := mcpServer.RequestSampling(ctx, samplingRequest)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: fmt.Sprintf("Error requesting sampling: %v", err),
					},
				},
				IsError: true,
			}, nil
		}

		// Return the LLM response
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("LLM Response (model: %s): %s",
						result.Model, result.Content.(mcp.TextContent).Text),
				},
			},
		}, nil
	})

	// Create client with sampling handler
	mockHandler := &MockSamplingHandler{}
	mcpClient, err := client.NewInProcessClientWithSamplingHandler(mcpServer, mockHandler)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer mcpClient.Close()

	// Start the client
	ctx := context.Background()
	if err := mcpClient.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}

	// Initialize
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "inprocess-sampling-client",
		Version: "1.0.0",
	}

	_, err = mcpClient.Initialize(ctx, initRequest)
	if err != nil {
		log.Fatalf("Failed to initialize: %v", err)
	}

	// Call the tool that uses sampling
	result, err := mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "ask_llm",
			Arguments: map[string]any{
				"question":      "What is the capital of France?",
				"system_prompt": "You are a helpful geography assistant.",
			},
		},
	})
	if err != nil {
		log.Fatalf("Tool call failed: %v", err)
	}

	// Print the result
	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(mcp.TextContent); ok {
			fmt.Printf("Tool result: %s\n", textContent.Text)
		}
	}
}
