package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
		fmt.Printf("Received notification: %s\n", notification.Method)
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

	// Wait for a moment
	fmt.Println("\nInitialization successful. Exiting...")
}
