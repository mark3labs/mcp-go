package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestSSEServer(t *testing.T) {
	t.Run("Can instantiate", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		sseServer := NewSSEServer(mcpServer,
			WithBaseURL("http://localhost:8080"),
			WithBasePath("/mcp"),
		)

		if sseServer == nil {
			t.Error("SSEServer should not be nil")
		}
		if sseServer.server == nil {
			t.Error("MCPServer should not be nil")
		}
		if sseServer.baseURL != "http://localhost:8080/mcp" {
			t.Errorf(
				"Expected baseURL http://localhost:8080/mcp, got %s",
				sseServer.baseURL,
			)
		}
		if sseServer.basePath != "/mcp" {
			t.Errorf(
				"Expected basePath /mcp, got %s",
				sseServer.basePath,
			)
		}
	})

	t.Run("Can send and receive messages", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0",
			WithResourceCapabilities(true, true),
		)
		testServer := NewTestServer(mcpServer)
		defer testServer.Close()

		// Connect to SSE endpoint
		sseResp, err := http.Get(fmt.Sprintf("%s/sse", testServer.URL))
		if err != nil {
			t.Fatalf("Failed to connect to SSE endpoint: %v", err)
		}
		defer sseResp.Body.Close()

		// Read the endpoint event
		buf := make([]byte, 1024)
		n, err := sseResp.Body.Read(buf)
		if err != nil {
			t.Fatalf("Failed to read SSE response: %v", err)
		}

		endpointEvent := string(buf[:n])
		if !strings.Contains(endpointEvent, "event: endpoint") {
			t.Fatalf("Expected endpoint event, got: %s", endpointEvent)
		}

		// Extract message endpoint URL
		messageURL := strings.TrimSpace(
			strings.Split(strings.Split(endpointEvent, "data: ")[1], "\n")[0],
		)

		// Send initialize request
		initRequest := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"clientInfo": map[string]interface{}{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}

		requestBody, err := json.Marshal(initRequest)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		resp, err := http.Post(
			messageURL,
			"application/json",
			bytes.NewBuffer(requestBody),
		)
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusAccepted {
			t.Errorf("Expected status 202, got %d", resp.StatusCode)
		}

		// Verify response
		var response map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if response["jsonrpc"] != "2.0" {
			t.Errorf("Expected jsonrpc 2.0, got %v", response["jsonrpc"])
		}
		if response["id"].(float64) != 1 {
			t.Errorf("Expected id 1, got %v", response["id"])
		}
	})

	t.Run("Can handle multiple sessions", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0",
			WithResourceCapabilities(true, true),
		)
		testServer := NewTestServer(mcpServer)
		defer testServer.Close()

		numSessions := 3
		var wg sync.WaitGroup
		wg.Add(numSessions)

		for i := 0; i < numSessions; i++ {
			go func(sessionNum int) {
				defer wg.Done()

				// Connect to SSE endpoint
				sseResp, err := http.Get(fmt.Sprintf("%s/sse", testServer.URL))
				if err != nil {
					t.Errorf(
						"Session %d: Failed to connect to SSE endpoint: %v",
						sessionNum,
						err,
					)
					return
				}
				defer sseResp.Body.Close()

				// Read the endpoint event
				buf := make([]byte, 1024)
				n, err := sseResp.Body.Read(buf)
				if err != nil {
					t.Errorf(
						"Session %d: Failed to read SSE response: %v",
						sessionNum,
						err,
					)
					return
				}

				endpointEvent := string(buf[:n])
				messageURL := strings.TrimSpace(
					strings.Split(strings.Split(endpointEvent, "data: ")[1], "\n")[0],
				)

				// Send initialize request
				initRequest := map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      sessionNum,
					"method":  "initialize",
					"params": map[string]interface{}{
						"protocolVersion": "2024-11-05",
						"clientInfo": map[string]interface{}{
							"name": fmt.Sprintf(
								"test-client-%d",
								sessionNum,
							),
							"version": "1.0.0",
						},
					},
				}

				requestBody, err := json.Marshal(initRequest)
				if err != nil {
					t.Errorf(
						"Session %d: Failed to marshal request: %v",
						sessionNum,
						err,
					)
					return
				}

				resp, err := http.Post(
					messageURL,
					"application/json",
					bytes.NewBuffer(requestBody),
				)
				if err != nil {
					t.Errorf(
						"Session %d: Failed to send message: %v",
						sessionNum,
						err,
					)
					return
				}
				defer resp.Body.Close()

				var response map[string]interface{}
				if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
					t.Errorf(
						"Session %d: Failed to decode response: %v",
						sessionNum,
						err,
					)
					return
				}

				if response["id"].(float64) != float64(sessionNum) {
					t.Errorf(
						"Session %d: Expected id %d, got %v",
						sessionNum,
						sessionNum,
						response["id"],
					)
				}
			}(i)
		}

		// Wait with timeout
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// All sessions completed successfully
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for sessions to complete")
		}
	})

	t.Run("Can be used as http.Handler", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		sseServer := NewSSEServer(mcpServer, WithBaseURL("http://localhost:8080"))

		ts := httptest.NewServer(sseServer)
		defer ts.Close()

		// Test 404 for unknown path first (simpler case)
		resp, err := http.Get(fmt.Sprintf("%s/unknown", ts.URL))
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", resp.StatusCode)
		}

		// Test SSE endpoint with proper cleanup
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/sse", ts.URL), nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to connect to SSE endpoint: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		// Read initial message in goroutine
		done := make(chan struct{})
		go func() {
			defer close(done)
			buf := make([]byte, 1024)
			_, err := resp.Body.Read(buf)
			if err != nil && err.Error() != "context canceled" {
				t.Errorf("Failed to read from SSE stream: %v", err)
			}
		}()

		// Wait briefly for initial response then cancel
		time.Sleep(100 * time.Millisecond)
		cancel()
		<-done
	})

	t.Run("Works with middleware", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		sseServer := NewSSEServer(mcpServer, WithBaseURL("http://localhost:8080"))

		middleware := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Test", "middleware")
				next.ServeHTTP(w, r)
			})
		}

		ts := httptest.NewServer(middleware(sseServer))
		defer ts.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/sse", ts.URL), nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to connect to SSE endpoint: %v", err)
		}
		defer resp.Body.Close()

		if resp.Header.Get("X-Test") != "middleware" {
			t.Error("Middleware header not found")
		}

		// Read initial message in goroutine
		done := make(chan struct{})
		go func() {
			defer close(done)
			buf := make([]byte, 1024)
			_, err := resp.Body.Read(buf)
			if err != nil && err.Error() != "context canceled" {
				t.Errorf("Failed to read from SSE stream: %v", err)
			}
		}()

		// Wait briefly then cancel
		time.Sleep(100 * time.Millisecond)
		cancel()
		<-done
	})

	t.Run("Works with custom mux", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		sseServer := NewSSEServer(mcpServer)

		mux := http.NewServeMux()
		mux.Handle("/mcp/", http.StripPrefix("/mcp", sseServer))

		ts := httptest.NewServer(mux)
		defer ts.Close()

		sseServer.baseURL = ts.URL + "/mcp"

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/mcp/sse", ts.URL), nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to connect to SSE endpoint: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		// Read the endpoint event
		buf := make([]byte, 1024)
		n, err := resp.Body.Read(buf)
		if err != nil {
			t.Fatalf("Failed to read SSE response: %v", err)
		}

		endpointEvent := string(buf[:n])
		messageURL := strings.TrimSpace(
			strings.Split(strings.Split(endpointEvent, "data: ")[1], "\n")[0],
		)

		// The messageURL should already be correct since we set the baseURL correctly
		// Test message endpoint
		initRequest := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"clientInfo": map[string]interface{}{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}
		requestBody, _ := json.Marshal(initRequest)

		resp, err = http.Post(messageURL, "application/json", bytes.NewBuffer(requestBody))
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusAccepted {
			t.Errorf("Expected status 202, got %d", resp.StatusCode)
		}

		// Clean up SSE connection
		cancel()
	})

	t.Run("works as http.Handler with custom basePath", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		sseServer := NewSSEServer(mcpServer, WithBasePath("/mcp"))

		ts := httptest.NewServer(sseServer)
		defer ts.Close()

		// Test 404 for unknown path first (simpler case)
		resp, err := http.Get(fmt.Sprintf("%s/sse", ts.URL))
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", resp.StatusCode)
		}

		// Test SSE endpoint with proper cleanup
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sseURL := fmt.Sprintf("%s/sse", ts.URL+sseServer.basePath)
		req, err := http.NewRequestWithContext(ctx, "GET", sseURL, nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to connect to SSE endpoint: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		// Read initial message in goroutine
		done := make(chan struct{})
		go func() {
			defer close(done)
			buf := make([]byte, 1024)
			_, err := resp.Body.Read(buf)
			if err != nil && err.Error() != "context canceled" {
				t.Errorf("Failed to read from SSE stream: %v", err)
			}
		}()

		// Wait briefly for initial response then cancel
		time.Sleep(100 * time.Millisecond)
		cancel()
		<-done
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
		// Create a context function that sets a test value from the request.
		// In real life this could be used to send configuration using headers
		// or query parameters.
		const testHeader = "X-Test-Header"
		setTestValFromRequest := func(ctx context.Context, r *http.Request) context.Context {
			return context.WithValue(ctx, testContextKey{}, r.Header.Get(testHeader))
		}

		mcpServer := NewMCPServer("test", "1.0.0",
			WithResourceCapabilities(true, true),
		)
		// Add a tool which uses the context function.
		mcpServer.AddTool(mcp.NewTool("test_tool"), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// Note this is agnostic to the transport type i.e. doesn't know about request headers.
			testVal := testValFromContext(ctx)
			return mcp.NewToolResultText(testVal), nil
		})

		testServer := NewTestServer(mcpServer, WithSSEContextFunc(setTestValFromRequest))
		defer testServer.Close()

		// Connect to SSE endpoint
		sseResp, err := http.Get(fmt.Sprintf("%s/sse", testServer.URL))
		if err != nil {
			t.Fatalf("Failed to connect to SSE endpoint: %v", err)
		}
		defer sseResp.Body.Close()

		// Read the endpoint event
		buf := make([]byte, 1024)
		n, err := sseResp.Body.Read(buf)
		if err != nil {
			t.Fatalf("Failed to read SSE response: %v", err)
		}

		endpointEvent := string(buf[:n])
		messageURL := strings.TrimSpace(
			strings.Split(strings.Split(endpointEvent, "data: ")[1], "\n")[0],
		)

		// Send initialize request
		initRequest := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"clientInfo": map[string]interface{}{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}

		requestBody, err := json.Marshal(initRequest)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		resp, err := http.Post(
			messageURL,
			"application/json",
			bytes.NewBuffer(requestBody),
		)

		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusAccepted {
			t.Errorf("Expected status 202, got %d", resp.StatusCode)
		}

		// Verify response
		var response map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if response["jsonrpc"] != "2.0" {
			t.Errorf("Expected jsonrpc 2.0, got %v", response["jsonrpc"])
		}
		if response["id"].(float64) != 1 {
			t.Errorf("Expected id 1, got %v", response["id"])
		}

		// Call the tool.
		toolRequest := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "tools/call",
			"params": map[string]interface{}{
				"name": "test_tool",
			},
		}
		requestBody, err = json.Marshal(toolRequest)
		if err != nil {
			t.Fatalf("Failed to marshal tool request: %v", err)
		}

		req, err := http.NewRequest(http.MethodPost, messageURL, bytes.NewBuffer(requestBody))
		if err != nil {
			t.Fatalf("Failed to create tool request: %v", err)
		}
		// Set the test header to a custom value.
		req.Header.Set(testHeader, "test_value")

		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to call tool: %v", err)
		}
		defer resp.Body.Close()

		response = make(map[string]interface{})
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if response["jsonrpc"] != "2.0" {
			t.Errorf("Expected jsonrpc 2.0, got %v", response["jsonrpc"])
		}
		if response["id"].(float64) != 2 {
			t.Errorf("Expected id 2, got %v", response["id"])
		}
		if response["result"].(map[string]interface{})["content"].([]interface{})[0].(map[string]interface{})["text"] != "test_value" {
			t.Errorf("Expected result 'test_value', got %v", response["result"])
		}
		if response["error"] != nil {
			t.Errorf("Expected no error, got %v", response["error"])
		}
	})

}
