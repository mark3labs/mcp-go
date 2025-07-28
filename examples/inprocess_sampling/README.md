# InProcess Sampling Example

This example demonstrates how to use sampling with in-process MCP client/server communication.

## Overview

The example shows:
- Creating an MCP server with sampling enabled
- Adding a tool that uses sampling to request LLM completions
- Creating an in-process client with a sampling handler
- Making tool calls that trigger sampling requests

## Key Components

### Server Side
- `mcpServer.EnableSampling()` - Enables sampling capability
- Tool handler calls `mcpServer.RequestSampling()` to request LLM completions
- Sampling requests are handled directly by the client's sampling handler

### Client Side
- `MockSamplingHandler` - Implements the `SamplingHandler` interface
- `NewInProcessClientWithSamplingHandler()` - Creates client with sampling support
- The handler receives sampling requests and returns mock LLM responses

## Running the Example

```bash
go run main.go
```

## Expected Output

```
Tool result: LLM Response (model: mock-llm-v1): Mock LLM response to: 'What is the capital of France?'
```

## Real LLM Integration

To integrate with a real LLM service (OpenAI, Anthropic, etc.), replace the `MockSamplingHandler` with an implementation that calls your preferred LLM API. See the [client sampling documentation](https://mcp-go.dev/clients/advanced-sampling) for examples with real LLM providers.