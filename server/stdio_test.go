package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestStdioServer(t *testing.T) {
	t.Run("Can instantiate", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		stdioServer := NewStdioServer(mcpServer)

		if stdioServer.server == nil {
			t.Error("MCPServer should not be nil")
		}
		if stdioServer.errLogger == nil {
			t.Error("errLogger should not be nil")
		}
	})

	t.Run("Can send and receive messages", func(t *testing.T) {
		// Create pipes for stdin and stdout
		stdinReader, stdinWriter := io.Pipe()
		stdoutReader, stdoutWriter := io.Pipe()

		// Create server
		mcpServer := NewMCPServer("test", "1.0.0",
			WithResourceCapabilities(true, true),
		)
		stdioServer := NewStdioServer(mcpServer)
		stdioServer.SetErrorLogger(log.New(io.Discard, "", 0))

		// Create context with cancel
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Create error channel to catch server errors
		serverErrCh := make(chan error, 1)

		// Start server in goroutine
		go func() {
			err := stdioServer.Listen(ctx, stdinReader, stdoutWriter)
			if err != nil && err != io.EOF && err != context.Canceled {
				serverErrCh <- err
			}
			close(serverErrCh)
		}()

		// Create test message
		initRequest := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]any{
				"protocolVersion": "2024-11-05",
				"clientInfo": map[string]any{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}

		// Send request
		requestBytes, err := json.Marshal(initRequest)
		if err != nil {
			t.Fatal(err)
		}
		_, err = stdinWriter.Write(append(requestBytes, '\n'))
		if err != nil {
			t.Fatal(err)
		}

		// Read response
		scanner := bufio.NewScanner(stdoutReader)
		if !scanner.Scan() {
			t.Fatal("failed to read response")
		}
		responseBytes := scanner.Bytes()

		var response map[string]any
		if err := json.Unmarshal(responseBytes, &response); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		// Verify response structure
		if response["jsonrpc"] != "2.0" {
			t.Errorf("expected jsonrpc version 2.0, got %v", response["jsonrpc"])
		}
		if response["id"].(float64) != 1 {
			t.Errorf("expected id 1, got %v", response["id"])
		}
		if response["error"] != nil {
			t.Errorf("unexpected error in response: %v", response["error"])
		}
		if response["result"] == nil {
			t.Error("expected result in response")
		}

		// Clean up
		cancel()
		stdinWriter.Close()
		stdoutWriter.Close()

		// Check for server errors
		if err := <-serverErrCh; err != nil {
			t.Errorf("unexpected server error: %v", err)
		}
	})

	t.Run("Can use a custom context function", func(t *testing.T) {
		// Use a custom context key to store a test value.
		type testContextKey struct{}
		testValFromContext := func(ctx context.Context) string {
			val := ctx.Value(testContextKey{})
			if val == nil {
				return ""
			}
			return val.(string)
		}
		// Create a context function that sets a test value from the environment.
		// In real life this could be used to send configuration in a similar way,
		// or from a config file.
		const testEnvVar = "TEST_ENV_VAR"
		setTestValFromEnv := func(ctx context.Context) context.Context {
			return context.WithValue(ctx, testContextKey{}, os.Getenv(testEnvVar))
		}
		t.Setenv(testEnvVar, "test_value")

		// Create pipes for stdin and stdout
		stdinReader, stdinWriter := io.Pipe()
		stdoutReader, stdoutWriter := io.Pipe()

		// Create server
		mcpServer := NewMCPServer("test", "1.0.0")
		// Add a tool which uses the context function.
		mcpServer.AddTool(mcp.NewTool("test_tool"), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// Note this is agnostic to the transport type i.e. doesn't know about request headers.
			testVal := testValFromContext(ctx)
			return mcp.NewToolResultText(testVal), nil
		})
		stdioServer := NewStdioServer(mcpServer)
		stdioServer.SetErrorLogger(log.New(io.Discard, "", 0))
		stdioServer.SetContextFunc(setTestValFromEnv)

		// Create context with cancel
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Create error channel to catch server errors
		serverErrCh := make(chan error, 1)

		// Start server in goroutine
		go func() {
			err := stdioServer.Listen(ctx, stdinReader, stdoutWriter)
			if err != nil && err != io.EOF && err != context.Canceled {
				serverErrCh <- err
			}
			close(serverErrCh)
		}()

		// Create test message
		initRequest := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]any{
				"protocolVersion": "2024-11-05",
				"clientInfo": map[string]any{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}

		// Send request
		requestBytes, err := json.Marshal(initRequest)
		if err != nil {
			t.Fatal(err)
		}
		_, err = stdinWriter.Write(append(requestBytes, '\n'))
		if err != nil {
			t.Fatal(err)
		}

		// Read response
		scanner := bufio.NewScanner(stdoutReader)
		if !scanner.Scan() {
			t.Fatal("failed to read response")
		}
		responseBytes := scanner.Bytes()

		var response map[string]any
		if err := json.Unmarshal(responseBytes, &response); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		// Verify response structure
		if response["jsonrpc"] != "2.0" {
			t.Errorf("expected jsonrpc version 2.0, got %v", response["jsonrpc"])
		}
		if response["id"].(float64) != 1 {
			t.Errorf("expected id 1, got %v", response["id"])
		}
		if response["error"] != nil {
			t.Errorf("unexpected error in response: %v", response["error"])
		}
		if response["result"] == nil {
			t.Error("expected result in response")
		}

		// Call the tool.
		toolRequest := map[string]any{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "tools/call",
			"params": map[string]any{
				"name": "test_tool",
			},
		}
		requestBytes, err = json.Marshal(toolRequest)
		if err != nil {
			t.Fatalf("Failed to marshal tool request: %v", err)
		}

		_, err = stdinWriter.Write(append(requestBytes, '\n'))
		if err != nil {
			t.Fatal(err)
		}

		if !scanner.Scan() {
			t.Fatal("failed to read response")
		}
		responseBytes = scanner.Bytes()

		response = map[string]any{}
		if err := json.Unmarshal(responseBytes, &response); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if response["jsonrpc"] != "2.0" {
			t.Errorf("Expected jsonrpc 2.0, got %v", response["jsonrpc"])
		}
		if response["id"].(float64) != 2 {
			t.Errorf("Expected id 2, got %v", response["id"])
		}
		if response["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"] != "test_value" {
			t.Errorf("Expected result 'test_value', got %v", response["result"])
		}
		if response["error"] != nil {
			t.Errorf("Expected no error, got %v", response["error"])
		}

		// Clean up
		cancel()
		stdinWriter.Close()
		stdoutWriter.Close()

		// Check for server errors
		if err := <-serverErrCh; err != nil {
			t.Errorf("unexpected server error: %v", err)
		}
	})
}
