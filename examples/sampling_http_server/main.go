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
	// Create MCP server with sampling capability
	mcpServer := server.NewMCPServer("sampling-http-server", "1.0.0")

	// Enable sampling capability
	mcpServer.EnableSampling()

	// Add a tool that uses sampling to get LLM responses
	mcpServer.AddTool(mcp.Tool{
		Name:        "ask_llm",
		Description: "Ask the LLM a question using sampling over HTTP",
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
		// Extract parameters
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

		// Request sampling from the client with timeout
		samplingCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
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

		// Extract response text safely
		var responseText string
		if textContent, ok := result.Content.(mcp.TextContent); ok {
			responseText = textContent.Text
		} else {
			responseText = fmt.Sprintf("%v", result.Content)
		}

		// Return the LLM response
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("LLM Response (model: %s): %s", result.Model, responseText),
				},
			},
		}, nil
	})

	// Add a simple echo tool for testing
	mcpServer.AddTool(mcp.Tool{
		Name:        "echo",
		Description: "Echo back the input message",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"message": map[string]any{
					"type":        "string",
					"description": "The message to echo back",
				},
			},
			Required: []string{"message"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		message := request.GetString("message", "")

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Echo: %s", message),
				},
			},
		}, nil
	})

	// Create HTTP server
	httpServer := server.NewStreamableHTTPServer(mcpServer)

	log.Println("Starting HTTP MCP server with sampling support on :8080")
	log.Println("Endpoint: http://localhost:8080/mcp")
	log.Println("")
	log.Println("This server supports sampling over HTTP transport.")
	log.Println("Clients must:")
	log.Println("1. Initialize with sampling capability")
	log.Println("2. Establish SSE connection for bidirectional communication")
	log.Println("3. Handle incoming sampling requests from the server")
	log.Println("4. Send responses back via HTTP POST")
	log.Println("")
	log.Println("Available tools:")
	log.Println("- ask_llm: Ask the LLM a question (requires sampling)")
	log.Println("- echo: Simple echo tool (no sampling required)")

	// Start the server
	if err := httpServer.Start(":8080"); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}