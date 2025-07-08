package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	// Create a new MCP server
	mcpServer := server.NewMCPServer("sampling-example-server", "1.0.0")

	// Enable sampling capability
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
					"description": "Optional system prompt to provide context",
				},
			},
			Required: []string{"question"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Extract parameters using helper methods
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

		// Request sampling from the client
		samplingCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		serverFromCtx := server.ServerFromContext(ctx)
		result, err := serverFromCtx.RequestSampling(samplingCtx, samplingRequest)
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

		// Return the LLM's response
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("LLM Response (model: %s): %s", result.Model, getTextFromContent(result.Content)),
				},
			},
		}, nil
	})

	// Add a simple greeting tool
	mcpServer.AddTool(mcp.Tool{
		Name:        "greet",
		Description: "Greet the user",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Name of the person to greet",
				},
			},
			Required: []string{"name"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, err := request.RequireString("name")
		if err != nil {
			return nil, err
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Hello, %s! This server supports sampling - try using the ask_llm tool!", name),
				},
			},
		}, nil
	})

	// Start the stdio server
	log.Println("Starting sampling example server...")
	if err := server.ServeStdio(mcpServer); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// Helper function to extract text from content
func getTextFromContent(content interface{}) string {
	switch c := content.(type) {
	case mcp.TextContent:
		return c.Text
	case map[string]interface{}:
		// Handle JSON unmarshaled content
		if text, ok := c["text"].(string); ok {
			return text
		}
		return fmt.Sprintf("%v", content)
	case string:
		return c
	default:
		return fmt.Sprintf("%v", content)
	}
}
