package mcptest_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/mcptest"
	"github.com/mark3labs/mcp-go/server"
)

func TestServerWithTool(t *testing.T) {
	ctx := t.Context()

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

func TestServerWithToolStructuredContent(t *testing.T) {
	ctx := t.Context()

	srv, err := mcptest.NewServer(t, server.ServerTool{
		Tool: mcp.NewTool("get_user",
			mcp.WithDescription("Gets user information with structured data."),
			mcp.WithString("user_id", mcp.Description("The user ID to look up.")),
		),
		Handler: structuredContentHandler,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	client := srv.Client()

	var req mcp.CallToolRequest
	req.Params.Name = "get_user"
	req.Params.Arguments = map[string]any{
		"user_id": "123",
	}

	result, err := client.CallTool(ctx, req)
	if err != nil {
		t.Fatal("CallTool:", err)
	}

	if result.IsError {
		t.Fatalf("unexpected error result: %+v", result)
	}

	if len(result.Content) != 1 {
		t.Fatalf("Expected 1 content item, got %d", len(result.Content))
	}

	// Check text content (fallback)
	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("Expected content to be TextContent, got %T", result.Content[0])
	}
	expectedText := "User found"
	if textContent.Text != expectedText {
		t.Errorf("Expected text %q, got %q", expectedText, textContent.Text)
	}

	// Check structured content
	if result.StructuredContent == nil {
		t.Fatal("Expected StructuredContent to be present")
	}

	structuredData, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("Expected StructuredContent to be map[string]any, got %T", result.StructuredContent)
	}

	// Verify structured data
	if structuredData["id"] != "123" {
		t.Errorf("Expected id '123', got %v", structuredData["id"])
	}
	if structuredData["name"] != "John Doe" {
		t.Errorf("Expected name 'John Doe', got %v", structuredData["name"])
	}
	if structuredData["email"] != "john@example.com" {
		t.Errorf("Expected email 'john@example.com', got %v", structuredData["email"])
	}
	if structuredData["active"] != true {
		t.Errorf("Expected active true, got %v", structuredData["active"])
	}
}

func structuredContentHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID, ok := request.GetArguments()["user_id"].(string)
	if !ok {
		return mcp.NewToolResultError("user_id parameter is required"), nil
	}

	// Create structured data
	userData := map[string]any{
		"id":     userID,
		"name":   "John Doe",
		"email":  "john@example.com",
		"active": true,
	}

	// Use NewToolResultStructured to create result with both text fallback and structured content
	return mcp.NewToolResultStructured(userData, "User found"), nil
}

func TestServerWithPrompt(t *testing.T) {
	ctx := t.Context()

	srv := mcptest.NewUnstartedServer(t)
	defer srv.Close()

	prompt := mcp.Prompt{
		Name:        "greeting",
		Description: "A greeting prompt",
		Arguments: []mcp.PromptArgument{
			{
				Name:        "name",
				Description: "The name to greet",
				Required:    true,
			},
		},
	}
	handler := func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: "A greeting prompt",
			Messages: []mcp.PromptMessage{
				{
					Role:    mcp.RoleUser,
					Content: mcp.NewTextContent(fmt.Sprintf("Hello, %s!", request.Params.Arguments["name"])),
				},
			},
		}, nil
	}

	srv.AddPrompt(prompt, handler)

	err := srv.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var getReq mcp.GetPromptRequest
	getReq.Params.Name = "greeting"
	getReq.Params.Arguments = map[string]string{"name": "John"}
	getResult, err := srv.Client().GetPrompt(ctx, getReq)
	if err != nil {
		t.Fatal("GetPrompt:", err)
	}
	if getResult.Description != "A greeting prompt" {
		t.Errorf("Expected prompt description 'A greeting prompt', got %q", getResult.Description)
	}
	if len(getResult.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(getResult.Messages))
	}
	if getResult.Messages[0].Role != mcp.RoleUser {
		t.Errorf("Expected message role 'user', got %q", getResult.Messages[0].Role)
	}
	content, ok := getResult.Messages[0].Content.(mcp.TextContent)
	if !ok {
		t.Fatalf("Expected TextContent, got %T", getResult.Messages[0].Content)
	}
	if content.Text != "Hello, John!" {
		t.Errorf("Expected message content 'Hello, John!', got %q", content.Text)
	}
}

func TestServerWithResource(t *testing.T) {
	ctx := t.Context()

	srv := mcptest.NewUnstartedServer(t)
	defer srv.Close()

	resource := mcp.Resource{
		URI:         "test://resource",
		Name:        "Test Resource",
		Description: "A test resource",
		MIMEType:    "text/plain",
	}

	handler := func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      "test://resource",
				MIMEType: "text/plain",
				Text:     "This is a test resource content.",
			},
		}, nil
	}

	srv.AddResource(resource, handler)

	err := srv.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var readReq mcp.ReadResourceRequest
	readReq.Params.URI = "test://resource"
	readResult, err := srv.Client().ReadResource(ctx, readReq)
	if err != nil {
		t.Fatal("ReadResource:", err)
	}
	if len(readResult.Contents) != 1 {
		t.Fatalf("Expected 1 content, got %d", len(readResult.Contents))
	}
	textContent, ok := readResult.Contents[0].(mcp.TextResourceContents)
	if !ok {
		t.Fatalf("Expected TextResourceContents, got %T", readResult.Contents[0])
	}
	want := "This is a test resource content."
	if textContent.Text != want {
		t.Errorf("Got %q, want %q", textContent.Text, want)
	}
}

