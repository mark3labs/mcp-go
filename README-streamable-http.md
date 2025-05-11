# MCP Streamable HTTP Implementation

This is an implementation of the [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) Streamable HTTP transport for Go. It follows the [MCP Streamable HTTP transport specification](https://modelcontextprotocol.io/specification/2025-03-26/basic/transports).

## Features

- Implementation of the MCP Streamable HTTP transport specification
- Support for both client and server sides
- Session management with unique session IDs
- Support for SSE (Server-Sent Events) streaming
- Support for direct JSON responses
- Basic resumability with event IDs
- Support for notifications
- Support for session termination
- Origin header validation for security

## Current Limitations

- Limited batching support
- Basic resumability support (improved but not complete)
- No support for server -> client requests
- Limited support for continuously listening for server notifications

## Server Implementation

The server implementation is in `server/streamable_http.go`. It provides the Streamable HTTP transport for the server side.

### Key Components

- `StreamableHTTPServer`: The main server implementation that handles HTTP requests and responses
- `streamableHTTPSession`: Represents an active session with a client
- `EventStore`: Interface for storing and retrieving events for resumability
- `InMemoryEventStore`: A simple in-memory implementation of the EventStore interface

### Server Options

- `WithSessionIDGenerator`: Sets a custom session ID generator
- `WithStatelessMode`: Enables stateless mode (no sessions)
- `WithEnableJSONResponse`: Enables direct JSON responses instead of SSE streams
- `WithEventStore`: Sets a custom event store for resumability
- `WithStreamableHTTPContextFunc`: Sets a function to customize the context

## Client Implementation

The client implementation is in `client/transport/streamable_http.go`. It provides the Streamable HTTP transport for the client side.

### Client Options

- `WithHTTPHeaders`: Sets custom HTTP headers for all requests
- `WithHTTPTimeout`: Sets the timeout for HTTP requests and streams

## Usage

For complete examples, see:
- Server example: `examples/streamable_http_server/main.go`
- Client example: `examples/streamable_http_client/main.go`
- Complete client example: `examples/streamable_http_client_complete/main.go`

## Protocol Details

The Streamable HTTP transport follows the MCP Streamable HTTP transport specification:

1. **Session Management**: Sessions are created during initialization and maintained through a session ID header.
2. **SSE Streaming**: Server-Sent Events (SSE) are used for streaming responses and notifications.
3. **Direct JSON Responses**: For simple requests, direct JSON responses can be used instead of SSE.
4. **Resumability**: Events can be stored and replayed if a client reconnects with a Last-Event-ID header.
5. **Session Termination**: Sessions can be explicitly terminated with a DELETE request.
6. **Multiple Sessions**: The server supports multiple concurrent independent sessions.

## HTTP Methods

- **POST**: Used for sending JSON-RPC requests and notifications
- **GET**: Used for establishing a standalone SSE stream for receiving notifications
- **DELETE**: Used for terminating a session

## HTTP Headers

- **Mcp-Session-Id**: Used to identify a session
- **Accept**: Used to indicate support for SSE (`text/event-stream`)
- **Last-Event-Id**: Used for resumability
- **Origin**: Validated by the server for security

## Implementation Notes

- The server implementation supports both stateful and stateless modes.
- In stateful mode, a session ID is generated and maintained for each client.
- In stateless mode, no session ID is generated, and no session state is maintained.
- The client implementation supports reconnecting and resuming after disconnection.
- The server implementation supports multiple concurrent clients.
- Each client instance typically manages a single session at a time.
