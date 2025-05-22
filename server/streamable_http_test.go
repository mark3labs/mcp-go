package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

type jsonRPCResponse struct {
	ID     int               `json:"id"`
	Result map[string]any    `json:"result"`
	Error  *mcp.JSONRPCError `json:"error"`
}

var initRequest = map[string]any{
	"jsonrpc": "2.0",
	"id":      1,
	"method":  "initialize",
	"params": map[string]any{
		"protocolVersion": "2025-03-26",
		"clientInfo": map[string]any{
			"name":    "test-client",
			"version": "1.0.0",
		},
	},
}

func addSSETool(mcpServer *MCPServer) {
	mcpServer.AddTool(mcp.Tool{
		Name: "sseTool",
	}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Send notification to client
		server := ServerFromContext(ctx)
		for i := 0; i < 10; i++ {
			_ = server.SendNotificationToClient(ctx, "test/notification", map[string]any{
				"value": i,
			})
			time.Sleep(10 * time.Millisecond)
		}
		// send final response
		return mcp.NewToolResultText("done"), nil
	})
}

func TestStreamableHTTPServerBasic(t *testing.T) {
	t.Run("Can instantiate", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		httpServer := NewStreamableHTTPServer(mcpServer,
			WithEndpointPath("/mcp"),
		)

		if httpServer == nil {
			t.Error("SSEServer should not be nil")
		} else {
			if httpServer.server == nil {
				t.Error("MCPServer should not be nil")
			}
			if httpServer.endpointPath != "/mcp" {
				t.Errorf(
					"Expected endpointPath /mcp, got %s",
					httpServer.endpointPath,
				)
			}
		}
	})
}

func TestStreamableHTTP_POST_InvalidContent(t *testing.T) {
	mcpServer := NewMCPServer("test-mcp-server", "1.0")
	addSSETool(mcpServer)
	server := NewTestStreamableHTTPServer(mcpServer)

	t.Run("Invalid content type", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "text/plain") // Invalid type

		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
		bodyBytes, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(bodyBytes), "Invalid content type") {
			t.Errorf("Expected error message, got %s", string(bodyBytes))
		}
	})

	t.Run("Invalid JSON", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("{invalid json"))
		req.Header.Set("Content-Type", "application/json")

		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
		bodyBytes, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(bodyBytes), "jsonrpc") {
			t.Errorf("Expected error message, got %s", string(bodyBytes))
		}
		if !strings.Contains(string(bodyBytes), "not valid json") {
			t.Errorf("Expected error message, got %s", string(bodyBytes))
		}
	})
}

