package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

func main() {
	// Create a new Streamable HTTP transport with a longer timeout
	trans, err := transport.NewStreamableHTTP("http://localhost:8080/mcp",
		transport.WithHTTPTimeout(30*time.Second))
	if err != nil {
		fmt.Printf("Failed to create transport: %v\n", err)
		os.Exit(1)
	}
	defer trans.Close()

	// Set up notification handler
	trans.SetNotificationHandler(func(notification mcp.JSONRPCNotification) {
		fmt.Printf("\nReceived notification: %s\n", notification.Method)
		params, _ := json.MarshalIndent(notification.Params, "", "  ")
		fmt.Printf("Params: %s\n", params)
	})

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Initialize the connection
	fmt.Println("Initializing connection...")
	initRequest := transport.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
	}

	initResponse, err := trans.SendRequest(ctx, initRequest)
	if err != nil {
		fmt.Printf("Failed to initialize: %v\n", err)
		os.Exit(1)
	}

	// Print the initialization response
	initResponseJSON, _ := json.MarshalIndent(initResponse, "", "  ")
	fmt.Printf("Initialization response: %s\n", initResponseJSON)
	fmt.Printf("Session ID: %s\n", trans.GetSessionId())

	// List available tools
	fmt.Println("\nListing available tools...")
	listToolsRequest := transport.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/list",
	}

	listToolsResponse, err := trans.SendRequest(ctx, listToolsRequest)
	if err != nil {
		fmt.Printf("Failed to list tools: %v\n", err)
		os.Exit(1)
	}

	// Print the tools list response
	toolsResponseJSON, _ := json.MarshalIndent(listToolsResponse, "", "  ")
	fmt.Printf("Tools list response: %s\n", toolsResponseJSON)

	// Extract tool information
	var toolsResult struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(listToolsResponse.Result, &toolsResult); err != nil {
		fmt.Printf("Failed to parse tools list: %v\n", err)
	} else {
		fmt.Println("\nAvailable tools:")
		for _, tool := range toolsResult.Result.Tools {
			fmt.Printf("- %s: %s\n", tool.Name, tool.Description)
		}
	}

	// Call the echo tool
	fmt.Println("\nCalling echo tool...")
	echoRequest := transport.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name": "echo",
			"arguments": map[string]interface{}{
				"message": "Hello from Streamable HTTP client!",
			},
		},
	}

	echoResponse, err := trans.SendRequest(ctx, echoRequest)
	if err != nil {
		fmt.Printf("Failed to call echo tool: %v\n", err)
		os.Exit(1)
	}

	// Print the echo response
	echoResponseJSON, _ := json.MarshalIndent(echoResponse, "", "  ")
	fmt.Printf("Echo response: %s\n", echoResponseJSON)

	// Wait for notifications (the echo tool sends a notification after 1 second)
	fmt.Println("\nWaiting for notifications...")
	fmt.Println("(The server should send a notification about 1 second after the tool call)")

	// Set up a signal channel to handle Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for either a signal or a timeout
	select {
	case <-sigChan:
		fmt.Println("Received interrupt signal, exiting...")
	case <-time.After(5 * time.Second):
		fmt.Println("Timeout reached, exiting...")
	}
}
