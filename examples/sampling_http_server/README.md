# HTTP Sampling Server Example

This example demonstrates how to create an MCP server using HTTP transport that can send sampling requests to clients.

## Overview

This server:
- Runs on HTTP transport (port 8080 by default)
- Declares sampling capability during initialization  
- Can send sampling requests to connected clients via Server-Sent Events (SSE)
- Receives sampling responses from clients via HTTP POST
- Includes tools that demonstrate sampling functionality

## Usage

1. Start the server:
   ```bash
   go run main.go
   ```

2. The server will be available at: `http://localhost:8080/mcp`

3. Connect with an HTTP client that supports sampling (like the `sampling_http_client` example)

## Tools Available

### `ask_llm`
Demonstrates server-initiated sampling:
- Takes a question and optional system prompt
- Sends sampling request to client
- Returns the LLM's response

### `echo` 
Simple tool for testing basic functionality:
- Echoes back the input message
- Doesn't require sampling

## How Sampling Works

### Server → Client Flow
1. **Tool Invocation**: Client calls `ask_llm` tool
2. **Sampling Request**: Server creates sampling request with user's question
3. **SSE Transmission**: Server sends JSON-RPC request to client via SSE stream
4. **Client Processing**: Client's sampling handler processes the request
5. **HTTP Response**: Client sends JSON-RPC response back via HTTP POST
6. **Tool Response**: Server returns the LLM response to the original tool caller

### Communication Architecture
```
Client (HTTP + SSE) ←→ Server (HTTP)
     │                       │
     ├─ POST: Tool Call ──→  │
     │                       │
     │  ←── SSE: Sampling ───┤ 
     │      Request          │
     │                       │
     ├─ POST: Sampling ───→  │
     │      Response         │
     │                       │
     │  ←── HTTP: Tool ──────┤
           Response
```

## Key Features

### Bidirectional Communication
- **SSE Stream**: Server → Client requests (sampling, notifications)
- **HTTP POST**: Client → Server responses and requests

### Session Management
- Session ID tracking for request/response correlation
- Proper session lifecycle management
- Session validation for security

### Error Handling
- JSON-RPC error codes for different failure scenarios
- Timeout handling for sampling requests
- Queue overflow protection

### HTTP-Specific Features
- Standard MCP headers (`Mcp-Session-Id`, `Mcp-Protocol-Version`)
- Content-Type validation
- Proper HTTP status codes
- SSE event formatting

## Testing

You can test the server using the `sampling_http_client` example:

1. Start this server:
   ```bash
   go run examples/sampling_http_server/main.go
   ```

2. In another terminal, start the client:
   ```bash
   go run examples/sampling_http_client/main.go
   ```

3. The client will connect and be ready to handle sampling requests from the server.

## Production Considerations

### Security
- Implement proper authentication/authorization
- Use HTTPS in production
- Validate all incoming data
- Implement rate limiting

### Scalability
- Consider connection pooling for multiple clients
- Implement proper session cleanup
- Monitor memory usage for long-running sessions
- Add metrics and monitoring

### Reliability
- Implement request retries
- Add circuit breakers for failing clients
- Implement graceful degradation when sampling is unavailable
- Add comprehensive logging

## Integration

This server can be integrated into existing HTTP infrastructure:

```go
// Custom HTTP server integration
mux := http.NewServeMux()
mux.Handle("/mcp", httpServer)
mux.Handle("/health", healthHandler)

server := &http.Server{
    Addr:    ":8080",
    Handler: mux,
}
```

The sampling functionality works seamlessly with other MCP features like tools, resources, and prompts.