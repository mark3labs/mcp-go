package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// SSE URL provided by the user
	sseURL := "https://api.qnaigc.com/v1/mcp/sse/a8b8d1b46ebf48cd862f98ceb9fd2c66"

	log.Printf("Connecting to remote SSE server at: %s", sseURL)

	// Create a new SSE transport
	headers := map[string]string{
		"Authorization": "Bearer sk-8dcfdc26e7303a2561a2a852cb1c0ae2ca46eeea51f0458b40d580a913d4b4d3",
	}
	trans, err := transport.NewSSE(sseURL, transport.WithHeaders(headers))
	if err != nil {
		log.Fatalf("Failed to create SSE transport: %v", err)
	}

	// Create a new MCP client
	mcpClient := client.NewClient(trans)

	if err := mcpClient.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	defer mcpClient.Close()

	if _, err := mcpClient.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		log.Fatalf("Failed to initialize client: %v", err)
	}

	log.Println("Listing available tools...")
	tools, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		log.Fatalf("Failed to list tools: %v", err)
	}

	fmt.Println("Available tools:")
	for _, tool := range tools.Tools {
		fmt.Printf("- %s\n", tool.Name)
	}

	log.Println("\nCalling 'hello' tool with name 'mcp-go'...")
	hellorResult, err := mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "hello",
			Arguments: map[string]any{
				"name": "mcp-go",
			},
		},
	})
	if err != nil {
		log.Fatalf("Failed to call 'hello' tool: %v", err)
	}

	fmt.Println("\nResult from 'hello' tool:")
	for _, content := range hellorResult.Content {
		if textContent, ok := content.(mcp.TextContent); ok {
			fmt.Println(textContent.Text)
		} else {
			fmt.Printf("Received non-text content: %+v\n", content)
		}
	}
}