func TestStreamableHTTP_POST_SendAndReceive(t *testing.T) {
	mcpServer := NewMCPServer("test-mcp-server", "1.0")
	addSSETool(mcpServer)
	server := NewTestStreamableHTTPServer(mcpServer)
	var sessionID string

	t.Run("initialize", func(t *testing.T) {

		// Send initialize request
		resp, err := postJSON(server.URL, initRequest)
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
		bodyBytes, _ := io.ReadAll(resp.Body)
		var responseMessage jsonRPCResponse
		if err := json.Unmarshal(bodyBytes, &responseMessage); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}
		if responseMessage.Result["protocolVersion"] != "2025-03-26" {
			t.Errorf("Expected protocol version 2025-03-26, got %s", responseMessage.Result["protocolVersion"])
		}

		// get session id from header
		sessionID = resp.Header.Get(headerKeySessionID)
		if sessionID == "" {
			t.Fatalf("Expected session id in header, got %s", sessionID)
		}
	})

	t.Run("Send and receive message", func(t *testing.T) {
		// send ping message
		pingMessage := map[string]any{
			"jsonrpc": "2.0",
			"id":      123,
			"method":  "ping",
			"params":  map[string]any{},
		}
		pingMessageBody, _ := json.Marshal(pingMessage)
		req, err := http.NewRequest("POST", server.URL, bytes.NewBuffer(pingMessageBody))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(headerKeySessionID, sessionID)

		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		if resp.Header.Get("content-type") != "application/json" {
			t.Errorf("Expected content-type application/json, got %s", resp.Header.Get("content-type"))
		}

		// read response
		responseBody, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response: %v", err)
		}
		var response map[string]any
		if err := json.Unmarshal(responseBody, &response); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}
		if response["id"].(float64) != 123 {
			t.Errorf("Expected id 123, got %v", response["id"])
		}
	})

	t.Run("Send notification", func(t *testing.T) {
		// send notification
		notification := mcp.JSONRPCNotification{
			JSONRPC: "2.0",
			Notification: mcp.Notification{
				Method: "testNotification",
				Params: mcp.NotificationParams{
					AdditionalFields: map[string]interface{}{"param1": "value1"},
				},
			},
		}
		rawNotification, _ := json.Marshal(notification)

		req, _ := http.NewRequest(http.MethodPost, server.URL, bytes.NewBuffer(rawNotification))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(headerKeySessionID, sessionID)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusAccepted {
			t.Errorf("Expected status 202, got %d", resp.StatusCode)
		}
		bodyBytes, _ := io.ReadAll(resp.Body)
		if len(bodyBytes) > 0 {
			t.Errorf("Expected empty body, got %s", string(bodyBytes))
		}
	})

	t.Run("Invalid session id", func(t *testing.T) {
		// send ping message
		pingMessage := map[string]any{
			"jsonrpc": "2.0",
			"id":      123,
			"method":  "ping",
			"params":  map[string]any{},
		}
		pingMessageBody, _ := json.Marshal(pingMessage)
		req, err := http.NewRequest("POST", server.URL, bytes.NewBuffer(pingMessageBody))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(headerKeySessionID, "dummy-session-id")

		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 400 {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})

	t.Run("response with sse", func(t *testing.T) {

		callToolRequest := map[string]any{
			"jsonrpc": "2.0",
			"id":      123,
			"method":  "tools/call",
			"params": map[string]any{
				"name": "sseTool",
			},
		}
		callToolRequestBody, _ := json.Marshal(callToolRequest)
		req, err := http.NewRequest("POST", server.URL, bytes.NewBuffer(callToolRequestBody))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(headerKeySessionID, sessionID)

		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusAccepted {
			t.Errorf("Expected status 202, got %d", resp.StatusCode)
		}
		if resp.Header.Get("content-type") != "text/event-stream" {
			t.Errorf("Expected content-type text/event-stream, got %s", resp.Header.Get("content-type"))
		}

		// response should close finally
		responseBody, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response: %v", err)
		}
		if !strings.Contains(string(responseBody), "data:") {
			t.Errorf("Expected SSE response, got %s", string(responseBody))
		}

		// read sse
		// test there's 10 "test/notification" in the response
		if count := strings.Count(string(responseBody), "test/notification"); count != 10 {
			t.Errorf("Expected 10 test/notification, got %d", count)
		}
		for i := 0; i < 10; i++ {
			if !strings.Contains(string(responseBody), fmt.Sprintf("{\"value\":%d}", i)) {
				t.Errorf("Expected test/notification with value %d, got %s", i, string(responseBody))
			}
		}
		// get last line
		lines := strings.Split(strings.TrimSpace(string(responseBody)), "\n")
		lastLine := lines[len(lines)-1]
		if !strings.Contains(lastLine, "id") || !strings.Contains(lastLine, "done") {
			t.Errorf("Expected id and done in last line, got %s", lastLine)
		}
	})
}

func TestStreamableHTTP_POST_SendAndReceive_stateless(t *testing.T) {
	mcpServer := NewMCPServer("test-mcp-server", "1.0")
	server := NewTestStreamableHTTPServer(mcpServer, WithStateLess(true))

	t.Run("initialize", func(t *testing.T) {

		// Send initialize request
		resp, err := postJSON(server.URL, initRequest)
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
		bodyBytes, _ := io.ReadAll(resp.Body)
		var responseMessage jsonRPCResponse
		if err := json.Unmarshal(bodyBytes, &responseMessage); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}
		if responseMessage.Result["protocolVersion"] != "2025-03-26" {
			t.Errorf("Expected protocol version 2025-03-26, got %s", responseMessage.Result["protocolVersion"])
		}

		// no session id from header
		sessionID := resp.Header.Get(headerKeySessionID)
		if sessionID != "" {
			t.Fatalf("Expected no session id in header, got %s", sessionID)
		}
	})

	t.Run("Send and receive message", func(t *testing.T) {
		// send ping message
		pingMessage := map[string]any{
			"jsonrpc": "2.0",
			"id":      123,
			"method":  "ping",
			"params":  map[string]any{},
		}
		pingMessageBody, _ := json.Marshal(pingMessage)
		req, err := http.NewRequest("POST", server.URL, bytes.NewBuffer(pingMessageBody))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		// read response
		responseBody, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response: %v", err)
		}
		var response map[string]any
		if err := json.Unmarshal(responseBody, &response); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}
		if response["id"].(float64) != 123 {
			t.Errorf("Expected id 123, got %v", response["id"])
		}
	})

	t.Run("Send notification", func(t *testing.T) {
		// send notification
		notification := mcp.JSONRPCNotification{
			JSONRPC: "2.0",
			Notification: mcp.Notification{
				Method: "testNotification",
				Params: mcp.NotificationParams{
					AdditionalFields: map[string]interface{}{"param1": "value1"},
				},
			},
		}
		rawNotification, _ := json.Marshal(notification)

		req, _ := http.NewRequest(http.MethodPost, server.URL, bytes.NewBuffer(rawNotification))
		req.Header.Set("Content-Type", "application/json")
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusAccepted {
			t.Errorf("Expected status 202, got %d", resp.StatusCode)
		}
		bodyBytes, _ := io.ReadAll(resp.Body)
		if len(bodyBytes) > 0 {
			t.Errorf("Expected empty body, got %s", string(bodyBytes))
		}
	})

	t.Run("Invalid session id", func(t *testing.T) {
		// send ping message
		pingMessage := map[string]any{
			"jsonrpc": "2.0",
			"id":      123,
			"method":  "ping",
			"params":  map[string]any{},
		}
		pingMessageBody, _ := json.Marshal(pingMessage)
		req, err := http.NewRequest("POST", server.URL, bytes.NewBuffer(pingMessageBody))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(headerKeySessionID, "dummy-session-id")

		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 400 {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})
}

