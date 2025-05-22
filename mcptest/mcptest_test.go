package mcptest_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/mcptest"
	"github.com/mark3labs/mcp-go/server"
)

func TestServer(t *testing.T) {
	ctx := context.Background()

	srv, err := mcptest.NewServer(t, server.ServerTool{
		Tool: mcp.NewTool("hello",
			mcp.WithDescription("Says hello to the provided name, or world."),
			mcp.WithString("name", mcp.Description("The name to say hello to.")),
		),
		Handler: helloWorldHandler,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	client := srv.Client()

	var req mcp.CallToolRequest
	req.Params.Name = "hello"
	req.Params.Arguments = map[string]any{
		"name": "Claude",
	}

	result, err := client.CallTool(ctx, req)
	if err != nil {
		t.Fatal("CallTool:", err)
	}

	got, err := resultToString(result)
	if err != nil {
		t.Fatal(err)
	}

	want := "Hello, Claude!"
	if got != want {
		t.Errorf("Got %q, want %q", got, want)
	}
}

func helloWorldHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Extract name from request arguments
	name, ok := request.GetArguments()["name"].(string)
	if !ok {
		name = "World"
	}

	return mcp.NewToolResultText(fmt.Sprintf("Hello, %s!", name)), nil
}

func resultToString(result *mcp.CallToolResult) (string, error) {
	var b strings.Builder

	for _, content := range result.Content {
		text, ok := content.(mcp.TextContent)
		if !ok {
			return "", fmt.Errorf("unsupported content type: %T", content)
		}
		b.WriteString(text.Text)
	}

	if result.IsError {
		return "", fmt.Errorf("%s", b.String())
	}

	return b.String(), nil
}
