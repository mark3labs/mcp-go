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
		"protocolVersion": mcp.LATEST_PROTOCOL_VERSION, "clientInfo": map[string]any{
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
		if responseMessage.Result["protocolVersion"] != mcp.LATEST_PROTOCOL_VERSION {
			t.Errorf("Expected protocol version %s, got %s", mcp.LATEST_PROTOCOL_VERSION, responseMessage.Result["protocolVersion"])
		}

		// get session id from header
		sessionID = resp.Header.Get(HeaderKeySessionID)
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
		req.Header.Set(HeaderKeySessionID, sessionID)

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
					AdditionalFields: map[string]any{"param1": "value1"},
				},
			},
		}
		rawNotification, _ := json.Marshal(notification)

		req, _ := http.NewRequest(http.MethodPost, server.URL, bytes.NewBuffer(rawNotification))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(HeaderKeySessionID, sessionID)
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
		req.Header.Set(HeaderKeySessionID, "dummy-session-id")

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
		req.Header.Set(HeaderKeySessionID, sessionID)

		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
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
		if responseMessage.Result["protocolVersion"] != mcp.LATEST_PROTOCOL_VERSION {
			t.Errorf("Expected protocol version %s, got %s", mcp.LATEST_PROTOCOL_VERSION, responseMessage.Result["protocolVersion"])
		}

		// no session id from header
		sessionID := resp.Header.Get(HeaderKeySessionID)
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
					AdditionalFields: map[string]any{"param1": "value1"},
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

	t.Run("Session id ignored in stateless mode", func(t *testing.T) {
		// send ping message with session ID - should be ignored in stateless mode
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
		req.Header.Set(HeaderKeySessionID, "dummy-session-id")

		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp.Body.Close()

		// In stateless mode, session IDs should be ignored and request should succeed
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		// Verify the response is valid
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

	t.Run("tools/list with session id in stateless mode", func(t *testing.T) {
		// Test the specific scenario from the issue - tools/list with session ID
		toolsListMessage := map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/list",
			"id":      1,
		}
		toolsListBody, _ := json.Marshal(toolsListMessage)
		req, err := http.NewRequest("POST", server.URL, bytes.NewBuffer(toolsListBody))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(HeaderKeySessionID, "mcp-session-2c44d701-fd50-44ce-92b8-dec46185a741")

		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp.Body.Close()

		// Should succeed in stateless mode even with session ID
		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Errorf("Expected status 200, got %d. Response: %s", resp.StatusCode, string(bodyBytes))
		}

		// Verify the response is valid
		responseBody, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response: %v", err)
		}
		var response map[string]any
		if err := json.Unmarshal(responseBody, &response); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}
		if response["id"].(float64) != 1 {
			t.Errorf("Expected id 1, got %v", response["id"])
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

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
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
				"protocolVersion": mcp.LATEST_PROTOCOL_VERSION, "clientInfo": map[string]any{
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
		if responseMessage.Result["protocolVersion"] != mcp.LATEST_PROTOCOL_VERSION {
			t.Errorf("Expected protocol version %s, got %s", mcp.LATEST_PROTOCOL_VERSION, responseMessage.Result["protocolVersion"])
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

func TestStreamableHTTP_SessionWithLogging(t *testing.T) {
	t.Run("SessionWithLogging implementation", func(t *testing.T) {
		hooks := &Hooks{}
		var logSession *streamableHttpSession
		var mu sync.Mutex

		hooks.AddAfterSetLevel(func(ctx context.Context, id any, message *mcp.SetLevelRequest, result *mcp.EmptyResult) {
			if s, ok := ClientSessionFromContext(ctx).(*streamableHttpSession); ok {
				mu.Lock()
				logSession = s
				mu.Unlock()
			}
		})

		mcpServer := NewMCPServer("test", "1.0.0", WithHooks(hooks), WithLogging())
		testServer := NewTestStreamableHTTPServer(mcpServer)
		defer testServer.Close()

		// obtain a valid session ID first
		initResp, err := postJSON(testServer.URL, initRequest)
		if err != nil {
			t.Fatalf("Failed to send init request: %v", err)
		}
		defer initResp.Body.Close()
		sessionID := initResp.Header.Get(HeaderKeySessionID)
		if sessionID == "" {
			t.Fatal("Expected session id in header")
		}

		setLevelRequest := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "logging/setLevel",
			"params": map[string]any{
				"level": mcp.LoggingLevelCritical,
			},
		}

		reqBody, _ := json.Marshal(setLevelRequest)
		req, err := http.NewRequest(http.MethodPost, testServer.URL, bytes.NewBuffer(reqBody))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(HeaderKeySessionID, sessionID)

		resp, err := testServer.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		mu.Lock()
		if logSession == nil {
			mu.Unlock()
			t.Fatal("Session was not captured")
		}
		if logSession.GetLogLevel() != mcp.LoggingLevelCritical {
			t.Errorf("Expected critical level, got %v", logSession.GetLogLevel())
		}
		mu.Unlock()
	})
}

func TestStreamableHTTPServer_WithOptions(t *testing.T) {
	t.Run("WithStreamableHTTPServer sets httpServer field", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		customServer := &http.Server{Addr: ":9999"}
		httpServer := NewStreamableHTTPServer(mcpServer, WithStreamableHTTPServer(customServer))

		if httpServer.httpServer != customServer {
			t.Errorf("Expected httpServer to be set to custom server instance, got %v", httpServer.httpServer)
		}
	})

	t.Run("Start with conflicting address returns error", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		customServer := &http.Server{Addr: ":9999"}
		httpServer := NewStreamableHTTPServer(mcpServer, WithStreamableHTTPServer(customServer))

		err := httpServer.Start(":8888")
		if err == nil {
			t.Error("Expected error for conflicting address, got nil")
		} else if !strings.Contains(err.Error(), "conflicting listen address") {
			t.Errorf("Expected error message to contain 'conflicting listen address', got '%s'", err.Error())
		}
	})

	t.Run("Options consistency test", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		endpointPath := "/test-mcp"
		customServer := &http.Server{}

		// Options to test
		options := []StreamableHTTPOption{
			WithEndpointPath(endpointPath),
			WithStreamableHTTPServer(customServer),
		}

		// Apply options multiple times and verify consistency
		for i := 0; i < 10; i++ {
			server := NewStreamableHTTPServer(mcpServer, options...)

			if server.endpointPath != endpointPath {
				t.Errorf("Expected endpointPath %s, got %s", endpointPath, server.endpointPath)
			}

			if server.httpServer != customServer {
				t.Errorf("Expected httpServer to match, got %v", server.httpServer)
			}
		}
	})
}

