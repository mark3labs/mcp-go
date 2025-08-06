package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"testing"
	"time"

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
			stdoutWriter.Close()
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
			stdoutWriter.Close()
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

		// Check for server errors
		if err := <-serverErrCh; err != nil {
			t.Errorf("unexpected server error: %v", err)
		}
	})

	t.Run("Can handle concurrent tool calls", func(t *testing.T) {
		// Create pipes for stdin and stdout
		stdinReader, stdinWriter := io.Pipe()
		stdoutReader, stdoutWriter := io.Pipe()

		// Track tool call executions (sync.Map is already thread-safe)
		var callCount sync.Map

		// Create server with test tools
		mcpServer := NewMCPServer("test", "1.0.0")

		// Add multiple tools that simulate work and track concurrent execution
		for i := 0; i < 5; i++ {
			toolName := fmt.Sprintf("test_tool_%d", i)
			mcpServer.AddTool(
				mcp.NewTool(toolName),
				func(name string) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						// Track concurrent executions
						count, _ := callCount.LoadOrStore(name, 0)
						callCount.Store(name, count.(int)+1)

						// Simulate some work
						time.Sleep(10 * time.Millisecond)

						return mcp.NewToolResultText(fmt.Sprintf("Result from %s", name)), nil
					}
				}(toolName),
			)
		}

		stdioServer := NewStdioServer(mcpServer)
		stdioServer.SetErrorLogger(log.New(io.Discard, "", 0))

		// Create context with cancel
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Start server
		serverErrCh := make(chan error, 1)
		go func() {
			err := stdioServer.Listen(ctx, stdinReader, stdoutWriter)
			if err != nil && err != io.EOF && err != context.Canceled {
				serverErrCh <- err
			}
			stdoutWriter.Close()
			close(serverErrCh)
		}()

		// Initialize the session
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

		requestBytes, _ := json.Marshal(initRequest)
		if _, err := stdinWriter.Write(append(requestBytes, '\n')); err != nil {
			t.Fatalf("Failed to write init request: %v", err)
		}

		// Read init response
		scanner := bufio.NewScanner(stdoutReader)
		scanner.Scan()

		// Send multiple concurrent tool calls
		var wg sync.WaitGroup
		responseChan := make(chan string, 10)

		// Send 10 concurrent tool calls
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				toolRequest := map[string]any{
					"jsonrpc": "2.0",
					"id":      id + 2,
					"method":  "tools/call",
					"params": map[string]any{
						"name": fmt.Sprintf("test_tool_%d", id%5),
					},
				}

				requestBytes, _ := json.Marshal(toolRequest)
				if _, err := stdinWriter.Write(append(requestBytes, '\n')); err != nil {
					t.Errorf("Failed to write tool request %d: %v", id, err)
				}
			}(i)
		}

		// Read all responses
		go func() {
			for i := 0; i < 10; i++ {
				if scanner.Scan() {
					responseChan <- scanner.Text()
				}
			}
			close(responseChan)
		}()

		// Wait for all requests to be sent
		wg.Wait()

		// Collect responses
		responses := make([]string, 0, 10)
		timeout := time.After(2 * time.Second)

	collectLoop:
		for len(responses) < 10 {
			select {
			case resp, ok := <-responseChan:
				if !ok {
					break collectLoop
				}
				responses = append(responses, resp)
			case <-timeout:
				t.Fatal("Timeout waiting for responses")
			}
		}
		// Verify we got all responses
		if len(responses) != 10 {
			t.Errorf("Expected 10 responses, got %d", len(responses))
		}

		// Verify no errors in responses
		for _, resp := range responses {
			var response map[string]any
			if err := json.Unmarshal([]byte(resp), &response); err != nil {
				t.Errorf("Failed to unmarshal response: %v", err)
				continue
			}

			if response["error"] != nil {
				t.Errorf("Unexpected error in response: %v", response["error"])
			}

			// Verify response has expected structure
			if response["result"] == nil {
				t.Error("Expected result in response")
			}
		}

		// Verify tools were called
		callCount.Range(func(key, value interface{}) bool {
			toolName := key.(string)
			count := value.(int)
			if count == 0 {
				t.Errorf("Tool %s was not called", toolName)
			}
			return true
		})

		// Clean up
		cancel()
		stdinWriter.Close()

		// Check for server errors
		if err := <-serverErrCh; err != nil {
			t.Errorf("Server error: %v", err)
		}
	})

	t.Run("Configuration options respect bounds", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")

		// Test worker pool size bounds
		stdioServer := NewStdioServer(mcpServer)
		WithWorkerPoolSize(150)(stdioServer)
		if stdioServer.workerPoolSize != 100 { // Should use maximum
			t.Errorf("Expected maximum worker pool size 100, got %d", stdioServer.workerPoolSize)
		}

		// Test valid worker pool size
		stdioServer = NewStdioServer(mcpServer)
		WithWorkerPoolSize(50)(stdioServer)
		if stdioServer.workerPoolSize != 50 {
			t.Errorf("Expected worker pool size 50, got %d", stdioServer.workerPoolSize)
		}

		// Test queue size bounds
		stdioServer = NewStdioServer(mcpServer)
		WithQueueSize(20000)(stdioServer)
		if stdioServer.queueSize != 10000 { // Should use maximum
			t.Errorf("Expected maximum queue size 10000, got %d", stdioServer.queueSize)
		}

		// Test valid queue size
		stdioServer = NewStdioServer(mcpServer)
		WithQueueSize(500)(stdioServer)
		if stdioServer.queueSize != 500 {
			t.Errorf("Expected queue size 500, got %d", stdioServer.queueSize)
		}

		// Test zero and negative values
		stdioServer = NewStdioServer(mcpServer)
		WithWorkerPoolSize(0)(stdioServer)
		WithQueueSize(-10)(stdioServer)
		if stdioServer.workerPoolSize != 5 {
			t.Errorf("Expected default worker pool size 5 for zero input, got %d", stdioServer.workerPoolSize)
		}
		if stdioServer.queueSize != 100 {
			t.Errorf("Expected default queue size 100 for negative input, got %d", stdioServer.queueSize)
		}
	})
}
