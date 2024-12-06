package main

import (
	"fmt"
	"log"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type ToolName string

const (
	ECHO                   ToolName = "echo"
	ADD                    ToolName = "add"
	LONG_RUNNING_OPERATION ToolName = "longRunningOperation"
	SAMPLE_LLM             ToolName = "sampleLLM"
	GET_TINY_IMAGE         ToolName = "getTinyImage"
)

type PromptName string

const (
	SIMPLE  PromptName = "simple_prompt"
	COMPLEX PromptName = "complex_prompt"
)

type MCPServer struct {
	server        *server.MCPServer
	subscriptions map[string]bool
	updateTicker  *time.Ticker
	allResources  []mcp.Resource
}

func NewMCPServer() *MCPServer {
	s := &MCPServer{
		server: server.NewMCPServer(
			"example-servers/everything",
			"1.0.0",
			server.WithResourceCapabilities(true, true),
			server.WithPromptCapabilities(true),
			server.WithToolCapabilities(true),
			server.WithLogging(),
		),
		subscriptions: make(map[string]bool),
		updateTicker:  time.NewTicker(5 * time.Second),
		allResources:  generateResources(),
	}

	s.server.AddResource("test://static/resource/", s.handleReadResource)
	s.server.AddResourceTemplate(
		"test://static/resource/{id}",
		s.handleResourceTemplate,
	)
	s.server.AddPrompt(mcp.Prompt{
		Name:        string(SIMPLE),
		Description: "A simple prompt",
	}, s.handleSimplePrompt)
	s.server.AddPrompt(mcp.Prompt{
		Name:        string(COMPLEX),
		Description: "A complex prompt",
	}, s.handleComplexPrompt)
	s.server.AddTool(mcp.Tool{
		Name:        string(ECHO),
		Description: "Echoes back the input",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Message to echo",
				},
			},
		},
	}, s.handleEchoTool)
	s.server.AddTool(mcp.Tool{
		Name:        string(ADD),
		Description: "Adds two numbers",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"a": map[string]interface{}{
					"type":        "number",
					"description": "First number",
				},
				"b": map[string]interface{}{
					"type":        "number",
					"description": "Second number",
				},
			},
		},
	}, s.handleAddTool)
	s.server.AddTool(mcp.Tool{
		Name:        string(LONG_RUNNING_OPERATION),
		Description: "Demonstrates a long running operation with progress updates",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"duration": map[string]interface{}{
					"type":        "number",
					"description": "Duration of the operation in seconds",
					"default":     10,
				},
				"steps": map[string]interface{}{
					"type":        "number",
					"description": "Number of steps in the operation",
					"default":     5,
				},
			},
		},
	}, s.handleLongRunningOperationTool)
	// s.server.AddTool(mcp.Tool{
	// 	Name:        string(SAMPLE_LLM),
	// 	Description: "Samples from an LLM using MCP's sampling feature",
	// 	InputSchema: mcp.ToolInputSchema{
	// 		Type: "object",
	// 		Properties: map[string]interface{}{
	// 			"prompt": map[string]interface{}{
	// 				"type":        "string",
	// 				"description": "The prompt to send to the LLM",
	// 			},
	// 			"maxTokens": map[string]interface{}{
	// 				"type":        "number",
	// 				"description": "Maximum number of tokens to generate",
	// 				"default":     100,
	// 			},
	// 		},
	// 	},
	// }, s.handleSampleLLMTool)
	s.server.AddTool(mcp.Tool{
		Name:        string(GET_TINY_IMAGE),
		Description: "Returns the MCP_TINY_IMAGE",
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: map[string]interface{}{},
		},
	}, s.handleGetTinyImageTool)

	s.server.AddNotificationHandler(s.handleNotification)

	go s.runUpdateInterval()

	return s
}

func generateResources() []mcp.Resource {
	resources := make([]mcp.Resource, 100)
	for i := 0; i < 100; i++ {
		uri := fmt.Sprintf("test://static/resource/%d", i+1)
		if i%2 == 0 {
			resources[i] = mcp.Resource{
				URI:      uri,
				Name:     fmt.Sprintf("Resource %d", i+1),
				MIMEType: "text/plain",
			}
		} else {
			resources[i] = mcp.Resource{
				URI:      uri,
				Name:     fmt.Sprintf("Resource %d", i+1),
				MIMEType: "application/octet-stream",
			}
		}
	}
	return resources
}

func (s *MCPServer) runUpdateInterval() {
	// for range s.updateTicker.C {
	// 	for uri := range s.subscriptions {
	// 		s.server.HandleMessage(
	// 			context.Background(),
	// 			mcp.JSONRPCNotification{
	// 				JSONRPC: mcp.JSONRPC_VERSION,
	// 				Notification: mcp.Notification{
	// 					Method: "resources/updated",
	// 					Params: struct {
	// 						Meta map[string]interface{} `json:"_meta,omitempty"`
	// 					}{
	// 						Meta: map[string]interface{}{"uri": uri},
	// 					},
	// 				},
	// 			},
	// 		)
	// 	}
	// }
}

func (s *MCPServer) handleReadResource() ([]interface{}, error) {
	return []interface{}{
		mcp.TextResourceContents{
			ResourceContents: mcp.ResourceContents{
				URI:      "test://static/resource/1",
				MIMEType: "text/plain",
			},
			Text: "This is a sample resource",
		},
	}, nil
}