func TestStreamableHTTP_HeaderPassthrough(t *testing.T) {
	mcpServer := NewMCPServer("test-mcp-server", "1.0")

	var receivedHeaders struct {
		contentType  string
		customHeader string
	}
	mcpServer.AddTool(
		mcp.NewTool("check-headers"),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			receivedHeaders.contentType = request.Header.Get("Content-Type")
			receivedHeaders.customHeader = request.Header.Get("X-Custom-Header")
			return mcp.NewToolResultText("ok"), nil
		},
	)

	server := NewTestStreamableHTTPServer(mcpServer)
	defer server.Close()

	// Initialize to get session
	resp, _ := postJSON(server.URL, initRequest)
	sessionID := resp.Header.Get(HeaderKeySessionID)
	resp.Body.Close()

	// Test header passthrough
	toolRequest := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "check-headers",
		},
	}
	toolBody, _ := json.Marshal(toolRequest)
	req, _ := http.NewRequest("POST", server.URL, bytes.NewReader(toolBody))

	const expectedContentType = "application/json"
	const expectedCustomHeader = "test-value"
	req.Header.Set("Content-Type", expectedContentType)
	req.Header.Set("X-Custom-Header", expectedCustomHeader)
	req.Header.Set(HeaderKeySessionID, sessionID)

	resp, _ = server.Client().Do(req)
	resp.Body.Close()

	if receivedHeaders.contentType != expectedContentType {
		t.Errorf("Expected Content-Type header '%s', got '%s'", expectedContentType, receivedHeaders.contentType)
	}
	if receivedHeaders.customHeader != expectedCustomHeader {
		t.Errorf("Expected X-Custom-Header '%s', got '%s'", expectedCustomHeader, receivedHeaders.customHeader)
	}
}

