package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// handleDummyTool is a simple tool that returns "foo bar"
func handleDummyTool(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText("foo bar"), nil
}

func NewMCPServer() *server.MCPServer {
	mcpServer := server.NewMCPServer(
		"example-server",
		"1.0.0",
		server.WithResourceCapabilities(true, true),
		server.WithPromptCapabilities(true),
		server.WithToolCapabilities(true),
	)
	mcpServer.AddTool(mcp.NewTool("dummy_tool",
		mcp.WithDescription("A dummy tool that returns foo bar"),
	), handleDummyTool)

	return mcpServer
}

type MCPClient struct {
	client     *client.Client
	serverInfo *mcp.InitializeResult
}

// NewMCPClient creates a new MCP client with an in-process MCP server.
func NewMCPClient(ctx context.Context) (*MCPClient, error) {
	srv := NewMCPServer()
	client, err := client.NewInProcessClient(srv)
	if err != nil {
		return nil, fmt.Errorf("failed to create in-process client: %w", err)
	}

	// Start the client with timeout context
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := client.Start(ctxWithTimeout); err != nil {
		return nil, fmt.Errorf("failed to start client: %w", err)
	}

	// Initialize the client
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "Example MCP Client",
		Version: "1.0.0",
	}
	initRequest.Params.Capabilities = mcp.ClientCapabilities{}

	serverInfo, err := client.Initialize(ctx, initRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MCP client: %w", err)
	}

	return &MCPClient{
		client:     client,
		serverInfo: serverInfo,
	}, nil
}

func main() {
	ctx := context.Background()
	client, err := NewMCPClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create MCP client: %v", err)
	}

	toolsRequest := mcp.ListToolsRequest{}
	toolsResult, err := client.client.ListTools(ctx, toolsRequest)
	if err != nil {
		log.Fatalf("Failed to list tools: %v", err)
	}
	fmt.Println(toolsResult.Tools)

	request := mcp.CallToolRequest{}
	request.Params.Name = "dummy_tool"

	result, err := client.client.CallTool(ctx, request)
	if err != nil {
		log.Fatalf("Failed to call tool: %v", err)
	}
	fmt.Println(result.Content)
}