func TestStreamableHTTP_GET(t *testing.T) {
	mcpServer := NewMCPServer("test-mcp-server", "1.0")
	addSSETool(mcpServer)
	server := NewTestStreamableHTTPServer(mcpServer)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "text/event-stream")

	go func() {
		time.Sleep(10 * time.Millisecond)
		mcpServer.SendNotificationToAllClients("test/notification", map[string]any{
			"value": "all clients",
		})
		time.Sleep(10 * time.Millisecond)
	}()

	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("Expected status 202, got %d", resp.StatusCode)
	}

	if resp.Header.Get("content-type") != "text/event-stream" {
		t.Errorf("Expected content-type text/event-stream, got %s", resp.Header.Get("content-type"))
	}

	reader := bufio.NewReader(resp.Body)
	_, _ = reader.ReadBytes('\n') // skip first line for event type
	bodyBytes, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("Failed to read response: %v, bytes: %s", err, string(bodyBytes))
	}
	if !strings.Contains(string(bodyBytes), "all clients") {
		t.Errorf("Expected all clients, got %s", string(bodyBytes))
	}
}

func TestStreamableHTTP_HttpHandler(t *testing.T) {
	t.Run("Works with custom mux", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		server := NewStreamableHTTPServer(mcpServer)

		mux := http.NewServeMux()
		mux.Handle("/mypath", server)

		ts := httptest.NewServer(mux)
		defer ts.Close()

		// Send initialize request
		initRequest := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]any{
				"protocolVersion": "2025-03-26",
				"clientInfo": map[string]any{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}

		resp, err := postJSON(ts.URL+"/mypath", initRequest)
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
		bodyBytes, _ := io.ReadAll(resp.Body)
		var responseMessage jsonRPCResponse
		if err := json.Unmarshal(bodyBytes, &responseMessage); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}
		if responseMessage.Result["protocolVersion"] != "2025-03-26" {
			t.Errorf("Expected protocol version 2025-03-26, got %s", responseMessage.Result["protocolVersion"])
		}
	})
}

func TestStreamableHTTP_SessionWithTools(t *testing.T) {

	t.Run("SessionWithTools implementation", func(t *testing.T) {
		// Create hooks to track sessions
		hooks := &Hooks{}
		var registeredSession *streamableHttpSession
		var mu sync.Mutex
		var sessionRegistered sync.WaitGroup
		sessionRegistered.Add(1)

		hooks.AddOnRegisterSession(func(ctx context.Context, session ClientSession) {
			if s, ok := session.(*streamableHttpSession); ok {
				mu.Lock()
				registeredSession = s
				mu.Unlock()
				sessionRegistered.Done()
			}
		})

		mcpServer := NewMCPServer("test", "1.0.0", WithHooks(hooks))
		testServer := NewTestStreamableHTTPServer(mcpServer)
		defer testServer.Close()

		// send initialize request to trigger the session registration
		resp, err := postJSON(testServer.URL, initRequest)
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp.Body.Close()

		// Watch the notification to ensure the session is registered
		// (Normal http request (post) will not trigger the session registration)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		go func() {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, testServer.URL, nil)
			req.Header.Set("Content-Type", "text/event-stream")
			getResp, err := http.DefaultClient.Do(req)
			if err != nil {
				fmt.Printf("Failed to get: %v\n", err)
				return
			}
			defer getResp.Body.Close()
		}()

		// Verify we got a session
		sessionRegistered.Wait()
		mu.Lock()
		if registeredSession == nil {
			mu.Unlock()
			t.Fatal("Session was not registered via hook")
		}
		mu.Unlock()

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
}

func postJSON(url string, bodyObject any) (*http.Response, error) {
	jsonBody, _ := json.Marshal(bodyObject)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}
