package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
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
		if sseServer.baseURL != "http://localhost:8080" {
			t.Errorf(
				"Expected baseURL http://localhost:8080, got %s",
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
		mux.Handle("/mcp/", sseServer)

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

	t.Run("test useFullURLForMessageEndpoint", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		sseServer := NewSSEServer(mcpServer)

		mux := http.NewServeMux()
		mux.Handle("/mcp/", sseServer)

		ts := httptest.NewServer(mux)
		defer ts.Close()

		sseServer.baseURL = ts.URL + "/mcp"
		sseServer.useFullURLForMessageEndpoint = false
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/sse", sseServer.baseURL), nil)
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
		if !strings.HasPrefix(messageURL, sseServer.messageEndpoint) {
			t.Errorf("Expected messageURL to be %s, got %s", sseServer.messageEndpoint, messageURL)
		}

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

		resp, err = http.Post(sseServer.baseURL+messageURL, "application/json", bytes.NewBuffer(requestBody))
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

	t.Run("SSEOption should not have negative effects when used repeatedly but should always remain consistent.", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		basePath := "/mcp-test"
		baseURL := "http://localhost:8080/test"
		messageEndpoint := "/message-test"
		sseEndpoint := "/sse-test"
		useFullURLForMessageEndpoint := false
		srv := &http.Server{}
		rands := []SSEOption{
			WithBasePath(basePath),
			WithBaseURL(baseURL),
			WithMessageEndpoint(messageEndpoint),
			WithUseFullURLForMessageEndpoint(useFullURLForMessageEndpoint),
			WithSSEEndpoint(sseEndpoint),
			WithHTTPServer(srv),
		}
		for i := 0; i < 100; i++ {

			var options []SSEOption
			for i2 := 0; i2 < 100; i2++ {
				index := rand.Intn(len(rands))
				options = append(options, rands[index])
			}
			sseServer := NewSSEServer(mcpServer, options...)

			if sseServer.basePath != basePath {
				t.Fatalf("basePath %v, got: %v", basePath, sseServer.basePath)
			}
			if sseServer.useFullURLForMessageEndpoint != useFullURLForMessageEndpoint {
				t.Fatalf("useFullURLForMessageEndpoint %v, got: %v", useFullURLForMessageEndpoint, sseServer.useFullURLForMessageEndpoint)
			}

			if sseServer.baseURL != baseURL {
				t.Fatalf("baseURL %v, got: %v", baseURL, sseServer.baseURL)
			}

			if sseServer.sseEndpoint != sseEndpoint {
				t.Fatalf("sseEndpoint %v, got: %v", sseEndpoint, sseServer.sseEndpoint)
			}

			if sseServer.messageEndpoint != messageEndpoint {
				t.Fatalf("messageEndpoint  %v, got: %v", messageEndpoint, sseServer.messageEndpoint)
			}

			if sseServer.srv != srv {
				t.Fatalf("srv  %v, got: %v", srv, sseServer.srv)
			}
		}
	})

	t.Run("Client receives and can respond to ping messages", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		testServer := NewTestServer(mcpServer,
			WithKeepAlive(true),
			WithKeepAliveInterval(50*time.Millisecond),
		)
		defer testServer.Close()

		sseResp, err := http.Get(fmt.Sprintf("%s/sse", testServer.URL))
		if err != nil {
			t.Fatalf("Failed to connect to SSE endpoint: %v", err)
		}
		defer sseResp.Body.Close()

		reader := bufio.NewReader(sseResp.Body)

		var messageURL string
		var pingID float64

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("Failed to read SSE event: %v", err)
			}

			if strings.HasPrefix(line, "event: endpoint") {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					t.Fatalf("Failed to read endpoint data: %v", err)
				}
				messageURL = strings.TrimSpace(strings.TrimPrefix(dataLine, "data: "))

				_, err = reader.ReadString('\n')
				if err != nil {
					t.Fatalf("Failed to read blank line: %v", err)
				}
			}

			if strings.HasPrefix(line, "event: message") {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					t.Fatalf("Failed to read message data: %v", err)
				}

				pingData := strings.TrimSpace(strings.TrimPrefix(dataLine, "data:"))
				var pingMsg mcp.JSONRPCRequest
				if err := json.Unmarshal([]byte(pingData), &pingMsg); err != nil {
					t.Fatalf("Failed to parse ping message: %v", err)
				}

				if pingMsg.Method == "ping" {
					pingID = pingMsg.ID.(float64)
					t.Logf("Received ping with ID: %f", pingID)
					break // We got the ping, exit the loop
				}

				_, err = reader.ReadString('\n')
				if err != nil {
					t.Fatalf("Failed to read blank line: %v", err)
				}
			}

			if messageURL != "" && pingID != 0 {
				break
			}
		}

		if messageURL == "" {
			t.Fatal("Did not receive message endpoint URL")
		}

		pingResponse := map[string]any{
			"jsonrpc": "2.0",
			"id":      pingID,
			"result":  map[string]any{},
		}

		requestBody, err := json.Marshal(pingResponse)
		if err != nil {
			t.Fatalf("Failed to marshal ping response: %v", err)
		}

		resp, err := http.Post(
			messageURL,
			"application/json",
			bytes.NewBuffer(requestBody),
		)
		if err != nil {
			t.Fatalf("Failed to send ping response: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusAccepted {
			t.Errorf("Expected status 202 for ping response, got %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		if len(body) > 0 {
			var response map[string]any
			if err := json.Unmarshal(body, &response); err != nil {
				t.Fatalf("Failed to parse response body: %v", err)
			}

			if response["error"] != nil {
				t.Errorf("Expected no error in response, got %v", response["error"])
			}
		}
	})
}
