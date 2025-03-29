package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sashabaranov/go-openai"
)

// LLMConfig defines the configuration for an LLM that uses MCP prompts and tools
type LLMConfig struct {
	PromptName string   // Name of the prompt to use from MCP server
	ToolNames  []string // Names of tools to use from MCP server
	Model      string   // OpenAI model to use
}

// LLMClient handles the integration between OpenAI and MCP
type LLMClient struct {
	openaiClient *openai.Client
	mcpClient    *client.SSEMCPClient
	config       LLMConfig
	history      []openai.ChatCompletionMessage
}

// NewLLMClient creates a new LLM client with OpenAI and MCP integration
func NewLLMClient(openaiKey string, mcpURL string, config LLMConfig) (*LLMClient, error) {
	// Create OpenAI client
	openaiClient := openai.NewClient(openaiKey)

	// Create MCP client
	mcpClient, err := client.NewSSEMCPClient(mcpURL, client.WithAuthToken("your-secret-token-here"))
	if err != nil {
		return nil, fmt.Errorf("failed to create MCP client: %w", err)
	}

	return &LLMClient{
		openaiClient: openaiClient,
		mcpClient:    mcpClient,
		config:       config,
	}, nil
}

// Start initializes the MCP connection
func (c *LLMClient) Start(ctx context.Context) error {
	if err := c.mcpClient.Start(ctx); err != nil {
		return fmt.Errorf("failed to start MCP client: %w", err)
	}

	// Initialize the client
	initRequest := mcp.InitializeRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodInitialize),
		},
		Params: struct {
			ProtocolVersion string                 `json:"protocolVersion"`
			Capabilities    mcp.ClientCapabilities `json:"capabilities"`
			ClientInfo      mcp.Implementation     `json:"clientInfo"`
		}{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			Capabilities: mcp.ClientCapabilities{
				Sampling: &struct{}{},
			},
			ClientInfo: mcp.Implementation{
				Name:    "openai-integration",
				Version: "1.0.0",
			},
		},
	}

	_, err := c.mcpClient.Initialize(ctx, initRequest)
	if err != nil {
		return fmt.Errorf("failed to initialize MCP client: %w", err)
	}

	return nil
}

// Execute runs the LLM with the configured prompt and tools
func (c *LLMClient) Execute(ctx context.Context, args map[string]string) (string, error) {
	// 1. Get the prompt from MCP
	prompt, err := c.getPrompt(ctx, args)
	if err != nil {
		return "", fmt.Errorf("failed to get prompt: %w", err)
	}

	// 2. Get the tools from MCP
	tools, err := c.getTools(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get tools: %w", err)
	}

	// 3. Convert MCP tools to OpenAI function definitions
	openaiTools := c.convertToolsToOpenAI(tools)

	// 4. Create the chat completion request with history
	req := openai.ChatCompletionRequest{
		Model: c.config.Model,
		Messages: append(c.history, openai.ChatCompletionMessage{
			Role:    "user",
			Content: prompt,
		}),
		Tools:  openaiTools,
		Stream: true,
	}

	// 5. Create a channel for streaming responses
	stream, err := c.openaiClient.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to create stream: %w", err)
	}
	defer stream.Close()

	// 6. Process the stream
	var fullResponse strings.Builder
	var lastMessage openai.ChatCompletionMessage
	for {
		response, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("stream error: %w", err)
		}

		if len(response.Choices) > 0 {
			delta := response.Choices[0].Delta
			if delta.Content != "" {
				fmt.Print(delta.Content)
				fullResponse.WriteString(delta.Content)
			}
			if delta.Role != "" {
				lastMessage.Role = delta.Role
			}
		}
	}
	fmt.Println()

	// 7. Handle any tool calls from the last message
	if lastMessage.Role == "assistant" && lastMessage.ToolCalls != nil {
		// Execute each tool call
		for _, toolCall := range lastMessage.ToolCalls {
			result, err := c.executeTool(ctx, toolCall)
			if err != nil {
				return "", fmt.Errorf("failed to execute tool: %w", err)
			}
			// Add the tool result to the conversation
			toolMessage := openai.ChatCompletionMessage{
				Role:    "assistant",
				Content: fmt.Sprintf("Tool %s returned: %v", toolCall.Function.Name, result),
			}
			c.history = append(c.history, toolMessage)
		}
	}

	// 8. Update conversation history
	c.history = append(c.history,
		openai.ChatCompletionMessage{Role: "user", Content: prompt},
		openai.ChatCompletionMessage{Role: "assistant", Content: fullResponse.String()},
	)

	return fullResponse.String(), nil
}

