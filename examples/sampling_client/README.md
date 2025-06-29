# MCP Sampling Example Client

This example demonstrates how to implement an MCP client that supports sampling requests from servers.

## Features

- **Sampling Handler**: Implements the `SamplingHandler` interface to process sampling requests
- **Mock LLM**: Provides a mock LLM implementation for demonstration purposes
- **Capability Declaration**: Automatically declares sampling capability when a handler is configured
- **Bidirectional Communication**: Handles incoming requests from the server

## Mock LLM Handler

The `MockSamplingHandler` simulates an LLM by:
- Logging the received request parameters
- Generating a mock response that echoes the input
- Returning proper MCP sampling response format

In a real implementation, you would:
- Integrate with actual LLM APIs (OpenAI, Anthropic, etc.)
- Implement proper model selection based on preferences
- Add human-in-the-loop approval mechanisms
- Handle rate limiting and error cases

## Usage

Build the client:

```bash
go build -o sampling_client
```

Run with the sampling server:

```bash
./sampling_client ../sampling_server/sampling_server
```

Or with any other MCP server that supports sampling:

```bash
./sampling_client /path/to/your/mcp/server
```

## Implementation Details

1. **Sampling Handler**: Implements `client.SamplingHandler` interface
2. **Client Configuration**: Uses `client.WithSamplingHandler()` to enable sampling
3. **Automatic Capability**: Sampling capability is automatically declared during initialization
4. **Request Processing**: Handles incoming `sampling/createMessage` requests from servers

## Sample Output

```
Connected to server: sampling-example-server v1.0.0
Available tools:
  - ask_llm: Ask the LLM a question using sampling
  - greet: Greet the user

--- Testing greet tool ---
Greet result: Hello, Sampling Demo User! This server supports sampling - try using the ask_llm tool!

--- Testing ask_llm tool (with sampling) ---
Mock LLM received: What is the capital of France?
System prompt: You are a helpful geography assistant.
Max tokens: 1000
Temperature: 0.700000
Ask LLM result: LLM Response (model: mock-llm-v1): Mock LLM response to: 'What is the capital of France?'. This is a simulated response from a mock LLM handler.
```

## Real LLM Integration

To integrate with a real LLM, replace the `MockSamplingHandler` with an implementation that:

```go
type RealSamplingHandler struct {
    apiKey string
    client *openai.Client // or other LLM client
}

func (h *RealSamplingHandler) CreateMessage(ctx context.Context, request mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
    // Convert MCP request to LLM API format
    // Call LLM API
    // Convert response back to MCP format
    // Return result
}
```