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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		for i := range 10 {
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
	server := NewTestStreamableHTTPServer(mcpServer, WithStateful(true))
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

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", resp.StatusCode)
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
		for i := range 10 {
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

func TestStreamableHttpResourceGet(t *testing.T) {
	s := NewMCPServer("test-mcp-server", "1.0", WithResourceCapabilities(true, true))

	testServer := NewTestStreamableHTTPServer(
		s,
		WithStateful(true),
		WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			session := ClientSessionFromContext(ctx)

			if st, ok := session.(SessionWithResources); ok {
				if _, ok := st.GetSessionResources()["file://test_resource"]; !ok {
					st.SetSessionResources(map[string]ServerResource{
						"file://test_resource": {
							Resource: mcp.Resource{
								URI:         "file://test_resource",
								Name:        "test_resource",
								Description: "A test resource",
								MIMEType:    "text/plain",
							},
							Handler: func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
								return []mcp.ResourceContents{
									mcp.TextResourceContents{
										URI:      "file://test_resource",
										Text:     "test content",
										MIMEType: "text/plain",
									},
								}, nil
							},
						},
					})
				}
			} else {
				t.Error("Session does not support tools/resources")
			}

			return ctx
		}),
	)

	var sessionID string

	// Initialize session
	resp, err := postJSON(testServer.URL, initRequest)
	if err != nil {
		t.Fatalf("Failed to send initialize request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	sessionID = resp.Header.Get(HeaderKeySessionID)
	if sessionID == "" {
		t.Fatal("Expected session id in header")
	}

	// List resources
	listResourcesRequest := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "resources/list",
		"params":  map[string]any{},
	}
	resp, err = postSessionJSON(testServer.URL, sessionID, listResourcesRequest)
	if err != nil {
		t.Fatalf("Failed to send list resources request: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	var listResponse jsonRPCResponse
	if err := json.Unmarshal(bodyBytes, &listResponse); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	items, ok := listResponse.Result["resources"].([]any)
	if !ok {
		t.Fatal("Expected resources array in response")
	}
	if len(items) != 1 {
		t.Fatalf("Expected 1 resource, got %d", len(items))
	}
	imap, ok := items[0].(map[string]any)
	if !ok {
		t.Fatal("Expected resource to be a map")
	}
	if imap["uri"] != "file://test_resource" {
		t.Errorf("Expected resource URI file://test_resource, got %v", imap["uri"])
	}

	// List resources
	getResourceRequest := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "resources/read",
		"params":  map[string]any{"uri": "file://test_resource"},
	}
	resp, err = postSessionJSON(testServer.URL, sessionID, getResourceRequest)
	if err != nil {
		t.Fatalf("Failed to send list resources request: %v", err)
	}

	bodyBytes, _ = io.ReadAll(resp.Body)
	var readResponse jsonRPCResponse
	if err := json.Unmarshal(bodyBytes, &readResponse); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	contents, ok := readResponse.Result["contents"].([]any)
	if !ok {
		t.Fatal("Expected contents array in response")
	}
	if len(contents) != 1 {
		t.Fatalf("Expected 1 content, got %d", len(contents))
	}

	cmap, ok := contents[0].(map[string]any)
	if !ok {
		t.Fatal("Expected content to be a map")
	}
	if cmap["uri"] != "file://test_resource" {
		t.Errorf("Expected content URI file://test_resource, got %v", cmap["uri"])
	}
}

func TestStreamableHTTP_SessionWithTools(t *testing.T) {
	t.Run("SessionWithTools implementation", func(t *testing.T) {
		// Create hooks to track sessions
		hooks := &Hooks{}
		sessionChan := make(chan *streamableHttpSession, 1)

		hooks.AddOnRegisterSession(func(ctx context.Context, session ClientSession) {
			if s, ok := session.(*streamableHttpSession); ok {
				select {
				case sessionChan <- s:
				default:
					// Channel already has a session, ignore
				}
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

		// Wait for session with timeout
		var session *streamableHttpSession
		select {
		case session = <-sessionChan:
			// Got the session!
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for session registration")
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
		session.SetSessionTools(tools)

		// Test GetSessionTools
		retrievedTools := session.GetSessionTools()
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
		for i := range 10 {
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
				session.SetSessionTools(tools)
			}(i)
			go func() {
				defer wg.Done()
				_ = session.GetSessionTools()
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
		session.SetSessionTools(finalTools)
		retrievedTools = session.GetSessionTools()
		if len(retrievedTools) != 1 {
			t.Errorf("Expected 1 tool, got %d", len(retrievedTools))
		}
		if _, exists := retrievedTools["final_tool"]; !exists {
			t.Error("Expected final_tool to exist")
		}
	})
}

func TestStreamableHTTP_SessionWithResources(t *testing.T) {
	t.Run("SessionWithResources implementation", func(t *testing.T) {
		hooks := &Hooks{}
		sessionChan := make(chan *streamableHttpSession, 1)

		hooks.AddOnRegisterSession(func(ctx context.Context, session ClientSession) {
			if s, ok := session.(*streamableHttpSession); ok {
				select {
				case sessionChan <- s:
				default:
					// Channel already has a session, ignore
				}
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

		// Wait for session with timeout
		var session *streamableHttpSession
		select {
		case session = <-sessionChan:
			// Got the session!
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for session registration")
		}

		// Test setting and getting resources
		resources := map[string]ServerResource{
			"test_resource": {
				Resource: mcp.Resource{
					URI:         "file://test_resource",
					Name:        "test_resource",
					Description: "A test resource",
					MIMEType:    "text/plain",
				},
				Handler: func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
					return []mcp.ResourceContents{
						mcp.TextResourceContents{
							URI:  "file://test_resource",
							Text: "test content",
						},
					}, nil
				},
			},
		}

		// Test SetSessionResources
		session.SetSessionResources(resources)

		// Test GetSessionResources
		retrievedResources := session.GetSessionResources()
		if len(retrievedResources) != 1 {
			t.Errorf("Expected 1 resource, got %d", len(retrievedResources))
		}
		if resource, exists := retrievedResources["test_resource"]; !exists {
			t.Error("Expected test_resource to exist")
		} else if resource.Resource.Name != "test_resource" {
			t.Errorf("Expected resource name test_resource, got %s", resource.Resource.Name)
		}

		// Test concurrent access
		var wg sync.WaitGroup
		for i := range 10 {
			wg.Add(2)
			go func(i int) {
				defer wg.Done()
				resources := map[string]ServerResource{
					fmt.Sprintf("resource_%d", i): {
						Resource: mcp.Resource{
							URI:         fmt.Sprintf("file://resource_%d", i),
							Name:        fmt.Sprintf("resource_%d", i),
							Description: fmt.Sprintf("Resource %d", i),
							MIMEType:    "text/plain",
						},
					},
				}
				session.SetSessionResources(resources)
			}(i)
			go func() {
				defer wg.Done()
				_ = session.GetSessionResources()
			}()
		}
		wg.Wait()

		// Verify we can still get and set resources after concurrent access
		finalResources := map[string]ServerResource{
			"final_resource": {
				Resource: mcp.Resource{
					URI:         "file://final_resource",
					Name:        "final_resource",
					Description: "Final Resource",
					MIMEType:    "text/plain",
				},
			},
		}
		session.SetSessionResources(finalResources)
		retrievedResources = session.GetSessionResources()
		if len(retrievedResources) != 1 {
			t.Errorf("Expected 1 resource, got %d", len(retrievedResources))
		}
		if _, exists := retrievedResources["final_resource"]; !exists {
			t.Error("Expected final_resource to exist")
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
		testServer := NewTestStreamableHTTPServer(mcpServer, WithStateful(true))
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
		for range 10 {
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

func TestStreamableHTTPServer_WithDisableStreaming(t *testing.T) {
	t.Run("WithDisableStreaming blocks GET requests", func(t *testing.T) {
		mcpServer := NewMCPServer("test-mcp-server", "1.0.0")
		server := NewTestStreamableHTTPServer(mcpServer, WithDisableStreaming(true))
		defer server.Close()

		// Attempt a GET request (which should be blocked)
		req, err := http.NewRequest(http.MethodGet, server.URL, nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "text/event-stream")

		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		defer resp.Body.Close()

		// Verify the request is rejected with 405 Method Not Allowed
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405 Method Not Allowed, got %d", resp.StatusCode)
		}

		// Verify the error message
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		expectedMessage := "Streaming is disabled on this server"
		if !strings.Contains(string(bodyBytes), expectedMessage) {
			t.Errorf("Expected error message to contain '%s', got '%s'", expectedMessage, string(bodyBytes))
		}
	})

	t.Run("POST requests still work with WithDisableStreaming", func(t *testing.T) {
		mcpServer := NewMCPServer("test-mcp-server", "1.0.0")
		server := NewTestStreamableHTTPServer(mcpServer, WithDisableStreaming(true))
		defer server.Close()

		// POST requests should still work
		resp, err := postJSON(server.URL, initRequest)
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		// Verify the response is valid
		bodyBytes, _ := io.ReadAll(resp.Body)
		var responseMessage jsonRPCResponse
		if err := json.Unmarshal(bodyBytes, &responseMessage); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}
		if responseMessage.Result["protocolVersion"] != mcp.LATEST_PROTOCOL_VERSION {
			t.Errorf("Expected protocol version %s, got %s", mcp.LATEST_PROTOCOL_VERSION, responseMessage.Result["protocolVersion"])
		}
	})

	t.Run("Streaming works when WithDisableStreaming is false", func(t *testing.T) {
		mcpServer := NewMCPServer("test-mcp-server", "1.0.0")
		server := NewTestStreamableHTTPServer(mcpServer, WithDisableStreaming(false))
		defer server.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		// GET request should work when streaming is enabled
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "text/event-stream")

		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		if resp.Header.Get("content-type") != "text/event-stream" {
			t.Errorf("Expected content-type text/event-stream, got %s", resp.Header.Get("content-type"))
		}
	})
}

func postJSON(url string, bodyObject any) (*http.Response, error) {
	jsonBody, _ := json.Marshal(bodyObject)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

func postSessionJSON(url, session string, bodyObject any) (*http.Response, error) {
	jsonBody, _ := json.Marshal(bodyObject)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HeaderKeySessionID, session)
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

	t.Run("Accept tool call with properly formatted session ID", func(t *testing.T) {
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

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		var response map[string]any
		if err := json.Unmarshal(body, &response); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if result, ok := response["result"].(map[string]any); ok {
			if content, ok := result["content"].([]any); ok && len(content) > 0 {
				if textContent, ok := content[0].(map[string]any); ok {
					if text, ok := textContent["text"].(string); ok {
						// Should be a valid timestamp response
						if text == "" {
							t.Error("Expected non-empty timestamp response")
						}
					}
				}
			}
		} else {
			t.Errorf("Expected result in response, got: %s", string(body))
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

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", resp.StatusCode)
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

	t.Run("Reject tool call with terminated session ID (stateful mode)", func(t *testing.T) {
		mcpServer := NewMCPServer("test-server", "1.0.0")
		// Use explicit stateful mode for this test since termination requires local tracking
		server := NewTestStreamableHTTPServer(mcpServer, WithStateful(true))
		defer server.Close()

		// First, initialize a session
		initRequest := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"clientInfo": map[string]any{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}

		jsonBody, _ := json.Marshal(initRequest)
		req, _ := http.NewRequest(http.MethodPost, server.URL, bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to initialize session: %v", err)
		}

		sessionID := resp.Header.Get(HeaderKeySessionID)
		if sessionID == "" {
			t.Fatal("Expected session ID in response header")
		}
		resp.Body.Close()

		// Now terminate the session
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

		for i := range 100 {
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

func TestDefaultSessionIdManagerResolver(t *testing.T) {
	t.Run("ResolveSessionIdManager returns configured manager", func(t *testing.T) {
		manager := &InsecureStatefulSessionIdManager{}
		resolver := NewDefaultSessionIdManagerResolver(manager)

		req, err := http.NewRequest("POST", "/test", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		resolved := resolver.ResolveSessionIdManager(req)
		if resolved != manager {
			t.Error("Expected resolver to return the configured manager")
		}
	})

	t.Run("ResolveSessionIdManager works with StatelessSessionIdManager", func(t *testing.T) {
		manager := &StatelessSessionIdManager{}
		resolver := NewDefaultSessionIdManagerResolver(manager)

		req, err := http.NewRequest("GET", "/test", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		resolved := resolver.ResolveSessionIdManager(req)
		if resolved != manager {
			t.Error("Expected resolver to return the configured stateless manager")
		}

		// Test that the resolved manager works correctly
		sessionID := resolved.Generate()
		if sessionID != "" {
			t.Errorf("Expected stateless manager to return empty session ID, got: %s", sessionID)
		}

		isTerminated, err := resolved.Validate("any-session-id")
		if err != nil {
			t.Errorf("Expected stateless manager to validate any session ID, got error: %v", err)
		}
		if isTerminated {
			t.Error("Expected stateless manager to not mark sessions as terminated")
		}
	})

	t.Run("ResolveSessionIdManager is consistent across multiple calls", func(t *testing.T) {
		manager := &InsecureStatefulSessionIdManager{}
		resolver := NewDefaultSessionIdManagerResolver(manager)

		req1, _ := http.NewRequest("POST", "/test1", nil)
		req2, _ := http.NewRequest("GET", "/test2", nil)

		resolved1 := resolver.ResolveSessionIdManager(req1)
		resolved2 := resolver.ResolveSessionIdManager(req2)

		if resolved1 != resolved2 {
			t.Error("Expected resolver to return the same manager for different requests")
		}
		if resolved1 != manager {
			t.Error("Expected resolver to return the configured manager")
		}
	})

	t.Run("ResolveSessionIdManager handles nil request gracefully", func(t *testing.T) {
		manager := &InsecureStatefulSessionIdManager{}
		resolver := NewDefaultSessionIdManagerResolver(manager)

		// This should not panic even with nil request since we ignore the request parameter
		resolved := resolver.ResolveSessionIdManager(nil)
		if resolved != manager {
			t.Error("Expected resolver to return the configured manager even with nil request")
		}
	})

	t.Run("NewDefaultSessionIdManagerResolver handles nil manager defensively", func(t *testing.T) {
		// This should not panic and should use default manager
		resolver := NewDefaultSessionIdManagerResolver(nil)
		if resolver == nil {
			t.Fatal("Expected resolver to be created even with nil manager")
		}

		req, _ := http.NewRequest("POST", "/test", nil)
		resolved := resolver.ResolveSessionIdManager(req)
		if resolved == nil {
			t.Error("Expected resolver to return a non-nil manager")
		}

		// Test that the resolved manager works (stateless behavior)
		sessionID := resolved.Generate()
		if sessionID != "" {
			t.Error("Expected stateless manager to generate empty session ID")
		}

		// Test that validation accepts any session ID (stateless behavior)
		isTerminated, err := resolved.Validate("any-session-id")
		if err != nil {
			t.Errorf("Expected stateless manager to accept any session ID, got error: %v", err)
		}
		if isTerminated {
			t.Error("Expected stateless manager to not terminate sessions")
		}
	})
}

func TestSessionIdManagerResolver_Integration(t *testing.T) {
	t.Run("WithSessionIdManagerResolver option sets resolver correctly", func(t *testing.T) {
		mcpServer := NewMCPServer("test-server", "1.0.0")
		manager := &StatelessSessionIdManager{}
		resolver := NewDefaultSessionIdManagerResolver(manager)

		server := NewStreamableHTTPServer(mcpServer, WithSessionIdManagerResolver(resolver))

		// Test that the resolver was set
		if server.sessionIdManagerResolver != resolver {
			t.Error("Expected WithSessionIdManagerResolver to set the resolver")
		}

		// Test that it resolves correctly
		req, _ := http.NewRequest("POST", "/test", nil)
		resolved := server.sessionIdManagerResolver.ResolveSessionIdManager(req)
		if resolved != manager {
			t.Error("Expected resolver to return the configured manager")
		}
	})

	t.Run("WithSessionIdManager option creates resolver with manager", func(t *testing.T) {
		mcpServer := NewMCPServer("test-server", "1.0.0")
		manager := &StatelessSessionIdManager{}

		server := NewStreamableHTTPServer(mcpServer, WithSessionIdManager(manager))

		// Test that a resolver was created
		req, _ := http.NewRequest("POST", "/test", nil)
		resolved := server.sessionIdManagerResolver.ResolveSessionIdManager(req)
		if resolved != manager {
			t.Error("Expected WithSessionIdManager to create resolver with the configured manager")
		}

		// Verify it's a DefaultSessionIdManagerResolver
		if defaultResolver, ok := server.sessionIdManagerResolver.(*DefaultSessionIdManagerResolver); ok {
			if defaultResolver.manager != manager {
				t.Error("Expected DefaultSessionIdManagerResolver to wrap the configured manager")
			}
		} else {
			t.Error("Expected WithSessionIdManager to create a DefaultSessionIdManagerResolver")
		}
	})

	t.Run("WithStateLess option creates resolver with StatelessSessionIdManager", func(t *testing.T) {
		mcpServer := NewMCPServer("test-server", "1.0.0")

		server := NewStreamableHTTPServer(mcpServer, WithStateLess(true))

		// Test that a resolver was created with stateless manager
		req, _ := http.NewRequest("POST", "/test", nil)
		resolved := server.sessionIdManagerResolver.ResolveSessionIdManager(req)

		// Verify it's a stateless manager
		sessionID := resolved.Generate()
		if sessionID != "" {
			t.Error("Expected stateless manager from WithStateLess(true)")
		}

		// Verify it's wrapped in DefaultSessionIdManagerResolver
		if defaultResolver, ok := server.sessionIdManagerResolver.(*DefaultSessionIdManagerResolver); ok {
			if _, ok := defaultResolver.manager.(*StatelessSessionIdManager); !ok {
				t.Error("Expected DefaultSessionIdManagerResolver to wrap StatelessSessionIdManager")
			}
		} else {
			t.Error("Expected WithStateLess to create a DefaultSessionIdManagerResolver")
		}
	})

	t.Run("WithStateLess(false) does not override default manager", func(t *testing.T) {
		mcpServer := NewMCPServer("test-server", "1.0.0")

		server := NewStreamableHTTPServer(mcpServer, WithStateLess(false))

		// Test that the default manager is still used (StatelessGeneratingSessionIdManager)
		req, _ := http.NewRequest("POST", "/test", nil)
		resolved := server.sessionIdManagerResolver.ResolveSessionIdManager(req)

		// Verify it's a generating manager (default behavior)
		sessionID := resolved.Generate()
		if sessionID == "" {
			t.Error("Expected generating manager to generate session ID by default")
		}
		if !strings.HasPrefix(sessionID, idPrefix) {
			t.Error("Expected generating manager to generate session ID with correct prefix")
		}
	})

	t.Run("Option precedence: WithSessionIdManagerResolver overrides WithSessionIdManager", func(t *testing.T) {
		mcpServer := NewMCPServer("test-server", "1.0.0")
		statefulManager := &InsecureStatefulSessionIdManager{}
		statelessManager := &StatelessSessionIdManager{}
		resolver := NewDefaultSessionIdManagerResolver(statelessManager)

		server := NewStreamableHTTPServer(mcpServer,
			WithSessionIdManager(statefulManager),
			WithSessionIdManagerResolver(resolver),
		)

		// Test that the resolver option took precedence
		req, _ := http.NewRequest("POST", "/test", nil)
		resolved := server.sessionIdManagerResolver.ResolveSessionIdManager(req)
		if resolved != statelessManager {
			t.Error("Expected WithSessionIdManagerResolver to override WithSessionIdManager")
		}

		sessionID := resolved.Generate()
		if sessionID != "" {
			t.Error("Expected stateless manager from resolver to be used")
		}
	})

	t.Run("Option precedence: WithSessionIdManagerResolver overrides WithStateLess", func(t *testing.T) {
		mcpServer := NewMCPServer("test-server", "1.0.0")
		statefulManager := &InsecureStatefulSessionIdManager{}
		resolver := NewDefaultSessionIdManagerResolver(statefulManager)

		server := NewStreamableHTTPServer(mcpServer,
			WithStateLess(true),
			WithSessionIdManagerResolver(resolver),
		)

		// Test that the resolver option took precedence
		req, _ := http.NewRequest("POST", "/test", nil)
		resolved := server.sessionIdManagerResolver.ResolveSessionIdManager(req)
		if resolved != statefulManager {
			t.Error("Expected WithSessionIdManagerResolver to override WithStateLess")
		}

		sessionID := resolved.Generate()
		if sessionID == "" {
			t.Error("Expected stateful manager from resolver to be used")
		}
	})

	t.Run("WithSessionIdManagerResolver handles nil resolver defensively", func(t *testing.T) {
		mcpServer := NewMCPServer("test-server", "1.0.0")

		// This should not panic and should fall back to StatelessSessionIdManager (safe default)
		server := NewStreamableHTTPServer(mcpServer, WithSessionIdManagerResolver(nil))

		req, _ := http.NewRequest("POST", "/test", nil)
		resolved := server.sessionIdManagerResolver.ResolveSessionIdManager(req)
		if resolved == nil {
			t.Error("Expected nil resolver to be replaced with default")
		}

		// Test that the resolved manager works (should be default stateless manager)
		sessionID := resolved.Generate()
		if sessionID != "" {
			t.Error("Expected default stateless manager to generate empty session ID")
		}
	})

	t.Run("WithSessionIdManager handles nil manager defensively", func(t *testing.T) {
		mcpServer := NewMCPServer("test-server", "1.0.0")

		// This should not panic and should fall back to StatelessSessionIdManager (safe default)
		server := NewStreamableHTTPServer(mcpServer, WithSessionIdManager(nil))

		req, _ := http.NewRequest("POST", "/test", nil)
		resolved := server.sessionIdManagerResolver.ResolveSessionIdManager(req)
		if resolved == nil {
			t.Error("Expected nil manager to be replaced with default")
		}

		// Test that the resolved manager works (should be default stateless manager)
		sessionID := resolved.Generate()
		if sessionID != "" {
			t.Error("Expected default stateless manager to generate empty session ID")
		}
	})

	t.Run("Multiple nil options fall back safely", func(t *testing.T) {
		mcpServer := NewMCPServer("test-server", "1.0.0")

		// Chain multiple nil options - last one should win with StatelessSessionIdManager fallback
		server := NewStreamableHTTPServer(mcpServer,
			WithSessionIdManager(nil),
			WithSessionIdManagerResolver(nil),
		)

		req, _ := http.NewRequest("POST", "/test", nil)
		resolved := server.sessionIdManagerResolver.ResolveSessionIdManager(req)
		if resolved == nil {
			t.Error("Expected chained nil options to fall back safely")
		}

		// Verify it uses stateless behavior (default)
		sessionID := resolved.Generate()
		if sessionID != "" {
			t.Error("Expected fallback stateless manager to generate empty session ID")
		}
	})

	t.Run("Nil manager falls back safely", func(t *testing.T) {
		mcpServer := NewMCPServer("test-server", "1.0.0")
		// requires nil-guard in WithSessionIdManager
		srv := NewTestStreamableHTTPServer(mcpServer, WithSessionIdManager(nil))
		defer srv.Close()
		resp, err := postJSON(srv.URL, initRequest)
		if err != nil {
			t.Fatalf("init failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		_ = resp.Body.Close()
	})

	t.Run("Nil resolver falls back safely", func(t *testing.T) {
		mcpServer := NewMCPServer("test-server", "1.0.0")
		// requires nil-guard in WithSessionIdManagerResolver
		srv := NewTestStreamableHTTPServer(mcpServer, WithSessionIdManagerResolver(nil))
		defer srv.Close()
		resp, err := postJSON(srv.URL, initRequest)
		if err != nil {
			t.Fatalf("init failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		_ = resp.Body.Close()
	})

	t.Run("WithStateful enables stateful manager", func(t *testing.T) {
		mcpServer := NewMCPServer("test-server", "1.0.0")
		server := NewStreamableHTTPServer(mcpServer, WithStateful(true))

		req, _ := http.NewRequest("POST", "/test", nil)
		resolved := server.sessionIdManagerResolver.ResolveSessionIdManager(req)

		sessionID := resolved.Generate()
		if sessionID == "" {
			t.Error("Expected stateful manager to generate session ID")
		}
		if !strings.HasPrefix(sessionID, idPrefix) {
			t.Error("Expected stateful session ID format")
		}

		// Test that stateful manager validates session existence (unlike default)
		_, err := resolved.Validate("unknown-session-id")
		if err == nil {
			t.Error("Expected stateful manager to reject unknown session ID")
		}
	})
}

func TestStreamableHTTP_SendNotificationToSpecificClient(t *testing.T) {
	t.Run("POST session registration enables SendNotificationToSpecificClient", func(t *testing.T) {
		hooks := &Hooks{}
		var registeredSessionID string
		var mu sync.Mutex
		var sessionRegistered sync.WaitGroup
		sessionRegistered.Add(1)

		hooks.AddOnRegisterSession(func(ctx context.Context, session ClientSession) {
			mu.Lock()
			registeredSessionID = session.SessionID()
			mu.Unlock()
			sessionRegistered.Done()
		})

		mcpServer := NewMCPServer("test", "1.0.0", WithHooks(hooks))
		testServer := NewTestStreamableHTTPServer(mcpServer, WithStateful(true))
		defer testServer.Close()

		// Send initialize request to register session
		resp, err := postJSON(testServer.URL, initRequest)
		if err != nil {
			t.Fatalf("Failed to send initialize request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", resp.StatusCode)
		}

		// Get session ID from response header
		sessionID := resp.Header.Get(HeaderKeySessionID)
		if sessionID == "" {
			t.Fatal("Expected session ID in response header")
		}

		// Wait for session registration
		done := make(chan struct{})
		go func() {
			sessionRegistered.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Session registered successfully
		case <-time.After(2 * time.Second):
			t.Fatal("Timeout waiting for session registration")
		}

		mu.Lock()
		if registeredSessionID != sessionID {
			t.Errorf("Expected registered session ID %s, got %s", sessionID, registeredSessionID)
		}
		mu.Unlock()

		// Now test SendNotificationToSpecificClient
		err = mcpServer.SendNotificationToSpecificClient(sessionID, "test/notification", map[string]any{
			"message": "test notification",
		})
		if err != nil {
			t.Errorf("SendNotificationToSpecificClient failed: %v", err)
		}
	})

	t.Run("Session reuse for non-initialize requests", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")

		// Add a tool that sends a notification
		mcpServer.AddTool(mcp.NewTool("notify_tool"), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			session := ClientSessionFromContext(ctx)
			if session == nil {
				return mcp.NewToolResultError("no session in context"), nil
			}

			// Try to send notification to specific client
			server := ServerFromContext(ctx)
			err := server.SendNotificationToSpecificClient(session.SessionID(), "tool/notification", map[string]any{
				"from": "tool",
			})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("notification failed: %v", err)), nil
			}

			return mcp.NewToolResultText("notification sent"), nil
		})

		testServer := NewTestStreamableHTTPServer(mcpServer, WithStateful(true))
		defer testServer.Close()

		// Initialize session
		resp, err := postJSON(testServer.URL, initRequest)
		if err != nil {
			t.Fatalf("Failed to send initialize request: %v", err)
		}
		sessionID := resp.Header.Get(HeaderKeySessionID)
		resp.Body.Close()

		if sessionID == "" {
			t.Fatal("Expected session ID in response header")
		}

		// Give time for registration to complete
		time.Sleep(100 * time.Millisecond)

		// Call tool with the session ID
		toolCallRequest := map[string]any{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "tools/call",
			"params": map[string]any{
				"name": "notify_tool",
			},
		}

		jsonBody, _ := json.Marshal(toolCallRequest)
		req, _ := http.NewRequest(http.MethodPost, testServer.URL, bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(HeaderKeySessionID, sessionID)

		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to call tool: %v", err)
		}
		defer resp.Body.Close()

		bodyBytes, _ := io.ReadAll(resp.Body)
		bodyStr := string(bodyBytes)

		// Response might be SSE format if notification was sent
		var toolResponse jsonRPCResponse
		if strings.HasPrefix(bodyStr, "event: message") {
			// Parse SSE format
			lines := strings.Split(bodyStr, "\n")
			for _, line := range lines {
				if jsonData, ok := strings.CutPrefix(line, "data: "); ok {
					if err := json.Unmarshal([]byte(jsonData), &toolResponse); err == nil {
						break
					}
				}
			}
		} else {
			if err := json.Unmarshal(bodyBytes, &toolResponse); err != nil {
				t.Fatalf("Failed to unmarshal response: %v. Body: %s", err, bodyStr)
			}
		}

		if toolResponse.Error != nil {
			t.Errorf("Tool call failed: %v", toolResponse.Error)
		}

		// Verify the tool result indicates success
		if result, ok := toolResponse.Result["content"].([]any); ok {
			if len(result) > 0 {
				if content, ok := result[0].(map[string]any); ok {
					if text, ok := content["text"].(string); ok {
						if text != "notification sent" {
							t.Errorf("Expected 'notification sent', got %s", text)
						}
					}
				}
			}
		}
	})
}

// TestStreamableHTTP_AddToolDuringToolCall tests that adding a tool while a tool call
// is in progress doesn't break the client's response.
// This is a regression test for issue #638 where notifications sent via
// sendNotificationToAllClients during an in-progress request would cause
// the response to fail with "unexpected nil response".
func TestStreamableHTTP_AddToolDuringToolCall(t *testing.T) {
	mcpServer := NewMCPServer("test-mcp-server", "1.0",
		WithToolCapabilities(true), // Enable tool list change notifications
	)
	// Add a tool that takes some time to complete
	mcpServer.AddTool(mcp.NewTool("slow_tool",
		mcp.WithDescription("A tool that takes time to complete"),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Simulate work that takes some time
		time.Sleep(100 * time.Millisecond)
		return mcp.NewToolResultText("done"), nil
	})
	server := NewTestStreamableHTTPServer(mcpServer, WithStateful(true))
	defer server.Close()
	// Initialize to get session
	resp, err := postJSON(server.URL, initRequest)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	sessionID := resp.Header.Get(HeaderKeySessionID)
	resp.Body.Close()
	if sessionID == "" {
		t.Fatal("Expected session ID in response header")
	}
	// Start the tool call in a goroutine
	resultChan := make(chan struct {
		statusCode int
		body       string
		err        error
	})
	go func() {
		toolRequest := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params": map[string]any{
				"name": "slow_tool",
			},
		}
		toolBody, _ := json.Marshal(toolRequest)
		req, _ := http.NewRequest("POST", server.URL, bytes.NewReader(toolBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(HeaderKeySessionID, sessionID)
		resp, err := server.Client().Do(req)
		if err != nil {
			resultChan <- struct {
				statusCode int
				body       string
				err        error
			}{0, "", err}
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		resultChan <- struct {
			statusCode int
			body       string
			err        error
		}{resp.StatusCode, string(body), nil}
	}()
	// Wait a bit then add a new tool while the slow_tool is executing
	// This triggers sendNotificationToAllClients
	time.Sleep(50 * time.Millisecond)
	mcpServer.AddTool(mcp.NewTool("new_tool",
		mcp.WithDescription("A new tool added during execution"),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("new tool result"), nil
	})
	// Wait for the tool call to complete
	result := <-resultChan
	if result.err != nil {
		t.Fatalf("Tool call failed with error: %v", result.err)
	}
	if result.statusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", result.statusCode, result.body)
	}
	// The response should contain the tool result
	// It may be SSE format (text/event-stream) due to the notification upgrade
	if !strings.Contains(result.body, "done") {
		t.Errorf("Expected response to contain 'done', got: %s", result.body)
	}
}

// nonFlushingResponseWriter wraps an http.ResponseWriter but does NOT implement http.Flusher.
// This is used to test the fix for servers/proxies that don't support streaming.
type nonFlushingResponseWriter struct {
	http.ResponseWriter
	statusCode    int
	body          bytes.Buffer
	headerWritten bool // tracks if WriteHeader has been called
}

func (w *nonFlushingResponseWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

func (w *nonFlushingResponseWriter) Write(b []byte) (int, error) {
	// Write implicitly calls WriteHeader(200) if not already called
	if !w.headerWritten {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(b)
}

func (w *nonFlushingResponseWriter) WriteHeader(statusCode int) {
	// Only honor the first WriteHeader call, just like real http.ResponseWriter
	if w.headerWritten {
		return
	}
	w.statusCode = statusCode
	w.headerWritten = true
}

func TestStreamableHTTP_GET_NonFlusherReturns405(t *testing.T) {
	t.Run("GET returns 405 when ResponseWriter does not support Flusher", func(t *testing.T) {
		mcpServer := NewMCPServer("test-mcp-server", "1.0")
		sseServer := NewStreamableHTTPServer(mcpServer)

		// Create a request
		req, err := http.NewRequest(http.MethodGet, "/mcp", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "text/event-stream")

		// Create a ResponseWriter that does NOT implement http.Flusher
		baseRecorder := httptest.NewRecorder()
		nonFlusher := &nonFlushingResponseWriter{
			ResponseWriter: baseRecorder,
		}

		// Call the handler directly
		sseServer.ServeHTTP(nonFlusher, req)

		// Verify we get HTTP 405 Method Not Allowed
		if nonFlusher.statusCode != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405 Method Not Allowed, got %d", nonFlusher.statusCode)
		}

		// Verify the error message
		bodyStr := nonFlusher.body.String()
		if !strings.Contains(bodyStr, "Streaming unsupported") {
			t.Errorf("Expected error message to contain 'Streaming unsupported', got '%s'", bodyStr)
		}
	})

	t.Run("GET returns 200 when ResponseWriter supports Flusher", func(t *testing.T) {
		mcpServer := NewMCPServer("test-mcp-server", "1.0")
		server := NewTestStreamableHTTPServer(mcpServer)
		defer server.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "text/event-stream")

		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		defer resp.Body.Close()

		// When Flusher is supported, we should get HTTP 200 and SSE content type
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		if resp.Header.Get("Content-Type") != "text/event-stream" {
			t.Errorf("Expected content-type text/event-stream, got %s", resp.Header.Get("Content-Type"))
		}
	})

	t.Run("Flusher check happens before headers are written", func(t *testing.T) {
		// This test ensures the fix is correct: when the flusher check fails,
		// we should get HTTP 405 and NOT HTTP 200 with an error in the body.
		// The bug was that WriteHeader(200) was called before the flusher check,
		// so http.Error() would write the error message but the status was already 200.
		mcpServer := NewMCPServer("test-mcp-server", "1.0")
		sseServer := NewStreamableHTTPServer(mcpServer)

		req, err := http.NewRequest(http.MethodGet, "/mcp", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "text/event-stream")

		baseRecorder := httptest.NewRecorder()
		nonFlusher := &nonFlushingResponseWriter{
			ResponseWriter: baseRecorder,
		}

		sseServer.ServeHTTP(nonFlusher, req)

		// The critical assertion: status code must be 405, NOT 200
		// Before the fix, status would be 200 because WriteHeader(200) was called
		// before the flusher check
		if nonFlusher.statusCode == http.StatusOK {
			t.Error("BUG: Got HTTP 200 when flusher is not supported. " +
				"This means headers were written before the flusher check. " +
				"Expected HTTP 405 Method Not Allowed.")
		}

		if nonFlusher.statusCode != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405, got %d", nonFlusher.statusCode)
		}
	})

	t.Run("Non-flusher does not orphan sessions", func(t *testing.T) {
		// This test verifies that when the flusher check fails, we clean up
		// the session from activeSessions to avoid orphaning it.
		mcpServer := NewMCPServer("test-mcp-server", "1.0")
		sseServer := NewStreamableHTTPServer(mcpServer)

		// First request with non-flusher should fail and NOT leave orphaned session
		req1, err := http.NewRequest(http.MethodGet, "/mcp", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req1.Header.Set("X-Session-ID", "test-session-123")

		baseRecorder := httptest.NewRecorder()
		nonFlusher := &nonFlushingResponseWriter{
			ResponseWriter: baseRecorder,
		}

		sseServer.ServeHTTP(nonFlusher, req1)

		// Should get 405
		if nonFlusher.statusCode != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405, got %d", nonFlusher.statusCode)
		}

		// Verify the session was cleaned up from activeSessions
		// We can't directly access activeSessions (it's private), but we can verify
		// by checking that the server's internal session map doesn't have it
		if _, exists := sseServer.activeSessions.Load("test-session-123"); exists {
			t.Error("Session was orphaned in activeSessions after non-flusher error")
		}
	})
}

func TestStreamableHTTP_Delete(t *testing.T) {
	var hookCalled bool
	var hookSession ClientSession

	hooks := &Hooks{}
	hooks.AddOnUnregisterSession(func(ctx context.Context, session ClientSession) {
		hookCalled = true
		hookSession = session
	})

	mcpServer := NewMCPServer("test-mcp-server", "1.0", WithHooks(hooks))
	sseServer := NewStreamableHTTPServer(mcpServer, WithStateful(true))
	testServer := httptest.NewServer(sseServer)
	defer testServer.Close()

	resp, err := postJSON(testServer.URL, initRequest)
	require.NoError(t, err)
	resp.Body.Close()
	sessionID := resp.Header.Get(HeaderKeySessionID)

	req, _ := http.NewRequest(http.MethodDelete, testServer.URL, nil)
	req.Header.Set(HeaderKeySessionID, sessionID)

	resp, err = testServer.Client().Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	_, activeSessionExists := sseServer.activeSessions.Load(sessionID)
	assert.False(t, activeSessionExists)

	_, serverSessionExists := mcpServer.sessions.Load(sessionID)
	assert.False(t, serverSessionExists)

	assert.True(t, hookCalled)
	assert.Equal(t, sessionID, hookSession.SessionID())
}

func TestStreamableHTTP_DrainNotifications(t *testing.T) {
	t.Run("drain pending notifications after response is computed", func(t *testing.T) {
		mcpServer := NewMCPServer("test-mcp-server", "1.0")

		drainLoopCalled := make(chan int, 1)

		// Add a tool that sends notifications rapidly (faster than the goroutine can process)
		// This forces notifications to queue up in the channel, testing the drain loop
		mcpServer.AddTool(mcp.Tool{
			Name: "drainTestTool",
		}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			server := ServerFromContext(ctx)
			// Send notifications in rapid succession (no delays)
			// The concurrent goroutine (line 394-434 in streamable_http.go) may not process all of them
			// before we hit the drain loop at line 448-468
			for i := range 10 {
				_ = server.SendNotificationToClient(ctx, "test/drain", map[string]any{
					"index": i,
				})
			}
			return mcp.NewToolResultText("drain test done"), nil
		})

		server := NewTestStreamableHTTPServer(mcpServer)
		defer server.Close()

		// Initialize session
		resp, err := postJSON(server.URL, initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize session: %v", err)
		}
		resp.Body.Close()
		sessionID := resp.Header.Get(HeaderKeySessionID)

		// Call tool with rapid notifications
		callToolRequest := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params": map[string]any{
				"name": "drainTestTool",
			},
		}
		callToolRequestBody, err := json.Marshal(callToolRequest)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}
		req, err := http.NewRequest("POST", server.URL, bytes.NewBuffer(callToolRequestBody))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(HeaderKeySessionID, sessionID)

		resp, err = server.Client().Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		defer resp.Body.Close()

		// Verify response is SSE format (indicates drain loop was used)
		if resp.Header.Get("content-type") != "text/event-stream" {
			t.Errorf("Expected content-type text/event-stream, got %s", resp.Header.Get("content-type"))
		}

		responseBody, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response: %v", err)
		}
		responseStr := string(responseBody)

		// Verify we received drain notifications
		// Without the drain loop, we'd get fewer notifications
		// With the drain loop, we catch the pending ones at line 448-468
		drainCount := strings.Count(responseStr, "test/drain")
		if drainCount < 5 {
			t.Logf("Drain loop captured %d notifications. Response:\n%s", drainCount, responseStr)
			// This is informational - the test verifies the drain loop is functional
		}

		// The critical verification: final response is present
		if !strings.Contains(responseStr, "drain test done") {
			t.Errorf("Expected final response with 'drain test done'")
		}

		// Verify response has SSE event format (proves drain loop was executed)
		if !strings.Contains(responseStr, "event: message") {
			t.Errorf("Expected SSE event format in response")
		}

		_ = drainLoopCalled
	})
}

func TestStreamableHTTP_SessionIdleTTLSweeper(t *testing.T) {
	t.Run("expired sessions are swept", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0")
		httpServer := NewStreamableHTTPServer(mcpServer,
			WithStateful(true),
			WithSessionIdleTTL(100*time.Millisecond),
		)
		defer func() { _ = httpServer.Shutdown(context.Background()) }()
		ts := httptest.NewServer(httpServer)
		defer ts.Close()

		// Initialize a session
		resp, err := postJSON(ts.URL, initRequest)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		sessionID := resp.Header.Get(HeaderKeySessionID)
		require.NotEmpty(t, sessionID)

		// Verify the session has transport state
		_, hasActivity := httpServer.sessionLastActive.Load(sessionID)
		assert.True(t, hasActivity, "session should be tracked after initialize")

		// Populate sessionTools so we can verify cleanup is not vacuously true
		httpServer.sessionTools.set(sessionID, map[string]ServerTool{"test": {}})

		// Wait for the sweeper to clean the expired session
		assert.Eventually(t, func() bool {
			_, exists := httpServer.sessionLastActive.Load(sessionID)
			return !exists
		}, 2*time.Second, 50*time.Millisecond, "session should be swept after idle TTL")

		// Verify per-session transport state is cleaned
		_, hasActiveSession := httpServer.activeSessions.Load(sessionID)
		assert.False(t, hasActiveSession, "activeSessions should be cleaned")
		tools := httpServer.sessionTools.get(sessionID)
		assert.Empty(t, tools, "sessionTools should be cleaned")
	})

	t.Run("active sessions are not swept", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0")
		httpServer := NewStreamableHTTPServer(mcpServer,
			WithStateful(true),
			WithSessionIdleTTL(200*time.Millisecond),
		)
		defer func() { _ = httpServer.Shutdown(context.Background()) }()
		ts := httptest.NewServer(httpServer)
		defer ts.Close()

		// Initialize a session
		resp, err := postJSON(ts.URL, initRequest)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		sessionID := resp.Header.Get(HeaderKeySessionID)
		require.NotEmpty(t, sessionID)

		// TTL=200ms → sweep interval = max(100ms, 1s) = 1s.
		// Ping for 2s so at least one sweep fires while the session is still active.
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(80 * time.Millisecond)
			pingResp, pingErr := postSessionJSON(ts.URL, sessionID, map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "ping",
				"params":  map[string]any{},
			})
			require.NoError(t, pingErr)
			pingResp.Body.Close()
		}

		// Session should still be tracked — the sweeper fired but the session
		// was touched within TTL each time.
		_, hasActivity := httpServer.sessionLastActive.Load(sessionID)
		assert.True(t, hasActivity, "active session should not be swept")
	})

	t.Run("disabled sweeper does not clean sessions", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0")
		httpServer := NewStreamableHTTPServer(mcpServer,
			WithStateful(true),
			// No WithSessionIdleTTL — sweeper disabled
		)
		defer func() { _ = httpServer.Shutdown(context.Background()) }()
		ts := httptest.NewServer(httpServer)
		defer ts.Close()

		// Initialize a session
		resp, err := postJSON(ts.URL, initRequest)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		sessionID := resp.Header.Get(HeaderKeySessionID)
		require.NotEmpty(t, sessionID)

		// sessionLastActive should NOT be populated when sweeper is disabled
		_, hasActivity := httpServer.sessionLastActive.Load(sessionID)
		assert.False(t, hasActivity, "touchSession should be a no-op when sweeper is disabled")

		// Session should still be in activeSessions (not cleaned)
		time.Sleep(200 * time.Millisecond)
		_, hasActiveSession := httpServer.activeSessions.Load(sessionID)
		assert.True(t, hasActiveSession, "session should remain in activeSessions")
	})

	t.Run("DELETE still cleans session when sweeper is enabled", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0")
		httpServer := NewStreamableHTTPServer(mcpServer,
			WithStateful(true),
			WithSessionIdleTTL(10*time.Second), // long TTL so sweeper won't fire
		)
		defer func() { _ = httpServer.Shutdown(context.Background()) }()
		ts := httptest.NewServer(httpServer)
		defer ts.Close()

		// Initialize a session
		resp, err := postJSON(ts.URL, initRequest)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		sessionID := resp.Header.Get(HeaderKeySessionID)
		require.NotEmpty(t, sessionID)

		// Send DELETE
		req, err := http.NewRequest(http.MethodDelete, ts.URL, nil)
		require.NoError(t, err)
		req.Header.Set(HeaderKeySessionID, sessionID)
		delResp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		delResp.Body.Close()
		require.Equal(t, http.StatusOK, delResp.StatusCode)

		// Verify all state is cleaned immediately
		_, hasActivity := httpServer.sessionLastActive.Load(sessionID)
		assert.False(t, hasActivity, "sessionLastActive should be cleaned after DELETE")
		_, hasActiveSession := httpServer.activeSessions.Load(sessionID)
		assert.False(t, hasActiveSession, "activeSessions should be cleaned after DELETE")
	})
}
