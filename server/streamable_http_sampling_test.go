package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestStreamableHTTPServer_SamplingBasic tests basic sampling session functionality
func TestStreamableHTTPServer_SamplingBasic(t *testing.T) {
	// Create MCP server with sampling enabled
	mcpServer := NewMCPServer("test-server", "1.0.0")
	mcpServer.EnableSampling()

	// Create HTTP server
	httpServer := NewStreamableHTTPServer(mcpServer)
	testServer := httptest.NewServer(httpServer)
	defer testServer.Close()

	// Test session creation and interface implementation
	sessionID := "test-session"
	session := newStreamableHttpSession(sessionID, httpServer.sessionTools, httpServer.sessionResources, httpServer.sessionResourceTemplates, httpServer.sessionLogLevels)

	// Verify it implements SessionWithSampling
	_, ok := any(session).(SessionWithSampling)
	if !ok {
		t.Error("streamableHttpSession should implement SessionWithSampling")
	}

	// Test that sampling request channels are initialized
	if session.samplingRequestChan == nil {
		t.Error("samplingRequestChan should be initialized")
	}
}

// TestStreamableHTTPServer_SamplingErrorHandling tests error scenarios
func TestStreamableHTTPServer_SamplingErrorHandling(t *testing.T) {
	mcpServer := NewMCPServer("test-server", "1.0.0")
	mcpServer.EnableSampling()

	httpServer := NewStreamableHTTPServer(mcpServer, WithStateLess(true))
	testServer := httptest.NewServer(httpServer)
	defer testServer.Close()

	client := &http.Client{}
	baseURL := testServer.URL

	tests := []struct {
		name           string
		sessionID      string
		body           map[string]any
		expectedStatus int
	}{
		{
			name:      "missing session ID",
			sessionID: "",
			body: map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result": map[string]any{
					"role": "assistant",
					"content": map[string]any{
						"type": "text",
						"text": "Test response",
					},
				},
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:      "invalid request ID",
			sessionID: "any-session-id",
			body: map[string]any{
				"jsonrpc": "2.0",
				"id":      "invalid-id",
				"result": map[string]any{
					"role": "assistant",
					"content": map[string]any{
						"type": "text",
						"text": "Test response",
					},
				},
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:      "malformed result",
			sessionID: "any-session-id",
			body: map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  "invalid-result",
			},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, _ := json.Marshal(tt.body)
			req, err := http.NewRequest("POST", baseURL, bytes.NewReader(payload))
			if err != nil {
				t.Errorf("Failed to create request: %v", err)
				return
			}
			req.Header.Set("Content-Type", "application/json")
			if tt.sessionID != "" {
				req.Header.Set("Mcp-Session-Id", tt.sessionID)
			}

			resp, err := client.Do(req)
			if err != nil {
				t.Errorf("Failed to send request: %v", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}
		})
	}
}

// TestStreamableHTTPServer_SamplingInterface verifies interface implementation
func TestStreamableHTTPServer_SamplingInterface(t *testing.T) {
	mcpServer := NewMCPServer("test-server", "1.0.0")
	mcpServer.EnableSampling()
	httpServer := NewStreamableHTTPServer(mcpServer)
	testServer := httptest.NewServer(httpServer)
	defer testServer.Close()

	// Create a session
	sessionID := "test-session"
	session := newStreamableHttpSession(sessionID, httpServer.sessionTools, httpServer.sessionResources, httpServer.sessionResourceTemplates, httpServer.sessionLogLevels)

	// Verify it implements SessionWithSampling
	_, ok := any(session).(SessionWithSampling)
	if !ok {
		t.Error("streamableHttpSession should implement SessionWithSampling")
	}

	// Test RequestSampling with timeout
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	request := mcp.CreateMessageRequest{
		CreateMessageParams: mcp.CreateMessageParams{
			Messages: []mcp.SamplingMessage{
				{
					Role: mcp.RoleUser,
					Content: mcp.TextContent{
						Type: "text",
						Text: "Test message",
					},
				},
			},
		},
	}

	_, err := session.RequestSampling(ctx, request)
	if err == nil {
		t.Error("Expected timeout error, but got nil")
	}

	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("Expected timeout error, got: %v", err)
	}
}

// TestStreamableHTTPServer_SamplingQueueFull tests queue overflow scenarios
func TestStreamableHTTPServer_SamplingQueueFull(t *testing.T) {
	sessionID := "test-session"
	session := newStreamableHttpSession(sessionID, nil, nil, nil, nil)

	// Fill the sampling request queue
	for i := 0; i < cap(session.samplingRequestChan); i++ {
		session.samplingRequestChan <- samplingRequestItem{
			requestID: int64(i),
			request:   mcp.CreateMessageRequest{},
			response:  make(chan samplingResponseItem, 1),
		}
	}

	// Try to add another request (should fail)
	ctx := t.Context()
	request := mcp.CreateMessageRequest{
		CreateMessageParams: mcp.CreateMessageParams{
			Messages: []mcp.SamplingMessage{
				{
					Role: mcp.RoleUser,
					Content: mcp.TextContent{
						Type: "text",
						Text: "Test message",
					},
				},
			},
		},
	}

	_, err := session.RequestSampling(ctx, request)
	if err == nil {
		t.Error("Expected queue full error, but got nil")
	}

	if !strings.Contains(err.Error(), "queue is full") {
		t.Errorf("Expected queue full error, got: %v", err)
	}
}

// TestStreamableHTTPServer_ClientRequestErrorIncludesMethod verifies JSON-RPC client
// errors include the originating MCP method (e.g. elicitation/create vs sampling/createMessage).
func TestStreamableHTTPServer_ClientRequestErrorIncludesMethod(t *testing.T) {
	mcpServer := NewMCPServer("test-server", "1.0.0")
	mcpServer.EnableSampling()

	httpServer := NewStreamableHTTPServer(mcpServer, WithStateLess(true))
	testServer := httptest.NewServer(httpServer)
	defer testServer.Close()

	sessionID := "test-session-elicitation-error"
	session := newStreamableHttpSession(sessionID, httpServer.sessionTools, httpServer.sessionResources, httpServer.sessionResourceTemplates, httpServer.sessionLogLevels)
	httpServer.activeSessions.Store(sessionID, session)

	requestID := int64(99)
	responseChan := make(chan samplingResponseItem, 1)
	session.samplingRequests.Store(requestID, pendingClientRequest{
		response: responseChan,
		method:   mcp.MethodElicitationCreate,
	})

	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      requestID,
		"error": map[string]any{
			"code":    -32601,
			"message": "Method not found",
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("Failed to marshal body: %v", err)
	}

	req, err := http.NewRequest("POST", testServer.URL, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Session-Id", sessionID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("Expected status %d, got %d", http.StatusAccepted, resp.StatusCode)
	}

	select {
	case response := <-responseChan:
		if response.err == nil {
			t.Fatal("Expected error response, got nil")
		}
		expected := "elicitation/create error -32601: Method not found"
		if response.err.Error() != expected {
			t.Errorf("Expected error %q, got %q", expected, response.err.Error())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for elicitation error response")
	}
}