func TestServerWithResourceTemplate(t *testing.T) {
	ctx := t.Context()

	srv := mcptest.NewUnstartedServer(t)
	defer srv.Close()

	template := mcp.NewResourceTemplate(
		"file://users/{userId}/documents/{docId}",
		"User Document",
		mcp.WithTemplateDescription("A user's document"),
		mcp.WithTemplateMIMEType("text/plain"),
	)

	handler := func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		if request.Params.Arguments == nil {
			return nil, fmt.Errorf("expected arguments to be populated from URI template")
		}

		userIds, ok := request.Params.Arguments["userId"].([]string)
		if !ok {
			return nil, fmt.Errorf("expected userId argument to be populated from URI template")
		}
		if len(userIds) != 1 {
			return nil, fmt.Errorf("expected userId to have one value, but got %d", len(userIds))
		}
		if userIds[0] != "john" {
			return nil, fmt.Errorf("expected userId argument to be 'john', got %s", userIds[0])
		}

		docIds, ok := request.Params.Arguments["docId"].([]string)
		if !ok {
			return nil, fmt.Errorf("expected docId argument to be populated from URI template")
		}
		if len(docIds) != 1 {
			return nil, fmt.Errorf("expected docId to have one value, but got %d", len(docIds))
		}
		if docIds[0] != "readme.txt" {
			return nil, fmt.Errorf("expected docId argument to be 'readme.txt', got %v", docIds)
		}

		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      request.Params.URI,
				MIMEType: "text/plain",
				Text:     fmt.Sprintf("Document %s for user %s", docIds[0], userIds[0]),
			},
		}, nil
	}

	srv.AddResourceTemplate(template, handler)

	err := srv.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Test reading a resource that matches the template
	var readReq mcp.ReadResourceRequest
	readReq.Params.URI = "file://users/john/documents/readme.txt"
	readResult, err := srv.Client().ReadResource(ctx, readReq)
	if err != nil {
		t.Fatal("ReadResource:", err)
	}
	if len(readResult.Contents) != 1 {
		t.Fatalf("Expected 1 content, got %d", len(readResult.Contents))
	}
	textContent, ok := readResult.Contents[0].(mcp.TextResourceContents)
	if !ok {
		t.Fatalf("Expected TextResourceContents, got %T", readResult.Contents[0])
	}
	want := "Document readme.txt for user john"
	if textContent.Text != want {
		t.Errorf("Got %q, want %q", textContent.Text, want)
	}
}

func TestListToolsWithHeader(t *testing.T) {
	expectedHeaderValue := "test-header-value"
	gotHeaderValue := ""

	hooks := &server.Hooks{}
	hooks.AddAfterListTools(func(ctx context.Context, id any, message *mcp.ListToolsRequest, result *mcp.ListToolsResult) {
		gotHeaderValue = message.Header.Get("X-Test-Header")
	})

	// Create MCP server with capabilities
	mcpServer := server.NewMCPServer(
		"test-server",
		"1.0.0",
		server.WithToolCapabilities(true),
		server.WithHooks(hooks),
	)

	testServer := server.NewTestStreamableHTTPServer(mcpServer)
	defer testServer.Close()

	initRequest := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "test-client",
				Version: "1.0.0",
			},
		},
	}

	client, err := client.NewStreamableHttpClient(testServer.URL)
	if err != nil {
		t.Fatalf("Create client failed %v", err)
		return
	}
	ctx := t.Context()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("Failed to start client: %v", err)
		return
	}

	// Initialize
	_, err = client.Initialize(ctx, initRequest)
	if err != nil {
		t.Fatalf("Failed to initialize: %v\n", err)
	}

	req := mcp.ListToolsRequest{Header: http.Header{"X-Test-Header": {expectedHeaderValue}}}
	_, err = client.ListToolsByPage(t.Context(), req)
	if err != nil {
		t.Fatalf("Failed to ListTools: %v\n", err)
	}
	if expectedHeaderValue != gotHeaderValue {
		t.Fatalf("Expected value is %s, got %s", expectedHeaderValue, gotHeaderValue)
	}
}

