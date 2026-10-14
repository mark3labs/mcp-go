package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

func main() {
	c, err := client.NewStdioMCPClient(
		"npx",
		[]string{}, // Empty ENV
		"-y",
		"@modelcontextprotocol/server-filesystem",
		"/tmp",
	)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Initialize the client
	fmt.Println("Initializing client...")
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "example-client",
		Version: "1.0.0",
	}

	initResult, err := c.Initialize(ctx, initRequest)
	if err != nil {
		log.Fatalf("Failed to initialize: %v", err)
	}
	fmt.Printf(
		"Initialized with server: %s %s\n\n",
		initResult.ServerInfo.Name,
		initResult.ServerInfo.Version,
	)

	// List Tools
	fmt.Println("Listing available tools...")
	toolsRequest := mcp.ListToolsRequest{}
	tools, err := c.ListTools(ctx, toolsRequest)
	if err != nil {
		log.Fatalf("Failed to list tools: %v", err)
	}
	for _, tool := range tools.Tools {
		fmt.Printf("- %s: %s\n", tool.Name, tool.Description)
	}
	fmt.Println()

	// List allowed directories
	fmt.Println("Listing allowed directories...")
	listDirRequest := mcp.CallToolRequest{
		Request: mcp.Request{
			Method: "tools/call",
		},
	}
	listDirRequest.Params.Name = "list_allowed_directories"

	result, err := c.CallTool(ctx, listDirRequest)
	if err != nil {
		log.Fatalf("Failed to list allowed directories: %v", err)
	}
	printToolResult(result)
	fmt.Println()

	// List /tmp
	fmt.Println("Listing /tmp directory...")
	listTmpRequest := mcp.CallToolRequest{}
	listTmpRequest.Params.Name = "list_directory"
	listTmpRequest.Params.Arguments = map[string]any{
		"path": "/tmp",
	}

	result, err = c.CallTool(ctx, listTmpRequest)
	if err != nil {
		log.Fatalf("Failed to list directory: %v", err)
	}
	printToolResult(result)
	fmt.Println()

	// Create mcp directory
	fmt.Println("Creating /tmp/mcp directory...")
	createDirRequest := mcp.CallToolRequest{}
	createDirRequest.Params.Name = "create_directory"
	createDirRequest.Params.Arguments = map[string]any{
		"path": "/tmp/mcp",
	}

	result, err = c.CallTool(ctx, createDirRequest)
	if err != nil {
		log.Fatalf("Failed to create directory: %v", err)
	}
	printToolResult(result)
	fmt.Println()

	// Create hello.txt
	fmt.Println("Creating /tmp/mcp/hello.txt...")
	writeFileRequest := mcp.CallToolRequest{}
	writeFileRequest.Params.Name = "write_file"
	writeFileRequest.Params.Arguments = map[string]any{
		"path":    "/tmp/mcp/hello.txt",
		"content": "Hello World",
	}

	result, err = c.CallTool(ctx, writeFileRequest)
	if err != nil {
		log.Fatalf("Failed to create file: %v", err)
	}
	printToolResult(result)
	fmt.Println()

	// Verify file contents
	fmt.Println("Reading /tmp/mcp/hello.txt...")
	readFileRequest := mcp.CallToolRequest{}
	readFileRequest.Params.Name = "read_file"
	readFileRequest.Params.Arguments = map[string]any{
		"path": "/tmp/mcp/hello.txt",
	}

	result, err = c.CallTool(ctx, readFileRequest)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}
	printToolResult(result)

	// Get file info
	fmt.Println("Getting info for /tmp/mcp/hello.txt...")
	fileInfoRequest := mcp.CallToolRequest{}
	fileInfoRequest.Params.Name = "get_file_info"
	fileInfoRequest.Params.Arguments = map[string]any{
		"path": "/tmp/mcp/hello.txt",
	}

	result, err = c.CallTool(ctx, fileInfoRequest)
	if err != nil {
		log.Fatalf("Failed to get file info: %v", err)
	}
	printToolResult(result)
}

// Helper function to print tool results
func printToolResult(result *mcp.CallToolResult) {
	for _, content := range result.Content {
		if textContent, ok := content.(mcp.TextContent); ok {
			fmt.Println(textContent.Text)
		} else {
			jsonBytes, _ := json.MarshalIndent(content, "", "  ")
			fmt.Println(string(jsonBytes))
		}
	}
}
