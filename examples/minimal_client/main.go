package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
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

	// Call the echo tool
	fmt.Println("\nCalling echo tool...")
	echoRequest := transport.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name": "echo",
			"arguments": map[string]interface{}{
				"message": "Hello from minimal client!",
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

	fmt.Println("\nTest completed successfully!")
}
