# MCP Sampling Examples

This directory contains examples demonstrating the MCP sampling functionality, which allows MCP servers to request LLM completions from clients.

## Overview

MCP sampling enables sophisticated agentic workflows where servers can request LLM assistance while maintaining security and user control. The client has full discretion over which model to use and can implement human-in-the-loop approval processes.

## Examples

### Server Example (`server/main.go`)

Demonstrates how to create an MCP server that uses sampling capabilities:

- **Text Analysis Tool**: Analyzes text sentiment, summary, or keywords using LLM sampling
- **Chat Completion Tool**: Shows multi-message conversation sampling
- **Advanced Sampling Tool**: Demonstrates advanced sampling options and parameters

Key features shown:
- Enabling sampling capability with `server.WithSampling()`
- Using `server.SamplingContextFromContext(ctx)` to access sampling in tools
- Various sampling options: temperature, max tokens, system prompts, model preferences
- Error handling for sampling failures

### Client Example (`client/main.go`)

Demonstrates how to create an MCP client that handles sampling requests:

- **Sampling Handler**: Implements a custom sampling handler that simulates LLM responses
- **Capability Negotiation**: Shows how to enable sampling capability on the client
- **Request Processing**: Demonstrates handling different types of sampling requests

Key features shown:
- Enabling sampling with `client.WithSamplingHandler()`
- Implementing the `SamplingHandler` interface
- Processing sampling requests with context and parameters
- Returning properly formatted sampling results

## Running the Examples

### Prerequisites

- Go 1.23 or later
- This repository cloned locally

### Running the Complete Example

The client example automatically starts the server example as a subprocess:

```bash
cd examples/sampling/client
go run main.go
```

This will:
1. Start the server example as a subprocess
2. Connect to it via stdio
3. Demonstrate various sampling capabilities
4. Show the interaction between server sampling requests and client responses

### Running Server Only

To run just the server (for testing with other clients):

```bash
cd examples/sampling/server
go run main.go
```

The server will run on stdio and can be connected to by any MCP client that supports sampling.

## Sample Output

When running the client example, you'll see output like:

```
Initializing MCP client with sampling support...
Connected to server: sampling-example-server v1.0.0
Server capabilities: {Sampling:0xc0000b4060 ...}

Available tools:
  - analyze_text: Analyzes text using LLM sampling
  - chat_completion: Demonstrates multi-message sampling with conversation context
  - advanced_sampling: Demonstrates advanced sampling options and parameters

=== Demonstrating text analysis ===
Received sampling request with 1 messages
System prompt: You are a helpful text analysis assistant. Provide clear, concise responses.
Temperature: 0.30
Max tokens: 200
Conversation: user: Analyze the sentiment of this text and provide a brief explanation: I love this new product! It's amazing and works perfectly.
Generated response: This text expresses very positive sentiment. The words 'love' and 'amazing' indicate strong positive emotions and satisfaction with the product.
Result: Analysis (sentiment): This text expresses very positive sentiment. The words 'love' and 'amazing' indicate strong positive emotions and satisfaction with the product.

=== Demonstrating chat completion ===
Received sampling request with 2 messages
Temperature: 0.70
Max tokens: 300
Conversation: system: You are an environmental science expert.
user: What are the benefits of renewable energy?
Generated response: Renewable energy offers several key benefits: 1) Environmental protection by reducing greenhouse gas emissions, 2) Energy independence and security, 3) Long-term cost savings, 4) Job creation in green industries, and 5) Sustainable development for future generations.
Result: Assistant: Renewable energy offers several key benefits: 1) Environmental protection by reducing greenhouse gas emissions, 2) Energy independence and security, 3) Long-term cost savings, 4) Job creation in green industries, and 5) Sustainable development for future generations.

Model used: simulated-llm-v1
```

## Implementation Notes

### Server-Side Sampling

1. **Enable Capability**: Use `server.WithSampling()` when creating the server
2. **Access Context**: Use `server.SamplingContextFromContext(ctx)` in tool handlers
3. **Make Requests**: Use the `Sample()` method with various input types and options
4. **Handle Errors**: Check for sampling availability and handle failures gracefully

### Client-Side Sampling

1. **Enable Capability**: Use `client.WithSamplingHandler()` when creating the client
2. **Implement Handler**: Create a function that implements the `SamplingHandler` interface
3. **Process Requests**: Handle incoming sampling requests and return appropriate responses
4. **Model Selection**: Choose appropriate models based on the request parameters

### Security Considerations

- Clients should implement human-in-the-loop approval for sampling requests
- Servers should handle sampling failures gracefully
- Sensitive information should not be included in sampling requests without user consent
- Clients have full control over which models to use and can reject requests

## Real-World Usage

In production environments:

1. **Server Tools** would use sampling for tasks like:
   - Code analysis and suggestions
   - Document summarization
   - Natural language queries
   - Content generation

2. **Client Handlers** would:
   - Connect to real LLM APIs (OpenAI, Anthropic, etc.)
   - Implement user approval workflows
   - Apply content filtering and safety measures
   - Log and audit sampling requests

3. **Transport Security** would include:
   - TLS encryption for network transports
   - Authentication and authorization
   - Rate limiting and quota management
   - Request validation and sanitization

## Next Steps

To extend these examples:

1. **Add Real LLM Integration**: Replace the simulated responses with actual LLM API calls
2. **Implement User Approval**: Add interactive prompts for user consent
3. **Add Content Filtering**: Implement safety measures and content validation
4. **Enhance Error Handling**: Add retry logic and better error messages
5. **Add Metrics**: Implement logging and monitoring for sampling requests