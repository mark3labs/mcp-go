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
	"github.com/stretchr/testify/require"
)

func TestSSEServer(t *testing.T) {
	t.Run("Can instantiate", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		sseServer := NewSSEServer(mcpServer,
			WithBaseURL("http://localhost:8080"),
			WithStaticBasePath("/mcp"),
		)

		if sseServer == nil {
			t.Error("SSEServer should not be nil")
			return
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
		endpointEvent, err := readSSEEvent(sseResp)
		if err != nil {
			t.Fatalf("Failed to read SSE response: %v", err)
		}
		if !strings.Contains(endpointEvent, "event: endpoint") {
			t.Fatalf("Expected endpoint event, got: %s", endpointEvent)
		}

		// Extract message endpoint URL
		messageURL := strings.TrimSpace(
			strings.Split(strings.Split(endpointEvent, "data: ")[1], "\n")[0],
		)

		// Send initialize request
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
				initRequest := map[string]any{
					"jsonrpc": "2.0",
					"id":      sessionNum,
					"method":  "initialize",
					"params": map[string]any{
						"protocolVersion": "2024-11-05",
						"clientInfo": map[string]any{
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

				endpointEvent, err = readSSEEvent(sseResp)
				if err != nil {
					t.Errorf("Failed to read SSE response: %v", err)
					return
				}
				respFromSee := strings.TrimSpace(
					strings.Split(strings.Split(endpointEvent, "data: ")[1], "\n")[0],
				)

				var response map[string]any
				if err := json.NewDecoder(strings.NewReader(respFromSee)).Decode(&response); err != nil {
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

		// Read the endpoint event using a bufio.Reader loop to ensure we get the full SSE frame
		reader := bufio.NewReader(resp.Body)
		var endpointEvent strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("Failed to read SSE response: %v", err)
			}
			endpointEvent.WriteString(line)
			if line == "\n" || line == "\r\n" {
				break // End of SSE frame
			}
		}
		endpointEventStr := endpointEvent.String()
		if !strings.Contains(endpointEventStr, "event: endpoint") {
			t.Fatalf("Expected endpoint event, got: %s", endpointEventStr)
		}
		// Extract message endpoint and check correctness
		messageURL := strings.TrimSpace(strings.Split(strings.Split(endpointEventStr, "data: ")[1], "\n")[0])
		if !strings.HasPrefix(messageURL, sseServer.messageEndpoint) {
			t.Errorf("Expected messageURL to be %s, got %s", sseServer.messageEndpoint, messageURL)
		}

		// The messageURL should already be correct since we set the baseURL correctly
		// Test message endpoint
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
		sseServer := NewSSEServer(mcpServer, WithStaticBasePath("/mcp"))

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
		endpointEvent, err := readSSEEvent(sseResp)
		if err != nil {
			t.Fatalf("Failed to read SSE response: %v", err)
		}
		messageURL := strings.TrimSpace(
			strings.Split(strings.Split(endpointEvent, "data: ")[1], "\n")[0],
		)

		// Send initialize request
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
		endpointEvent, err = readSSEEvent(sseResp)
		if err != nil {
			t.Fatalf("Failed to read SSE response: %v", err)
		}
		respFromSSE := strings.TrimSpace(
			strings.Split(strings.Split(endpointEvent, "data: ")[1], "\n")[0],
		)

		var response map[string]any
		if err := json.NewDecoder(strings.NewReader(respFromSSE)).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if response["jsonrpc"] != "2.0" {
			t.Errorf("Expected jsonrpc 2.0, got %v", response["jsonrpc"])
		}
		if response["id"].(float64) != 1 {
			t.Errorf("Expected id 1, got %v", response["id"])
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
		requestBody, err = json.Marshal(toolRequest)
		if err != nil {
			t.Fatalf("Failed to marshal tool request: %v", err)
		}

		var req *http.Request
		req, err = http.NewRequest(http.MethodPost, messageURL, bytes.NewBuffer(requestBody))
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

		endpointEvent, err = readSSEEvent(sseResp)
		if err != nil {
			t.Fatalf("Failed to read SSE response: %v", err)
		}

		respFromSSE = strings.TrimSpace(
			strings.Split(strings.Split(endpointEvent, "data: ")[1], "\n")[0],
		)

		response = make(map[string]any)
		if err := json.NewDecoder(strings.NewReader(respFromSSE)).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
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
			WithStaticBasePath(basePath),
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
					idValue, ok := pingMsg.ID.Value().(int64)
					if ok {
						pingID = float64(idValue)
					} else {
						floatValue, ok := pingMsg.ID.Value().(float64)
						if !ok {
							t.Fatalf("Expected ping ID to be number, got %T: %v", pingMsg.ID.Value(), pingMsg.ID.Value())
						}
						pingID = floatValue
					}
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

	t.Run("TestSSEHandlerWithDynamicMounting", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		// MessageEndpointFunc that extracts tenant from the path using Go 1.22+ PathValue

		sseServer := NewSSEServer(
			mcpServer,
			WithDynamicBasePath(func(r *http.Request, sessionID string) string {
				tenant := r.PathValue("tenant")
				return "/mcp/" + tenant
			}),
		)

		mux := http.NewServeMux()
		mux.Handle("/mcp/{tenant}/sse", sseServer.SSEHandler())
		mux.Handle("/mcp/{tenant}/message", sseServer.MessageHandler())

		ts := httptest.NewServer(mux)
		defer ts.Close()

		// Use a dynamic tenant
		tenant := "tenant123"
		// Connect to SSE endpoint
		req, _ := http.NewRequest("GET", ts.URL+"/mcp/"+tenant+"/sse", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to connect to SSE endpoint: %v", err)
		}
		defer resp.Body.Close()

		reader := bufio.NewReader(resp.Body)
		var endpointEvent strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("Failed to read SSE response: %v", err)
			}
			endpointEvent.WriteString(line)
			if line == "\n" || line == "\r\n" {
				break // End of SSE frame
			}
		}
		endpointEventStr := endpointEvent.String()
		if !strings.Contains(endpointEventStr, "event: endpoint") {
			t.Fatalf("Expected endpoint event, got: %s", endpointEventStr)
		}
		// Extract message endpoint and check correctness
		messageURL := strings.TrimSpace(strings.Split(strings.Split(endpointEventStr, "data: ")[1], "\n")[0])
		if !strings.HasPrefix(messageURL, "/mcp/"+tenant+"/message") {
			t.Errorf("Expected message endpoint to start with /mcp/%s/message, got %s", tenant, messageURL)
		}

		// Optionally, test sending a message to the message endpoint
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
		requestBody, err := json.Marshal(initRequest)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		// The message endpoint is relative, so prepend the test server URL
		fullMessageURL := ts.URL + messageURL
		resp2, err := http.Post(fullMessageURL, "application/json", bytes.NewBuffer(requestBody))
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp2.Body.Close()

		if resp2.StatusCode != http.StatusAccepted {
			t.Errorf("Expected status 202, got %d", resp2.StatusCode)
		}

		// Read the response from the SSE stream
		reader = bufio.NewReader(resp.Body)
		var initResponse strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("Failed to read SSE response: %v", err)
			}
			initResponse.WriteString(line)
			if line == "\n" || line == "\r\n" {
				break // End of SSE frame
			}
		}
		initResponseStr := initResponse.String()
		if !strings.Contains(initResponseStr, "event: message") {
			t.Fatalf("Expected message event, got: %s", initResponseStr)
		}

		// Extract and parse the response data
		respData := strings.TrimSpace(strings.Split(strings.Split(initResponseStr, "data: ")[1], "\n")[0])
		var response map[string]any
		if err := json.NewDecoder(strings.NewReader(respData)).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if response["jsonrpc"] != "2.0" {
			t.Errorf("Expected jsonrpc 2.0, got %v", response["jsonrpc"])
		}
		if response["id"].(float64) != 1 {
			t.Errorf("Expected id 1, got %v", response["id"])
		}
	})
	t.Run("TestSSEHandlerRequiresDynamicBasePath", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		sseServer := NewSSEServer(mcpServer)
		require.NotPanics(t, func() { sseServer.SSEHandler() })
		require.NotPanics(t, func() { sseServer.MessageHandler() })

		sseServer = NewSSEServer(
			mcpServer,
			WithDynamicBasePath(func(r *http.Request, sessionID string) string {
				return "/foo"
			}),
		)
		req := httptest.NewRequest("GET", "/foo/sse", nil)
		w := httptest.NewRecorder()

		sseServer.ServeHTTP(w, req)
		require.Equal(t, http.StatusInternalServerError, w.Code)
		require.Contains(t, w.Body.String(), "ServeHTTP cannot be used with WithDynamicBasePath")
	})

	t.Run("TestCompleteSseEndpointAndMessageEndpointErrors", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		sseServer := NewSSEServer(mcpServer, WithDynamicBasePath(func(r *http.Request, sessionID string) string {
			return "/foo"
		}))

		// Test CompleteSseEndpoint
		endpoint, err := sseServer.CompleteSseEndpoint()
		require.Error(t, err)
		var dynamicPathErr *ErrDynamicPathConfig
		require.ErrorAs(t, err, &dynamicPathErr)
		require.Equal(t, "CompleteSseEndpoint", dynamicPathErr.Method)
		require.Empty(t, endpoint)

		// Test CompleteMessageEndpoint
		messageEndpoint, err := sseServer.CompleteMessageEndpoint()
		require.Error(t, err)
		require.ErrorAs(t, err, &dynamicPathErr)
		require.Equal(t, "CompleteMessageEndpoint", dynamicPathErr.Method)
		require.Empty(t, messageEndpoint)

		// Test that path methods still work and return fallback values
		ssePath := sseServer.CompleteSsePath()
		require.Equal(t, sseServer.basePath+sseServer.sseEndpoint, ssePath)

		messagePath := sseServer.CompleteMessagePath()
		require.Equal(t, sseServer.basePath+sseServer.messageEndpoint, messagePath)
	})

	t.Run("TestNormalizeURLPath", func(t *testing.T) {
		tests := []struct {
			name     string
			inputs   []string
			expected string
		}{
			// Basic path joining
			{
				name:     "empty inputs",
				inputs:   []string{"", ""},
				expected: "/",
			},
			{
				name:     "single path segment",
				inputs:   []string{"mcp"},
				expected: "/mcp",
			},
			{
				name:     "multiple path segments",
				inputs:   []string{"mcp", "api", "message"},
				expected: "/mcp/api/message",
			},

			// Leading slash handling
			{
				name:     "already has leading slash",
				inputs:   []string{"/mcp", "message"},
				expected: "/mcp/message",
			},
			{
				name:     "mixed leading slashes",
				inputs:   []string{"/mcp", "/message"},
				expected: "/mcp/message",
			},

			// Trailing slash handling
			{
				name:     "with trailing slashes",
				inputs:   []string{"mcp/", "message/"},
				expected: "/mcp/message",
			},
			{
				name:     "mixed trailing slashes",
				inputs:   []string{"mcp", "message/"},
				expected: "/mcp/message",
			},
			{
				name:     "root path",
				inputs:   []string{"/"},
				expected: "/",
			},

			// Path normalization
			{
				name:     "normalize double slashes",
				inputs:   []string{"mcp//api", "//message"},
				expected: "/mcp/api/message",
			},
			{
				name:     "normalize parent directory",
				inputs:   []string{"mcp/parent/../child", "message"},
				expected: "/mcp/child/message",
			},
			{
				name:     "normalize current directory",
				inputs:   []string{"mcp/./api", "./message"},
				expected: "/mcp/api/message",
			},

			// Complex cases
			{
				name:     "complex mixed case",
				inputs:   []string{"/mcp/", "/api//", "message/"},
				expected: "/mcp/api/message",
			},
			{
				name:     "absolute path in second segment",
				inputs:   []string{"tenant", "/message"},
				expected: "/tenant/message",
			},
			{
				name:     "URL pattern with parameters",
				inputs:   []string{"/mcp/{tenant}", "message"},
				expected: "/mcp/{tenant}/message",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := normalizeURLPath(tt.inputs...)
				if result != tt.expected {
					t.Errorf("normalizeURLPath(%q) = %q, want %q",
						tt.inputs, result, tt.expected)
				}
			})
		}
	})

	t.Run("TestSessionWithPrompts", func(t *testing.T) {
		// Create hooks to track sessions
		hooks := &Hooks{}
		var registeredSession *sseSession
		hooks.AddOnRegisterSession(func(ctx context.Context, session ClientSession) {
			if s, ok := session.(*sseSession); ok {
				registeredSession = s
			}
		})

		mcpServer := NewMCPServer("test", "1.0.0", WithHooks(hooks))
		testServer := NewTestServer(mcpServer)
		defer testServer.Close()

		// Connect to SSE endpoint
		sseResp, err := http.Get(fmt.Sprintf("%s/sse", testServer.URL))
		if err != nil {
			t.Fatalf("Failed to connect to SSE endpoint: %v", err)
		}
		defer sseResp.Body.Close()

		// Read the endpoint event to ensure session is established
		_, err = readSSEEvent(sseResp)
		if err != nil {
			t.Fatalf("Failed to read SSE response: %v", err)
		}

		// Verify we got a session
		if registeredSession == nil {
			t.Fatal("Session was not registered via hook")
		}

		// Test setting and getting prompts
		prompts := map[string]ServerPrompt{
			"test_prompt": {
				Prompt: mcp.Prompt{
					Name:        "test_prompt",
					Description: "A test prompt",
				},
				Handler: func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
					return mcp.NewGetPromptResult("test", []mcp.PromptMessage{
						{
							Role:    mcp.RoleUser,
							Content: mcp.TextContent{Text: "test"},
						},
					}), nil
				},
			},
		}

		// Test SetSessionPrompts
		registeredSession.SetSessionPrompts(prompts)

		// Test GetSessionPrompts
		retrievedPrompts := registeredSession.GetSessionPrompts()
		if len(retrievedPrompts) != 1 {
			t.Errorf("Expected 1 prompt, got %d", len(retrievedPrompts))
		}
		if prompt, exists := retrievedPrompts["test_prompt"]; !exists {
			t.Error("Expected test_prompt to exist")
		} else if prompt.Prompt.Name != "test_prompt" {
			t.Errorf("Expected prompt name test_prompt, got %s", prompt.Prompt.Name)
		}

		// Test concurrent access
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(2)
			go func(i int) {
				defer wg.Done()
				prompts := map[string]ServerPrompt{
					fmt.Sprintf("prompt_%d", i): {
						Prompt: mcp.Prompt{
							Name:        fmt.Sprintf("prompt_%d", i),
							Description: fmt.Sprintf("Prompt %d", i),
						},
					},
				}
				registeredSession.SetSessionPrompts(prompts)
			}(i)
			go func() {
				defer wg.Done()
				_ = registeredSession.GetSessionPrompts()
			}()
		}
		wg.Wait()

		// Verify we can still get and set tools after concurrent access
		finalPrompts := map[string]ServerPrompt{
			"final_prompt": {
				Prompt: mcp.Prompt{
					Name:        "final_prompt",
					Description: "Final Prompt",
				},
			},
		}
		registeredSession.SetSessionPrompts(finalPrompts)
		retrievedPrompts = registeredSession.GetSessionPrompts()
		if len(retrievedPrompts) != 1 {
			t.Errorf("Expected 1 prompt, got %d", len(retrievedPrompts))
		}
		if _, exists := retrievedPrompts["final_prompt"]; !exists {
			t.Error("Expected final_prompt to exist")
		}
	})

	t.Run("SessionWithTools implementation", func(t *testing.T) {
		// Create hooks to track sessions
		hooks := &Hooks{}
		var registeredSession *sseSession
		hooks.AddOnRegisterSession(func(ctx context.Context, session ClientSession) {
			if s, ok := session.(*sseSession); ok {
				registeredSession = s
			}
		})

		mcpServer := NewMCPServer("test", "1.0.0", WithHooks(hooks))
		testServer := NewTestServer(mcpServer)
		defer testServer.Close()

		// Connect to SSE endpoint
		sseResp, err := http.Get(fmt.Sprintf("%s/sse", testServer.URL))
		if err != nil {
			t.Fatalf("Failed to connect to SSE endpoint: %v", err)
		}
		defer sseResp.Body.Close()

		// Read the endpoint event to ensure session is established
		_, err = readSSEEvent(sseResp)
		if err != nil {
			t.Fatalf("Failed to read SSE response: %v", err)
		}

		// Verify we got a session
		if registeredSession == nil {
			t.Fatal("Session was not registered via hook")
		}

		// Test setting and getting tools
		tools := map[string]ServerTool{
			"test_tool": {
				Tool: mcp.Tool{
					Name:        "test_tool",
					Description: "A test tool",
					Annotations: mcp.ToolAnnotation{
						Title: "Test Tool",
					},
				},
				Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return mcp.NewToolResultText("test"), nil
				},
			},
		}

		// Test SetSessionTools
		registeredSession.SetSessionTools(tools)

		// Test GetSessionTools
		retrievedTools := registeredSession.GetSessionTools()
		if len(retrievedTools) != 1 {
			t.Errorf("Expected 1 tool, got %d", len(retrievedTools))
		}
		if tool, exists := retrievedTools["test_tool"]; !exists {
			t.Error("Expected test_tool to exist")
		} else if tool.Tool.Name != "test_tool" {
			t.Errorf("Expected tool name test_tool, got %s", tool.Tool.Name)
		}

		// Test concurrent access
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(2)
			go func(i int) {
				defer wg.Done()
				tools := map[string]ServerTool{
					fmt.Sprintf("tool_%d", i): {
						Tool: mcp.Tool{
							Name:        fmt.Sprintf("tool_%d", i),
							Description: fmt.Sprintf("Tool %d", i),
							Annotations: mcp.ToolAnnotation{
								Title: fmt.Sprintf("Tool %d", i),
							},
						},
					},
				}
				registeredSession.SetSessionTools(tools)
			}(i)
			go func() {
				defer wg.Done()
				_ = registeredSession.GetSessionTools()
			}()
		}
		wg.Wait()

		// Verify we can still get and set tools after concurrent access
		finalTools := map[string]ServerTool{
			"final_tool": {
				Tool: mcp.Tool{
					Name:        "final_tool",
					Description: "Final Tool",
					Annotations: mcp.ToolAnnotation{
						Title: "Final Tool",
					},
				},
			},
		}
		registeredSession.SetSessionTools(finalTools)
		retrievedTools = registeredSession.GetSessionTools()
		if len(retrievedTools) != 1 {
			t.Errorf("Expected 1 tool, got %d", len(retrievedTools))
		}
		if _, exists := retrievedTools["final_tool"]; !exists {
			t.Error("Expected final_tool to exist")
		}
	})

	t.Run("TestServerResponseMarshalError", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0",
			WithResourceCapabilities(true, true),
			WithHooks(&Hooks{
				OnAfterInitialize: []OnAfterInitializeFunc{
					func(ctx context.Context, id any, message *mcp.InitializeRequest, result *mcp.InitializeResult) {
						result.Meta = map[string]any{"invalid": func() {}} // marshal will fail
					},
				},
			}),
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
		endpointEvent, err := readSSEEvent(sseResp)
		if err != nil {
			t.Fatalf("Failed to read SSE response: %v", err)
		}
		if !strings.Contains(endpointEvent, "event: endpoint") {
			t.Fatalf("Expected endpoint event, got: %s", endpointEvent)
		}

		// Extract message endpoint URL
		messageURL := strings.TrimSpace(
			strings.Split(strings.Split(endpointEvent, "data: ")[1], "\n")[0],
		)

		// Send initialize request
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

		endpointEvent, err = readSSEEvent(sseResp)
		if err != nil {
			t.Fatalf("Failed to read SSE response: %v", err)
		}

		if !strings.Contains(endpointEvent, "\"id\": null") {
			t.Errorf("Expected id to be null")
		}
	})

	t.Run("Message processing continues after we return back result to client", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")

		processingCompleted := make(chan struct{})
		processingStarted := make(chan struct{})

		mcpServer.AddTool(mcp.NewTool("slowMethod"), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			close(processingStarted) // signal for processing started

			select {
			case <-ctx.Done(): // If this happens, the test will fail because processingCompleted won't be closed
				return nil, fmt.Errorf("context was canceled")
			case <-time.After(1 * time.Second): // Simulate processing time
				// Successfully completed processing, now close the completed channel to signal completion
				close(processingCompleted)
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.TextContent{
							Type: "text",
							Text: "success",
						},
					},
				}, nil
			}
		})

		testServer := NewTestServer(mcpServer)
		defer testServer.Close()

		sseResp, err := http.Get(fmt.Sprintf("%s/sse", testServer.URL))
		require.NoError(t, err, "Failed to connect to SSE endpoint")
		defer sseResp.Body.Close()

		endpointEvent, err := readSSEEvent(sseResp)
		require.NoError(t, err, "Failed to read SSE response")
		require.Contains(t, endpointEvent, "event: endpoint", "Expected endpoint event")

		messageURL := strings.TrimSpace(
			strings.Split(strings.Split(endpointEvent, "data: ")[1], "\n")[0],
		)

		messageRequest := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params": map[string]any{
				"name":       "slowMethod",
				"parameters": map[string]any{},
			},
		}

		requestBody, err := json.Marshal(messageRequest)
		require.NoError(t, err, "Failed to marshal request")

		ctx, cancel := context.WithCancel(context.Background())
		req, err := http.NewRequestWithContext(ctx, "POST", messageURL, bytes.NewBuffer(requestBody))
		require.NoError(t, err, "Failed to create request")
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		require.NoError(t, err, "Failed to send message")
		defer resp.Body.Close()

		require.Equal(t, http.StatusAccepted, resp.StatusCode, "Expected status 202 Accepted")

		// Wait for processing to start
		select {
		case <-processingStarted: // Processing has started, now cancel the client context to simulate disconnection
		case <-time.After(2 * time.Second):
			t.Fatal("Timed out waiting for processing to start")
		}

		cancel() // cancel the client context to simulate disconnection

		// wait for processing to complete, if the test passes, it means the processing continued despite client disconnection
		select {
		case <-processingCompleted:
		case <-time.After(2 * time.Second):
			t.Fatal("Processing did not complete after client disconnection")
		}
	})

	t.Run("Start() then Shutdown() should not deadlock", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		sseServer := NewSSEServer(mcpServer, WithBaseURL("http://localhost:0"))

		done := make(chan struct{})

		go func() {
			_ = sseServer.Start("127.0.0.1:0")
			close(done)
		}()

		// Wait a bit to ensure the server is running
		time.Sleep(50 * time.Millisecond)

		shutdownDone := make(chan error, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		go func() {
			err := sseServer.Shutdown(ctx)
			shutdownDone <- err
		}()

		select {
		case err := <-shutdownDone:
			if ctx.Err() == context.DeadlineExceeded {
				t.Fatalf("Shutdown deadlocked (timed out): %v", err)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("Shutdown did not return in time (likely deadlocked)")
		}
	})
}

func readSSEEvent(sseResp *http.Response) (string, error) {
	buf := make([]byte, 1024)
	n, err := sseResp.Body.Read(buf)
	if err != nil {
		return "", err
	}
	return string(buf[:n]), nil
}
