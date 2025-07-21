package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync/atomic"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// demoElicitationHandler demonstrates how to use elicitation in a tool
func demoElicitationHandler(s *server.MCPServer) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Create an elicitation request to get project details
		elicitationRequest := mcp.ElicitationRequest{
			Params: mcp.ElicitationParams{
				Message: "I need some information to set up your project. Please provide the project details.",
				RequestedSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"projectName": map[string]interface{}{
							"type":        "string",
							"description": "Name of the project",
							"minLength":   1,
						},
						"framework": map[string]interface{}{
							"type":        "string",
							"description": "Frontend framework to use",
							"enum":        []string{"react", "vue", "angular", "none"},
						},
						"includeTests": map[string]interface{}{
							"type":        "boolean",
							"description": "Include test setup",
							"default":     true,
						},
					},
					"required": []string{"projectName"},
				},
			},
		}

		// Request elicitation from the client
		result, err := s.RequestElicitation(ctx, elicitationRequest)
		if err != nil {
			return nil, fmt.Errorf("failed to request elicitation: %w", err)
		}

		// Handle the user's response
		switch result.Response.Type {
		case mcp.ElicitationResponseTypeAccept:
			// User provided the information
			data, ok := result.Response.Value.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("unexpected response format")
			}

			projectName := data["projectName"].(string)
			framework := "none"
			if fw, ok := data["framework"].(string); ok {
				framework = fw
			}
			includeTests := true
			if tests, ok := data["includeTests"].(bool); ok {
				includeTests = tests
			}

			// Create project based on user input
			message := fmt.Sprintf(
				"Created project '%s' with framework: %s, tests: %v",
				projectName, framework, includeTests,
			)

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(message),
				},
			}, nil

		case mcp.ElicitationResponseTypeDecline:
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent("Project creation cancelled - user declined to provide information"),
				},
			}, nil

		case mcp.ElicitationResponseTypeCancel:
			return nil, fmt.Errorf("project creation cancelled by user")

		default:
			return nil, fmt.Errorf("unexpected response type: %s", result.Response.Type)
		}
	}
}

var requestCount atomic.Int32

func main() {
	// Create server with elicitation capability
	mcpServer := server.NewMCPServer(
		"elicitation-demo-server",
		"1.0.0",
		server.WithElicitation(), // Enable elicitation
	)

	// Add a tool that uses elicitation
	mcpServer.AddTool(
		mcp.NewTool(
			"create_project",
			mcp.WithDescription("Creates a new project with user-specified configuration"),
		),
		demoElicitationHandler(mcpServer),
	)

	// Add another tool that demonstrates conditional elicitation
	mcpServer.AddTool(
		mcp.NewTool(
			"process_data",
			mcp.WithDescription("Processes data with optional user confirmation"),
			mcp.WithString("data", mcp.Required(), mcp.Description("Data to process")),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			data := request.GetArguments()["data"].(string)

			// Only request elicitation if data seems sensitive
			if len(data) > 100 {
				elicitationRequest := mcp.ElicitationRequest{
					Params: mcp.ElicitationParams{
						Message: fmt.Sprintf("The data is %d characters long. Do you want to proceed with processing?", len(data)),
						RequestedSchema: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"proceed": map[string]interface{}{
									"type":        "boolean",
									"description": "Whether to proceed with processing",
								},
								"reason": map[string]interface{}{
									"type":        "string",
									"description": "Optional reason for your decision",
								},
							},
							"required": []string{"proceed"},
						},
					},
				}

				result, err := mcpServer.RequestElicitation(ctx, elicitationRequest)
				if err != nil {
					return nil, fmt.Errorf("failed to get confirmation: %w", err)
				}

				if result.Response.Type != mcp.ElicitationResponseTypeAccept {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent("Processing cancelled by user"),
						},
					}, nil
				}

				responseData := result.Response.Value.(map[string]interface{})
				if proceed, ok := responseData["proceed"].(bool); !ok || !proceed {
					reason := "No reason provided"
					if r, ok := responseData["reason"].(string); ok && r != "" {
						reason = r
					}
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Processing declined: %s", reason)),
						},
					}, nil
				}
			}

			// Process the data
			processed := fmt.Sprintf("Processed %d characters of data", len(data))
			count := requestCount.Add(1)

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("%s (request #%d)", processed, count)),
				},
			}, nil
		},
	)

	// Create and start stdio server
	stdioServer := server.NewStdioServer(mcpServer)

	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	go func() {
		<-sigChan
		cancel()
	}()

	fmt.Fprintln(os.Stderr, "Elicitation demo server started")
	if err := stdioServer.Listen(ctx, os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}
