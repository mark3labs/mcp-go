# MCP Sampling Example Server

This example demonstrates how to implement an MCP server that uses sampling to request LLM completions from clients.

## Features

- **Sampling Support**: The server can request LLM completions from clients that support sampling
- **Tool Integration**: Shows how to use sampling within tool implementations
- **Bidirectional Communication**: Demonstrates server-to-client requests

## Tools

### `ask_llm`
Asks the LLM a question using sampling. This tool demonstrates how servers can leverage client-side LLM capabilities.

**Parameters:**
- `question` (required): The question to ask the LLM
- `system_prompt` (optional): System prompt to provide context

### `greet`
A simple greeting tool that doesn't use sampling, for comparison.

**Parameters:**
- `name` (required): Name of the person to greet

## Usage

Build and run the server:

```bash
go build -o sampling_server
./sampling_server
```

The server communicates via stdio and expects to be connected to an MCP client that supports sampling.

## Implementation Details

1. **Enable Sampling**: The server calls `mcpServer.EnableSampling()` to declare sampling capability
2. **Request Sampling**: Tools use `mcpServer.RequestSampling(ctx, request)` to send sampling requests to the client
3. **Handle Responses**: The server receives and processes the LLM responses from the client via bidirectional stdio communication
4. **Response Routing**: Incoming responses are automatically routed to the correct pending request using request IDs

## Testing

Use the companion `sampling_client` example to test this server:

```bash
cd ../sampling_client
go build -o sampling_client
./sampling_client ../sampling_server/sampling_server
```