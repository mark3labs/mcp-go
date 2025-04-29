# MCP Streamable HTTP Implementation

This is an implementation of the [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) Streamable HTTP transport for Go. It follows the [MCP Streamable HTTP transport specification](https://modelcontextprotocol.io/specification/2025-03-26/basic/transports).

## Features

- Full implementation of the MCP Streamable HTTP transport specification
- Support for both client and server sides
- Session management with unique session IDs
- Support for SSE (Server-Sent Events) streaming
- Support for direct JSON responses
- Support for resumability with event IDs
- Support for notifications
- Support for session termination

## Server Implementation

The server implementation is in `server/streamable_http.go`. It provides a complete implementation of the Streamable HTTP transport for the server side.

### Key Components

- `StreamableHTTPServer`: The main server implementation that handles HTTP requests and responses
- `streamableHTTPSession`: Represents an active session with a client
- `EventStore`: Interface for storing and retrieving events for resumability
- `InMemoryEventStore`: A simple in-memory implementation of the EventStore interface

### Server Options

- `WithSessionIDGenerator`: Sets a custom session ID generator
- `WithEnableJSONResponse`: Enables direct JSON responses instead of SSE streams
- `WithEventStore`: Sets a custom event store for resumability
- `WithStreamableHTTPContextFunc`: Sets a function to customize the context

## Client Implementation

The client implementation is in `client/transport/streamable_http.go`. It provides a complete implementation of the Streamable HTTP transport for the client side.

## Usage

### Server Example

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	// Create a new MCP server
	mcpServer := server.NewMCPServer("example-server", "1.0.0",
		server.WithResourceCapabilities(true, true),
		server.WithPromptCapabilities(true),
		server.WithToolCapabilities(true),
		server.WithLogging(),
		server.WithInstructions("This is an example Streamable HTTP server."),
	)

	// Add a simple echo tool
	mcpServer.AddTool(
		mcp.Tool{
			Name:        "echo",
			Description: "Echoes back the input",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"message": map[string]interface{}{
						"type":        "string",
						"description": "The message to echo",
					},
				},
				Required: []string{"message"},
			},
		},
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// Extract the message from the request
			message, ok := request.Params.Arguments["message"].(string)
			if !ok {
				return nil, fmt.Errorf("message must be a string")
			}

			// Create the result
			result := &mcp.CallToolResult{
				Result: mcp.Result{},
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: fmt.Sprintf("Message: %s\nTimestamp: %s", message, time.Now().Format(time.RFC3339)),
					},
				},
			}

			// Send a notification after a short delay
			go func() {
				time.Sleep(1 * time.Second)
				mcpServer.SendNotificationToClient(ctx, "echo/notification", map[string]interface{}{
					"message": "Echo notification: " + message,
				})
			}()

			return result, nil
		},
	)

	// Create a new Streamable HTTP server
	streamableServer := server.NewStreamableHTTPServer(mcpServer,
		server.WithEnableJSONResponse(false), // Use SSE streaming by default
	)

	// Start the server in a goroutine
	go func() {
		log.Println("Starting Streamable HTTP server on :8080...")
		if err := streamableServer.Start(":8080"); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := streamableServer.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}
	log.Println("Server exited properly")
}
```

### Client Example

```go
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

	// Call the echo tool
	fmt.Println("\nCalling echo tool...")
	fmt.Println("Using session ID from initialization...")
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
```

## Running the Examples

1. Start the server:

```bash
go run examples/streamable_http_server/main.go
```

2. In another terminal, run the client:

```bash
go run examples/streamable_http_client/main.go
```

## Protocol Details

The Streamable HTTP transport follows the MCP Streamable HTTP transport specification. Key aspects include:

1. **Session Management**: Sessions are created during initialization and maintained through a session ID header.
2. **SSE Streaming**: Server-Sent Events (SSE) are used for streaming responses and notifications.
3. **Direct JSON Responses**: For simple requests, direct JSON responses can be used instead of SSE.
4. **Resumability**: Events can be stored and replayed if a client reconnects with a Last-Event-ID header.
5. **Session Termination**: Sessions can be explicitly terminated with a DELETE request.

## HTTP Methods

- **POST**: Used for sending JSON-RPC requests and notifications
- **GET**: Used for establishing a standalone SSE stream for receiving notifications
- **DELETE**: Used for terminating a session

## HTTP Headers

- **Mcp-Session-Id**: Used to identify a session
- **Accept**: Used to indicate support for SSE (`text/event-stream`)
- **Last-Event-Id**: Used for resumability

## Implementation Notes

- The server implementation supports both stateful and stateless modes.
- In stateful mode, a session ID is generated and maintained for each client.
- In stateless mode, no session ID is generated, and no session state is maintained.
- The client implementation supports reconnecting and resuming after disconnection.
- The server implementation supports multiple concurrent clients.