func TestServerWithHooks(t *testing.T) {
	ctx := t.Context()

	var callCount atomic.Int32
	hooks := &server.Hooks{}
	hooks.AddBeforeCallTool(func(ctx context.Context, id any, request *mcp.CallToolRequest) {
		callCount.Add(1)
	})

	srv := mcptest.NewUnstartedServer(t)
	defer srv.Close()

	srv.AddTool(mcp.NewTool("greet",
		mcp.WithDescription("Says hello."),
		mcp.WithString("name", mcp.Description("Name to greet.")),
	), helloWorldHandler)

	srv.AddServerOptions(server.WithHooks(hooks))

	if err := srv.Start(ctx); err != nil {
		t.Fatal("Start:", err)
	}

	var req mcp.CallToolRequest
	req.Params.Name = "greet"
	req.Params.Arguments = map[string]any{"name": "World"}

	result, err := srv.Client().CallTool(ctx, req)
	if err != nil {
		t.Fatal("CallTool:", err)
	}

	got, err := resultToString(result)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Hello, World!" {
		t.Errorf("Got %q, want %q", got, "Hello, World!")
	}

	if n := callCount.Load(); n != 1 {
		t.Errorf("Expected hook to be called 1 time, got %d", n)
	}
}

func TestServerWithToolFilter(t *testing.T) {
	ctx := t.Context()

	srv := mcptest.NewUnstartedServer(t)
	defer srv.Close()

	srv.AddTools(
		server.ServerTool{
			Tool:    mcp.NewTool("visible_tool", mcp.WithDescription("This tool is visible.")),
			Handler: helloWorldHandler,
		},
		server.ServerTool{
			Tool:    mcp.NewTool("hidden_tool", mcp.WithDescription("This tool is hidden.")),
			Handler: helloWorldHandler,
		},
	)

	// Filter out tools whose name starts with "hidden_".
	srv.AddServerOptions(
		server.WithToolCapabilities(false),
		server.WithToolFilter(func(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
			var filtered []mcp.Tool
			for _, tool := range tools {
				if !strings.HasPrefix(tool.Name, "hidden_") {
					filtered = append(filtered, tool)
				}
			}
			return filtered
		}),
	)

	if err := srv.Start(ctx); err != nil {
		t.Fatal("Start:", err)
	}

	result, err := srv.Client().ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal("ListTools:", err)
	}

	if len(result.Tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(result.Tools))
	}
	if result.Tools[0].Name != "visible_tool" {
		t.Errorf("Expected tool name %q, got %q", "visible_tool", result.Tools[0].Name)
	}
}

func TestServerWithToolHandlerMiddleware(t *testing.T) {
	ctx := t.Context()

	srv := mcptest.NewUnstartedServer(t)
	defer srv.Close()

	srv.AddTool(mcp.NewTool("echo",
		mcp.WithDescription("Echoes input."),
		mcp.WithString("msg", mcp.Description("Message to echo.")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		msg, _ := request.RequireString("msg")
		return mcp.NewToolResultText(msg), nil
	})

	// Middleware that prefixes the result with "[middleware] ".
	srv.AddServerOptions(
		server.WithToolHandlerMiddleware(func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
			return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				result, err := next(ctx, request)
				if err != nil {
					return result, err
				}
				for i, content := range result.Content {
					if tc, ok := content.(mcp.TextContent); ok {
						tc.Text = "[middleware] " + tc.Text
						result.Content[i] = tc
					}
				}
				return result, nil
			}
		}),
	)

	if err := srv.Start(ctx); err != nil {
		t.Fatal("Start:", err)
	}

	var req mcp.CallToolRequest
	req.Params.Name = "echo"
	req.Params.Arguments = map[string]any{"msg": "hello"}

	result, err := srv.Client().CallTool(ctx, req)
	if err != nil {
		t.Fatal("CallTool:", err)
	}

	got, err := resultToString(result)
	if err != nil {
		t.Fatal(err)
	}

	want := "[middleware] hello"
	if got != want {
		t.Errorf("Got %q, want %q", got, want)
	}
}

func TestSimulateClientInfo(t *testing.T) {
	ctx := t.Context()

	srv := mcptest.NewUnstartedServer(t)
	defer srv.Close()
	srv.AddTool(mcp.NewTool("whoami", mcp.WithDescription("Says hello to client.")),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			clientName := ""
			if clientSession := server.ClientSessionFromContext(ctx); clientSession != nil {
				if sessionWithClientInfo, ok := clientSession.(server.SessionWithClientInfo); ok {
					clientName = sessionWithClientInfo.GetClientInfo().Name
				}
			}
			return mcp.NewToolResultText(fmt.Sprintf("Hello, %s!", clientName)), nil
		})
	srv.SetClientInfo(mcp.Implementation{
		Name: "test-client",
	})
	err := srv.Start(ctx)
	if err != nil {
		t.Fatal("Start:", err)
	}

	client := srv.Client()

	var req mcp.CallToolRequest
	req.Params.Name = "whoami"

	result, err := client.CallTool(ctx, req)
	if err != nil {
		t.Fatal("CallTool:", err)
	}

	got, err := resultToString(result)
	if err != nil {
		t.Fatal(err)
	}

	want := "Hello, test-client!"
	if got != want {
		t.Errorf("Got %q, want %q", got, want)
	}
}
