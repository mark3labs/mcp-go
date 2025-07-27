# HTTP Sampling Client Example

This example demonstrates how to create an MCP client using HTTP transport that supports sampling requests from the server.

## Overview

This client:
- Connects to an MCP server via HTTP/HTTPS transport
- Declares sampling capability during initialization
- Handles incoming sampling requests from the server
- Uses a mock LLM to generate responses (replace with real LLM integration)

## Usage

1. Start an MCP server that supports sampling (e.g., using the `sampling_server` example)

2. Update the server URL in `main.go`:
   ```go
   httpClient, err := client.NewStreamableHttpClient(
       "http://your-server:port", // Replace with your server URL
   )
   ```

3. Run the client:
   ```bash
   go run main.go
   ```

## Key Features

### HTTP Transport with Sampling
The client creates the HTTP transport directly and then wraps it with a client that supports sampling:

```go
httpTransport, err := transport.NewStreamableHTTP("http://localhost:8080")
mcpClient := client.NewClient(httpTransport, client.WithSamplingHandler(samplingHandler))
```

### Sampling Handler
The `MockSamplingHandler` implements the `client.SamplingHandler` interface:

```go
type MockSamplingHandler struct{}

func (h *MockSamplingHandler) CreateMessage(ctx context.Context, request mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
    // Process the sampling request and return LLM response
    // In production, integrate with OpenAI, Anthropic, or other LLM APIs
}
```

### Client Configuration
The client is configured with sampling capabilities:

```go
mcpClient := client.NewClient(
    httpTransport,
    client.WithSamplingHandler(samplingHandler),
)
// Sampling capability is automatically declared when a handler is provided
```

## Real Implementation

For a production implementation, replace the `MockSamplingHandler` with a real LLM client:

```go
type RealSamplingHandler struct {
    client *openai.Client // or other LLM client
}

func (h *RealSamplingHandler) CreateMessage(ctx context.Context, request mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
    // Convert MCP request to LLM API format
    // Call LLM API
    // Convert response back to MCP format
    // Return the result
}
```

## HTTP-Specific Features

The HTTP transport supports:
- Standard HTTP headers for authentication and customization
- OAuth 2.0 authentication (using `WithHTTPOAuth`)
- Custom headers (using `WithHTTPHeaders`)
- Server-side events (SSE) for bidirectional communication
- Proper error handling with HTTP status codes
- Session management via HTTP headers

## Testing

The implementation includes comprehensive tests in `client/transport/streamable_http_sampling_test.go` that verify:
- Sampling request handling
- Error scenarios
- Bidirectional interface compliance
- HTTP-specific error codes and responses