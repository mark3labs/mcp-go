package main

import (
	"context"
	"fmt"
	"log"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/examples/local/server"
	"github.com/mark3labs/mcp-go/examples/local/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

func NewLocalClient(ctx context.Context) *client.Client {
	localServer := server.CreateLocalServer(ctx)
	localTransport := transport.NewLocalTransport(localServer)
	client := client.NewClient(localTransport)

	_ = client.Start(ctx)

	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "example-client",
		Version: "1.0.0",
	}
	_, _ = client.Initialize(ctx, initRequest)

	return client
}

func main() {
	ctx := context.Background()
	localClient := NewLocalClient(ctx)
	tools, err := localClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		log.Fatalf("Failed to list tools: %v", err)
		return
	}
	for _, tool := range tools.Tools {
		fmt.Printf("%s: %s\n", tool.Name, tool.Description)
	}

	for _, tool := range tools.Tools {
		toolsCallRequest := mcp.CallToolRequest{}
		toolsCallRequest.Params.Name = tool.Name
		callToolResp, err := localClient.CallTool(ctx, toolsCallRequest)
		if err != nil {
			log.Fatalf("Failed to call tool: %v", err)
			return
		}
		fmt.Printf("Call tool [%s] result: %+v\n", tool.Name, callToolResp)
	}
}
