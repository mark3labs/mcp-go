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
				RequestedSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"projectName": map[string]any{
							"type":        "string",
							"description": "Name of the project",
							"minLength":   1,
						},
						"framework": map[string]any{
							"type":        "string",
							"description": "Frontend framework to use",
							"enum":        []string{"react", "vue", "angular", "none"},
						},
						"includeTests": map[string]any{
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
		switch result.Action {
		case mcp.ElicitationResponseActionAccept:
			// User provided the information
			data, ok := result.Content.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("unexpected response format: expected map[string]any, got %T", result.Content)
			}

			// Safely extract projectName (required field)
			projectNameRaw, exists := data["projectName"]
			if !exists {
				return nil, fmt.Errorf("required field 'projectName' is missing from response")
			}
			projectName, ok := projectNameRaw.(string)
			if !ok {
				return nil, fmt.Errorf("field 'projectName' must be a string, got %T", projectNameRaw)
			}
			if projectName == "" {
				return nil, fmt.Errorf("field 'projectName' cannot be empty")
			}

			// Safely extract framework (optional field)
			framework := "none"
			if frameworkRaw, exists := data["framework"]; exists {
				if fw, ok := frameworkRaw.(string); ok {
					framework = fw
				} else {
					return nil, fmt.Errorf("field 'framework' must be a string, got %T", frameworkRaw)
				}
			}

			// Safely extract includeTests (optional field)
			includeTests := true
			if testsRaw, exists := data["includeTests"]; exists {
				if tests, ok := testsRaw.(bool); ok {
					includeTests = tests
				} else {
					return nil, fmt.Errorf("field 'includeTests' must be a boolean, got %T", testsRaw)
				}
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

		case mcp.ElicitationResponseActionDecline:
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent("Project creation cancelled - user declined to provide information"),
				},
			}, nil

		case mcp.ElicitationResponseActionCancel:
			return nil, fmt.Errorf("project creation cancelled by user")

		default:
			return nil, fmt.Errorf("unexpected response action: %s", result.Action)
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
			// Safely extract data argument
			dataRaw, exists := request.GetArguments()["data"]
			if !exists {
				return nil, fmt.Errorf("required parameter 'data' is missing")
			}
			data, ok := dataRaw.(string)
			if !ok {
				return nil, fmt.Errorf("parameter 'data' must be a string, got %T", dataRaw)
			}

			// Only request elicitation if data seems sensitive
			if len(data) > 100 {
				elicitationRequest := mcp.ElicitationRequest{
					Params: mcp.ElicitationParams{
						Message: fmt.Sprintf("The data is %d characters long. Do you want to proceed with processing?", len(data)),
						RequestedSchema: map[string]any{
							"type": "object",
							"properties": map[string]any{
								"proceed": map[string]any{
									"type":        "boolean",
									"description": "Whether to proceed with processing",
								},
								"reason": map[string]any{
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

				if result.Action != mcp.ElicitationResponseActionAccept {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent("Processing cancelled by user"),
						},
					}, nil
				}

				// Safely extract response data
				responseData, ok := result.Content.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("unexpected response format: expected map[string]any, got %T", result.Content)
				}

				// Safely extract proceed field
				proceedRaw, exists := responseData["proceed"]
				if !exists {
					return nil, fmt.Errorf("required field 'proceed' is missing from response")
				}
				proceed, ok := proceedRaw.(bool)
				if !ok {
					return nil, fmt.Errorf("field 'proceed' must be a boolean, got %T", proceedRaw)
				}

				if !proceed {
					reason := "No reason provided"
					if reasonRaw, exists := responseData["reason"]; exists {
						if r, ok := reasonRaw.(string); ok && r != "" {
							reason = r
						} else if reasonRaw != nil {
							return nil, fmt.Errorf("field 'reason' must be a string, got %T", reasonRaw)
						}
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