func TestStreamableHTTP_PongResponseHandling(t *testing.T) {
	// Ping/Pong does not require session ID
	// https://modelcontextprotocol.io/specification/2025-03-26/basic/utilities/ping
	mcpServer := NewMCPServer("test-mcp-server", "1.0")
	server := NewTestStreamableHTTPServer(mcpServer)
	defer server.Close()

	t.Run("Pong response with empty result should not be treated as sampling response", func(t *testing.T) {
		// According to MCP spec, pong responses have empty result: {"jsonrpc": "2.0", "id": "123", "result": {}}
		pongResponse := map[string]any{
			"jsonrpc": "2.0",
			"id":      123,
			"result":  map[string]any{},
		}

		resp, err := postJSON(server.URL, pongResponse)
		if err != nil {
			t.Fatalf("Failed to send pong response: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}
		bodyStr := string(bodyBytes)

		if strings.Contains(bodyStr, "Missing session ID for sampling response") {
			t.Errorf("Pong response was incorrectly detected as sampling response. Response: %s", bodyStr)
		}
		if strings.Contains(bodyStr, "Failed to handle sampling response") {
			t.Errorf("Pong response was incorrectly detected as sampling response. Response: %s", bodyStr)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 for pong response, got %d. Body: %s", resp.StatusCode, bodyStr)
		}
	})

	t.Run("Pong response with null result should not be treated as sampling response", func(t *testing.T) {
		pongResponse := map[string]any{
			"jsonrpc": "2.0",
			"id":      124,
		}

		resp, err := postJSON(server.URL, pongResponse)
		if err != nil {
			t.Fatalf("Failed to send pong response: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}
		bodyStr := string(bodyBytes)

		if strings.Contains(bodyStr, "Missing session ID for sampling response") {
			t.Errorf("Pong response with omitted result was incorrectly detected as sampling response. Response: %s", bodyStr)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 for pong response, got %d. Body: %s", resp.StatusCode, bodyStr)
		}
	})

	t.Run("Response with empty error should not be treated as sampling response", func(t *testing.T) {
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      125,
			"error":   map[string]any{},
		}

		resp, err := postJSON(server.URL, response)
		if err != nil {
			t.Fatalf("Failed to send response: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}
		bodyStr := string(bodyBytes)

		if strings.Contains(bodyStr, "Missing session ID for sampling response") {
			t.Errorf("Response with empty error was incorrectly detected as sampling response. Response: %s", bodyStr)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 for response with empty error, got %d. Body: %s", resp.StatusCode, bodyStr)
		}
	})
}

func TestStreamableHTTPServer_TLS(t *testing.T) {
	t.Run("TLS options are set correctly", func(t *testing.T) {
		mcpServer := NewMCPServer("test-mcp-server", "1.0.0")
		certFile := "/path/to/cert.pem"
		keyFile := "/path/to/key.pem"

		server := NewStreamableHTTPServer(
			mcpServer,
			WithTLSCert(certFile, keyFile),
		)

		if server.tlsCertFile != certFile {
			t.Errorf("Expected tlsCertFile to be %s, got %s", certFile, server.tlsCertFile)
		}
		if server.tlsKeyFile != keyFile {
			t.Errorf("Expected tlsKeyFile to be %s, got %s", keyFile, server.tlsKeyFile)
		}
	})
}

func postJSON(url string, bodyObject any) (*http.Response, error) {
	jsonBody, _ := json.Marshal(bodyObject)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

func TestStreamableHTTP_SessionValidation(t *testing.T) {
	mcpServer := NewMCPServer("test-server", "1.0.0")
	mcpServer.AddTool(mcp.NewTool("time",
		mcp.WithDescription("Get the current time")), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("2024-01-01T00:00:00Z"), nil
	})

	server := NewTestStreamableHTTPServer(mcpServer)
	defer server.Close()

	t.Run("Reject tool call with fake session ID", func(t *testing.T) {
		toolCallRequest := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params": map[string]any{
				"name": "time",
			},
		}

		jsonBody, _ := json.Marshal(toolCallRequest)
		req, _ := http.NewRequest(http.MethodPost, server.URL, bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(HeaderKeySessionID, "mcp-session-ffffffff-ffff-ffff-ffff-ffffffffffff")

		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "Invalid session ID") {
			t.Errorf("Expected 'Invalid session ID' error, got: %s", string(body))
		}
	})

	t.Run("Reject tool call with malformed session ID", func(t *testing.T) {
		toolCallRequest := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params": map[string]any{
				"name": "time",
			},
		}

		jsonBody, _ := json.Marshal(toolCallRequest)
		req, _ := http.NewRequest(http.MethodPost, server.URL, bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(HeaderKeySessionID, "invalid-session-id")

		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "Invalid session ID") {
			t.Errorf("Expected 'Invalid session ID' error, got: %s", string(body))
		}
	})

	t.Run("Accept tool call with valid session ID from initialize", func(t *testing.T) {
		jsonBody, _ := json.Marshal(initRequest)
		req, _ := http.NewRequest(http.MethodPost, server.URL, bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}
		defer resp.Body.Close()

		sessionID := resp.Header.Get(HeaderKeySessionID)
		if sessionID == "" {
			t.Fatal("Expected session ID in response header")
		}

		toolCallRequest := map[string]any{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "tools/call",
			"params": map[string]any{
				"name": "time",
			},
		}

		jsonBody, _ = json.Marshal(toolCallRequest)
		req, _ = http.NewRequest(http.MethodPost, server.URL, bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(HeaderKeySessionID, sessionID)

		resp, err = server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to call tool: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(body))
		}
	})

	t.Run("Reject tool call with terminated session ID", func(t *testing.T) {
		jsonBody, _ := json.Marshal(initRequest)
		req, _ := http.NewRequest(http.MethodPost, server.URL, bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}
		resp.Body.Close()

		sessionID := resp.Header.Get(HeaderKeySessionID)
		if sessionID == "" {
			t.Fatal("Expected session ID in response header")
		}

		req, _ = http.NewRequest(http.MethodDelete, server.URL, nil)
		req.Header.Set(HeaderKeySessionID, sessionID)

		resp, err = server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to terminate session: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 for termination, got %d", resp.StatusCode)
		}

		toolCallRequest := map[string]any{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "tools/call",
			"params": map[string]any{
				"name": "time",
			},
		}

		jsonBody, _ = json.Marshal(toolCallRequest)
		req, _ = http.NewRequest(http.MethodPost, server.URL, bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(HeaderKeySessionID, sessionID)

		resp, err = server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("Expected status 404, got %d. Body: %s", resp.StatusCode, string(body))
		}
	})
}

