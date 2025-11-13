package main

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Define a struct for our typed arguments
type GreetingArgs struct {
	Name      string   `json:"name"`
	Age       int      `json:"age"`
	IsVIP     bool     `json:"is_vip"`
	Languages []string `json:"languages"`
	Metadata  struct {
		Location string `json:"location"`
		Timezone string `json:"timezone"`
	} `json:"metadata"`
	AnyData any `json:"any_data"`
}

// main starts the MCP-based example server, registers a typed "greeting" tool, and serves it over standard I/O.
//
// The registered tool exposes a schema for typed inputs (name, age, is_vip, languages, metadata, and any_data)
// and uses a typed handler to produce personalized greetings. If the server fails to start, an error is printed to stdout.
func main() {
	// Create a new MCP server
	s := server.NewMCPServer(
		"Typed Tools Demo 🚀",
		"1.0.0",
		server.WithToolCapabilities(false),
	)

	// Add tool with complex schema
	tool := mcp.NewTool("greeting",
		mcp.WithDescription("Generate a personalized greeting"),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Name of the person to greet"),
		),
		mcp.WithNumber("age",
			mcp.Description("Age of the person"),
			mcp.Min(0),
			mcp.Max(150),
		),
		mcp.WithBoolean("is_vip",
			mcp.Description("Whether the person is a VIP"),
			mcp.DefaultBool(false),
		),
		mcp.WithArray("languages",
			mcp.Description("Languages the person speaks"),
			mcp.Items(map[string]any{"type": "string"}),
		),
		mcp.WithObject("metadata",
			mcp.Description("Additional information about the person"),
			mcp.Properties(map[string]any{
				"location": map[string]any{
					"type":        "string",
					"description": "Current location",
				},
				"timezone": map[string]any{
					"type":        "string",
					"description": "Timezone",
				},
			}),
		),
		mcp.WithAny("any_data",
			mcp.Description("Any kind of data, e.g., an integer"),
		),
	)

	// Add tool handler using the typed handler
	s.AddTool(tool, mcp.NewTypedToolHandler(typedGreetingHandler))

	// Start the stdio server
	if err := server.ServeStdio(s); err != nil {
		fmt.Printf("Server error: %v\n", err)
	}
}

// typedGreetingHandler constructs a personalized greeting from the provided GreetingArgs and returns it as a text tool result.
//
// If args.Name is empty the function returns a tool error result with the message "name is required" and a nil error.
// The returned greeting may include the caller's age, a VIP acknowledgement, the number and list of spoken languages,
// location and timezone from metadata, and a formatted representation of AnyData when present.
func typedGreetingHandler(ctx context.Context, request mcp.CallToolRequest, args GreetingArgs) (*mcp.CallToolResult, error) {
	if args.Name == "" {
		return mcp.NewToolResultError("name is required"), nil
	}

	// Build a personalized greeting based on the complex arguments
	greeting := fmt.Sprintf("Hello, %s!", args.Name)

	if args.Age > 0 {
		greeting += fmt.Sprintf(" You are %d years old.", args.Age)
	}

	if args.IsVIP {
		greeting += " Welcome back, valued VIP customer!"
	}

	if len(args.Languages) > 0 {
		greeting += fmt.Sprintf(" You speak %d languages: %v.", len(args.Languages), args.Languages)
	}

	if args.Metadata.Location != "" {
		greeting += fmt.Sprintf(" I see you're from %s.", args.Metadata.Location)

		if args.Metadata.Timezone != "" {
			greeting += fmt.Sprintf(" Your timezone is %s.", args.Metadata.Timezone)
		}
	}

	if args.AnyData != nil {
		greeting += fmt.Sprintf(" I also received some other data: %v.", args.AnyData)
	}

	return mcp.NewToolResultText(greeting), nil
}
