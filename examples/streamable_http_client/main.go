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
	// Create a new Streamable HTTP transport
	trans, err := transport.NewStreamableHTTP("http://localhost:8080/mcp")
	if err != nil {
		fmt.Printf("Failed to create transport: %v\n", err)
		os.Exit(1)
	}
	defer trans.Close()

	// Set up notification handler
	trans.SetNotificationHandler(func(notification mcp.JSONRPCNotification) {
		fmt.Printf("Received notification: %s\n", notification.Method)
		params, _ := json.MarshalIndent(notification.Params, "", "  ")
		fmt.Printf("Params: %s\n", params)
	})

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	// Call the echo tool
	fmt.Println("\nCalling echo tool...")
	echoRequest := transport.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
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
