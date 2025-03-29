# MCP OpenAI Integration Example

This example demonstrates how to integrate the Model Context Protocol (MCP) with OpenAI's function calling capabilities. It shows how to:

1. Define an LLM configuration using MCP prompts and tools
2. Retrieve and compile prompts from an MCP server
3. Convert MCP tools to OpenAI function definitions
4. Execute tool calls through the MCP server
5. Handle the conversation flow between OpenAI and MCP

## Prerequisites

- Go 1.16 or later
- OpenAI API key
- MCP server running (see the template example)

## Installation

1. Clone the repository:
```bash
git clone https://github.com/mark3labs/mcp-go.git
cd mcp-go
```

2. Install dependencies:
```bash
go mod download
```

3. Set up environment variables:
```bash
export OPENAI_API_KEY="your-openai-api-key"
export MCP_SERVER_URL="http://localhost:8080/mcp/events"  # Adjust if your server is running elsewhere
```

## Running the Example

1. First, start the MCP server (from the template example):
```bash
go run examples/mcp_template/server/server.go
```

2. In a new terminal, run the OpenAI integration example:
```bash
go run examples/mcp_openai_integration/main.go
```

## How It Works

The example demonstrates a complete workflow:

1. **LLM Configuration**:
   - Define which MCP prompt to use (e.g., "code_review")
   - Specify which MCP tools to make available
   - Set the OpenAI model to use

2. **Prompt Retrieval**:
   - Fetch the prompt template from the MCP server
   - Compile the prompt with provided arguments
   - Format the prompt for OpenAI

3. **Tool Integration**:
   - Retrieve tool definitions from the MCP server
   - Convert MCP tools to OpenAI function definitions
   - Make tools available to the LLM

4. **Execution Flow**:
   - Send the prompt to OpenAI
   - Handle any tool calls from the LLM
   - Execute tools through the MCP server
   - Add tool results back to the conversation

## Example Usage

The example uses the code review prompt and several tools:

```go
config := LLMConfig{
    PromptName: "code_review",
    ToolNames: []string{
        "echo",
        "add",
        "json_example",
    },
    Model: openai.GPT4TurboPreview,
}
```

When executed, it will:
1. Connect to the MCP server
2. Retrieve the code review prompt
3. Get the specified tools
4. Send a request to OpenAI
5. Handle any tool calls
6. Return the final result

## Customization

You can customize the example by:

1. Using different prompts from your MCP server
2. Adding or removing tools
3. Modifying the prompt arguments
4. Changing the OpenAI model

## Error Handling

The example includes comprehensive error handling for:
- MCP connection issues
- Prompt retrieval failures
- Tool execution errors
- OpenAI API errors

## Next Steps

1. Add more sophisticated prompt templates
2. Implement streaming responses
3. Add conversation history management
4. Implement retry logic for failed tool calls
5. Add more complex tool interactions 