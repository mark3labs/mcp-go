package main

import (
	"context"
	"fmt"
	"log"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	// Create server with sampling capability
	mcpServer := server.NewMCPServer(
		"sampling-example-server",
		"1.0.0",
		server.WithSampling(), // Enable sampling capability
		server.WithInstructions("This server demonstrates MCP sampling capabilities. It provides tools that can request LLM completions from clients."),
	)

	// Add a tool that uses sampling to analyze text
	mcpServer.AddTool(
		mcp.NewTool(
			"analyze_text",
			mcp.WithDescription("Analyzes text using LLM sampling"),
			mcp.WithString("text", mcp.Required(), mcp.Description("Text to analyze")),
			mcp.WithString("analysis_type", mcp.Description("Type of analysis: sentiment, summary, or keywords"), mcp.Default("sentiment")),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			text := request.GetString("text", "")
			analysisType := request.GetString("analysis_type", "sentiment")

			// Get sampling context from the request context
			samplingCtx := server.SamplingContextFromContext(ctx)
			if samplingCtx == nil {
				return mcp.NewToolResultError("Sampling not available - client does not support sampling"), nil
			}

			// Create appropriate prompt based on analysis type
			var prompt string
			switch analysisType {
			case "sentiment":
				prompt = fmt.Sprintf("Analyze the sentiment of this text and provide a brief explanation: %s", text)
			case "summary":
				prompt = fmt.Sprintf("Provide a concise summary of this text: %s", text)
			case "keywords":
				prompt = fmt.Sprintf("Extract the key topics and keywords from this text: %s", text)
			default:
				return mcp.NewToolResultError("Invalid analysis type. Use: sentiment, summary, or keywords"), nil
			}

			// Request analysis from LLM via client
			result, err := samplingCtx.Sample(ctx,
				server.StringInput(prompt),
				server.WithTemperature(0.3),
				server.WithMaxTokens(200),
				server.WithSystemPrompt("You are a helpful text analysis assistant. Provide clear, concise responses."),
			)
			if err != nil {
				return mcp.NewToolResultError("Sampling failed: " + err.Error()), nil
			}

			return mcp.NewToolResultText(fmt.Sprintf("Analysis (%s): %s", analysisType, result.Text())), nil
		},
	)

	// Add a tool that demonstrates conversation sampling
	mcpServer.AddTool(
		mcp.NewTool(
			"chat_completion",
			mcp.WithDescription("Demonstrates multi-message sampling with conversation context"),
			mcp.WithString("user_message", mcp.Required(), mcp.Description("User's message")),
			mcp.WithString("system_context", mcp.Description("System context for the conversation"), mcp.Default("You are a helpful assistant.")),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			userMessage := request.GetString("user_message", "")
			systemContext := request.GetString("system_context", "You are a helpful assistant.")

			samplingCtx := server.SamplingContextFromContext(ctx)
			if samplingCtx == nil {
				return mcp.NewToolResultError("Sampling not available"), nil
			}

			// Create a conversation with system and user messages
			messages := []mcp.SamplingMessage{
				{
					Role:    mcp.RoleSystem,
					Content: mcp.NewTextContent(systemContext),
				},
				{
					Role:    mcp.RoleUser,
					Content: mcp.NewTextContent(userMessage),
				},
			}

			result, err := samplingCtx.Sample(ctx,
				server.MessagesInput(messages),
				server.WithTemperature(0.7),
				server.WithMaxTokens(300),
				server.WithModel("gpt-4", "claude-3"), // Prefer GPT-4, fallback to Claude-3
			)
			if err != nil {
				return mcp.NewToolResultError("Chat completion failed: " + err.Error()), nil
			}

			return mcp.NewToolResultText(fmt.Sprintf("Assistant: %s\n\nModel used: %s", result.Text(), result.Model())), nil
		},
	)

	// Add a tool that demonstrates advanced sampling options
	mcpServer.AddTool(
		mcp.NewTool(
			"advanced_sampling",
			mcp.WithDescription("Demonstrates advanced sampling options and parameters"),
			mcp.WithString("prompt", mcp.Required(), mcp.Description("The prompt to send")),
			mcp.WithNumber("temperature", mcp.Description("Sampling temperature (0.0-2.0)"), mcp.Default(1.0)),
			mcp.WithInteger("max_tokens", mcp.Description("Maximum tokens to generate"), mcp.Default(150)),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			prompt := request.GetString("prompt", "")
			temperature := request.GetFloat64("temperature", 1.0)
			maxTokens := request.GetInt("max_tokens", 150)

			samplingCtx := server.SamplingContextFromContext(ctx)
			if samplingCtx == nil {
				return mcp.NewToolResultError("Sampling not available"), nil
			}

			// Use advanced sampling options
			result, err := samplingCtx.Sample(ctx,
				server.StringInput(prompt),
				server.WithTemperature(temperature),
				server.WithMaxTokens(maxTokens),
				server.WithContext(mcp.ContextThisServer), // Include context from this server
				server.WithStopSequences("END", "STOP"),   // Stop on these sequences
			)
			if err != nil {
				return mcp.NewToolResultError("Advanced sampling failed: " + err.Error()), nil
			}

			// Return detailed information about the sampling result
			stopReason := result.StopReason()
			if stopReason == "" {
				stopReason = "completed"
			}

			return mcp.NewToolResultText(fmt.Sprintf(
				"Response: %s\n\nModel: %s\nStop Reason: %s\nParameters: temp=%.1f, max_tokens=%d",
				result.Text(),
				result.Model(),
				stopReason,
				temperature,
				maxTokens,
			)), nil
		},
	)

	// Start the server on stdio
	log.Println("Starting MCP sampling example server...")
	log.Println("This server demonstrates sampling capabilities.")
	log.Println("Tools available:")
	log.Println("  - analyze_text: Analyze text sentiment, summary, or keywords")
	log.Println("  - chat_completion: Multi-message conversation sampling")
	log.Println("  - advanced_sampling: Advanced sampling with custom parameters")
	log.Println()

	server.ServeStdio(mcpServer)
}