func (s *MCPServer) handleResourceTemplate() (mcp.ResourceTemplate, error) {
	return mcp.ResourceTemplate{
		URITemplate: "test://static/resource/{id}",
		Name:        "Static Resource",
		Description: "A static resource with a numeric ID",
	}, nil
}

func (s *MCPServer) handleSimplePrompt(
	arguments map[string]string,
) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{
		Description: "A simple prompt without arguments",
		Messages: []mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: "This is a simple prompt without arguments.",
				},
			},
		},
	}, nil
}

func (s *MCPServer) handleComplexPrompt(
	arguments map[string]string,
) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{
		Description: "A complex prompt with arguments",
		Messages: []mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf(
						"This is a complex prompt with arguments: temperature=%s, style=%s",
						arguments["temperature"],
						arguments["style"],
					),
				},
			},
			{
				Role: mcp.RoleAssistant,
				Content: mcp.TextContent{
					Type: "text",
					Text: "I understand. You've provided a complex prompt with temperature and style arguments. How would you like me to proceed?",
				},
			},
			{
				Role: mcp.RoleUser,
				Content: mcp.ImageContent{
					Type:     "image",
					Data:     MCP_TINY_IMAGE,
					MIMEType: "image/png",
				},
			},
		},
	}, nil
}

func (s *MCPServer) handleEchoTool(
	arguments map[string]interface{},
) (*mcp.CallToolResult, error) {
	message, ok := arguments["message"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid message argument")
	}
	return &mcp.CallToolResult{
		Content: []interface{}{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Echo: %s", message),
			},
		},
	}, nil
}

func (s *MCPServer) handleAddTool(
	arguments map[string]interface{},
) (*mcp.CallToolResult, error) {
	a, ok1 := arguments["a"].(float64)
	b, ok2 := arguments["b"].(float64)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("invalid number arguments")
	}
	sum := a + b
	return &mcp.CallToolResult{
		Content: []interface{}{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("The sum of %f and %f is %f.", a, b, sum),
			},
		},
	}, nil
}

func (s *MCPServer) handleLongRunningOperationTool(
	arguments map[string]interface{},
) (*mcp.CallToolResult, error) {
	duration, _ := arguments["duration"].(float64)
	steps, _ := arguments["steps"].(float64)
	stepDuration := duration / steps
	progressToken, _ := arguments["_meta"].(map[string]interface{})["progressToken"].(mcp.ProgressToken)

	for i := 1; i < int(steps)+1; i++ {
		time.Sleep(time.Duration(stepDuration * float64(time.Second)))
		if progressToken != nil {
			// 	s.server.HandleMessage(
			// 		context.Background(),
			// 		mcp.JSONRPCNotification{
			// 			JSONRPC: mcp.JSONRPC_VERSION,
			// 			Notification: mcp.Notification{
			// 				Method: "progress",
			// 				Params: struct {
			// 					Meta map[string]interface{} `json:"_meta,omitempty"`
			// 				}{
			// 					Meta: map[string]interface{}{
			// 						"progress":      i,
			// 						"total":         int(steps),
			// 						"progressToken": progressToken,
			// 					},
			// 				},
			// 			},
			// 		},
			// 	)
		}
	}

	return &mcp.CallToolResult{
		Content: []interface{}{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf(
					"Long running operation completed. Duration: %f seconds, Steps: %d.",
					duration,
					int(steps),
				),
			},
		},
	}, nil
}

// func (s *MCPServer) handleSampleLLMTool(arguments map[string]interface{}) (*mcp.CallToolResult, error) {
// 	prompt, _ := arguments["prompt"].(string)
// 	maxTokens, _ := arguments["maxTokens"].(float64)

// 	// This is a mock implementation. In a real scenario, you would use the server's RequestSampling method.
// 	result := fmt.Sprintf(
// 		"Sample LLM result for prompt: '%s' (max tokens: %d)",
// 		prompt,
// 		int(maxTokens),
// 	)

// 	return &mcp.CallToolResult{
// 		Content: []interface{}{
// 			mcp.TextContent{
// 				Type: "text",
// 				Text: fmt.Sprintf("LLM sampling result: %s", result),
// 			},
// 		},
// 	}, nil
// }

func (s *MCPServer) handleGetTinyImageTool(
	arguments map[string]interface{},
) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{
		Content: []interface{}{
			mcp.TextContent{
				Type: "text",
				Text: "This is a tiny image:",
			},
			mcp.ImageContent{
				Type:     "image",
				Data:     MCP_TINY_IMAGE,
				MIMEType: "image/png",
			},
			mcp.TextContent{
				Type: "text",
				Text: "The image above is the MCP tiny image.",
			},
		},
	}, nil
}

func (s *MCPServer) handleNotification(notification mcp.JSONRPCNotification) {
	log.Printf("Received notification: %s", notification.Method)
}

func (s *MCPServer) Serve() error {
	return server.ServeStdio(s.server)
}

func main() {
	server := NewMCPServer()
	if err := server.Serve(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

const MCP_TINY_IMAGE = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAACklEQVR4nGMAAQAABQABDQottAAAAABJRU5ErkJggg=="