// getPrompt retrieves and compiles the prompt from MCP
func (c *LLMClient) getPrompt(ctx context.Context, args map[string]string) (string, error) {
	request := mcp.GetPromptRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodPromptsGet),
		},
		Params: struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments,omitempty"`
		}{
			Name:      c.config.PromptName,
			Arguments: args,
		},
	}

	result, err := c.mcpClient.GetPrompt(ctx, request)
	if err != nil {
		return "", err
	}

	// Combine all messages into a single prompt
	var prompt string
	for _, msg := range result.Messages {
		prompt += fmt.Sprintf("%s: %v\n", msg.Role, msg.Content)
	}

	return prompt, nil
}

// getTools retrieves the tools from MCP
func (c *LLMClient) getTools(ctx context.Context) ([]mcp.Tool, error) {
	request := mcp.ListToolsRequest{}
	result, err := c.mcpClient.ListTools(ctx, request)
	if err != nil {
		return nil, err
	}

	// Filter tools based on configured names
	var tools []mcp.Tool
	for _, tool := range result.Tools {
		for _, name := range c.config.ToolNames {
			if tool.Name == name {
				tools = append(tools, tool)
				break
			}
		}
	}

	return tools, nil
}

// convertToolsToOpenAI converts MCP tools to OpenAI function definitions
func (c *LLMClient) convertToolsToOpenAI(tools []mcp.Tool) []openai.Tool {
	var openaiTools []openai.Tool
	for _, tool := range tools {
		openaiTool := openai.Tool{
			Type: "function",
			Function: &openai.FunctionDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		}
		openaiTools = append(openaiTools, openaiTool)
	}
	return openaiTools
}

// executeTool executes a tool call on the MCP server
func (c *LLMClient) executeTool(ctx context.Context, toolCall openai.ToolCall) (interface{}, error) {
	request := mcp.CallToolRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodToolsCall),
		},
		Params: struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments,omitempty"`
			Meta      *struct {
				ProgressToken mcp.ProgressToken `json:"progressToken,omitempty"`
			} `json:"_meta,omitempty"`
		}{
			Name: toolCall.Function.Name,
			Arguments: func() map[string]interface{} {
				var args map[string]interface{}
				if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
					return nil
				}
				return args
			}(),
		},
	}

	result, err := c.mcpClient.CallTool(ctx, request)
	if err != nil {
		return nil, err
	}

	return result.Content, nil
}

// ClearHistory clears the conversation history
func (c *LLMClient) ClearHistory() {
	c.history = nil
}

// GetHistory returns the current conversation history
func (c *LLMClient) GetHistory() []openai.ChatCompletionMessage {
	return c.history
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)

	// Get OpenAI API key
	openaiKey := os.Getenv("OPENAI_API_KEY")
	if openaiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Get MCP server URL
	mcpURL := os.Getenv("MCP_SERVER_URL")
	if mcpURL == "" {
		mcpURL = "http://localhost:8080/mcp/events"
	}

	// Create LLM configuration
	config := LLMConfig{
		PromptName: "code_review",
		ToolNames: []string{
			"echo",
			"add",
			"json_example",
		},
		Model: openai.GPT4TurboPreview,
	}

	// Create LLM client
	llmClient, err := NewLLMClient(openaiKey, mcpURL, config)
	if err != nil {
		log.Fatalf("Failed to create LLM client: %v", err)
	}

	// Start the client
	ctx := context.Background()
	if err := llmClient.Start(ctx); err != nil {
		log.Fatalf("Failed to start LLM client: %v", err)
	}

	// First code review
	fmt.Println("\n=== First Code Review ===")
	args := map[string]string{
		"language": "Go",
		"style":    "clean",
		"code": `func calculateTotal(items []Item) float64 {
    total := 0.0
    for i := 0; i < len(items); i++ {
        total += items[i].Price
    }
    return total
}`,
	}

	result, err := llmClient.Execute(ctx, args)
	if err != nil {
		log.Fatalf("Failed to execute LLM: %v", err)
	}
	fmt.Printf("LLM Result:\n%s\n", result)

	// Follow-up question
	fmt.Println("\n=== Follow-up Question ===")
	followUpArgs := map[string]string{
		"language": "Go",
		"style":    "clean",
		"code": `func calculateTotal(items []Item) float64 {
    return items.reduce((sum, item) => sum + item.Price, 0.0)
}`,
	}

	result, err = llmClient.Execute(ctx, followUpArgs)
	if err != nil {
		log.Fatalf("Failed to execute LLM: %v", err)
	}

	// Show conversation history
	fmt.Println("\n=== Conversation History ===")
	history := llmClient.GetHistory()
	for _, msg := range history {
		fmt.Printf("%s: %s\n", msg.Role, msg.Content)
	}

	// Clear history and start new conversation
	fmt.Println("\n=== New Conversation (Cleared History) ===")
	llmClient.ClearHistory()
	result, err = llmClient.Execute(ctx, args)
	if err != nil {
		log.Fatalf("Failed to execute LLM: %v", err)
	}
}
