package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

func main() {
	mode := flag.String("mode", "stdio", "client mode (http, sse, stdio)")
	addr := flag.String("addr", "localhost:8080", "server address")
	serverCmd := flag.String("server-cmd", "", "command to start the server for stdio mode")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var trans transport.Interface
	var err error

	switch *mode {
	case "http":
		trans, err = transport.NewStreamableHTTP(fmt.Sprintf("http://%s/mcp", *addr), nil)
		if err != nil {
			log.Fatalf("Failed to create HTTP transport: %v", err)
		}
	case "sse":
		trans, err = transport.NewSSE(fmt.Sprintf("http://%s/sse", *addr))
		if err != nil {
			log.Fatalf("Failed to create SSE transport: %v", err)
		}
	case "stdio":
		if *serverCmd == "" {
			log.Fatal("server-cmd is required for stdio mode")
		}
		cmdParts := strings.Fields(*serverCmd)
		trans = transport.NewStdio(cmdParts[0], cmdParts[1:], "")
	default:
		log.Fatalf("Unknown mode: %s", *mode)
	}

	mcpClient := client.NewClient(trans)

	if err := mcpClient.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	defer mcpClient.Close()

	if _, err := mcpClient.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		log.Fatalf("Failed to initialize client: %v", err)
	}

	log.Println("Calling say_hi tool")
	result, err := mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "say_hi",
		},
	})
	if err != nil {
		log.Fatalf("Failed to call tool: %v", err)
	}

	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(mcp.TextContent); ok {
			fmt.Printf("Response from server: %s\n", textContent.Text)
		} else {
			fmt.Printf("Received non-text content: %+v\n", result.Content[0])
		}
	} else {
		fmt.Println("Received empty content from server")
	}
}