func TestInsecureStatefulSessionIdManager(t *testing.T) {
	t.Run("Generate creates valid session ID", func(t *testing.T) {
		manager := &InsecureStatefulSessionIdManager{}
		sessionID := manager.Generate()

		if !strings.HasPrefix(sessionID, idPrefix) {
			t.Errorf("Expected session ID to start with %s, got %s", idPrefix, sessionID)
		}

		isTerminated, err := manager.Validate(sessionID)
		if err != nil {
			t.Errorf("Expected valid session ID, got error: %v", err)
		}
		if isTerminated {
			t.Error("Expected session to not be terminated")
		}
	})

	t.Run("Validate rejects non-existent session ID", func(t *testing.T) {
		manager := &InsecureStatefulSessionIdManager{}
		fakeSessionID := "mcp-session-ffffffff-ffff-ffff-ffff-ffffffffffff"

		isTerminated, err := manager.Validate(fakeSessionID)
		if err == nil {
			t.Error("Expected error for non-existent session ID")
		}
		if isTerminated {
			t.Error("Expected isTerminated to be false for invalid session")
		}
		if !strings.Contains(err.Error(), "session not found") {
			t.Errorf("Expected 'session not found' error, got: %v", err)
		}
	})

	t.Run("Validate rejects malformed session ID", func(t *testing.T) {
		manager := &InsecureStatefulSessionIdManager{}
		invalidSessionID := "invalid-session-id"

		_, err := manager.Validate(invalidSessionID)
		if err == nil {
			t.Error("Expected error for malformed session ID")
		}
		if !strings.Contains(err.Error(), "invalid session id") {
			t.Errorf("Expected 'invalid session id' error, got: %v", err)
		}
	})

	t.Run("Terminate marks session as terminated", func(t *testing.T) {
		manager := &InsecureStatefulSessionIdManager{}
		sessionID := manager.Generate()

		isNotAllowed, err := manager.Terminate(sessionID)
		if err != nil {
			t.Errorf("Expected no error on termination, got: %v", err)
		}
		if isNotAllowed {
			t.Error("Expected termination to be allowed")
		}

		isTerminated, err := manager.Validate(sessionID)
		if !isTerminated {
			t.Error("Expected session to be marked as terminated")
		}
		if err != nil {
			t.Errorf("Expected no error for terminated session, got: %v", err)
		}
	})

	t.Run("Terminate is idempotent for non-existent session ID", func(t *testing.T) {
		manager := &InsecureStatefulSessionIdManager{}
		fakeSessionID := "mcp-session-ffffffff-ffff-ffff-ffff-ffffffffffff"

		isNotAllowed, err := manager.Terminate(fakeSessionID)
		if err != nil {
			t.Errorf("Expected no error when terminating non-existent session, got: %v", err)
		}
		if isNotAllowed {
			t.Error("Expected isNotAllowed to be false")
		}
	})

	t.Run("Terminate is idempotent for already-terminated session", func(t *testing.T) {
		manager := &InsecureStatefulSessionIdManager{}
		sessionID := manager.Generate()

		isNotAllowed, err := manager.Terminate(sessionID)
		if err != nil {
			t.Errorf("Expected no error on first termination, got: %v", err)
		}
		if isNotAllowed {
			t.Error("Expected termination to be allowed")
		}

		isNotAllowed, err = manager.Terminate(sessionID)
		if err != nil {
			t.Errorf("Expected no error on second termination (idempotent), got: %v", err)
		}
		if isNotAllowed {
			t.Error("Expected termination to be allowed on retry")
		}
	})

	t.Run("Concurrent generate and validate", func(t *testing.T) {
		manager := &InsecureStatefulSessionIdManager{}
		var wg sync.WaitGroup
		sessionIDs := make([]string, 100)

		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				sessionIDs[index] = manager.Generate()
			}(i)
		}

		wg.Wait()

		for _, sessionID := range sessionIDs {
			isTerminated, err := manager.Validate(sessionID)
			if err != nil {
				t.Errorf("Expected valid session ID %s, got error: %v", sessionID, err)
			}
			if isTerminated {
				t.Errorf("Expected session %s to not be terminated", sessionID)
			}
		}
	})
}
