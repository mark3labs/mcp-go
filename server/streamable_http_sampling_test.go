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
	session := newStreamableHttpSession(sessionID, httpServer.sessionTools, httpServer.sessionLogLevels)

	// Verify it implements SessionWithSampling
	_, ok := interface{}(session).(SessionWithSampling)
	if !ok {
		t.Error("streamableHttpSession should implement SessionWithSampling")
	}

	// Test that sampling request channels are initialized
	if session.samplingRequestChan == nil {
		t.Error("samplingRequestChan should be initialized")
	}
	if session.samplingResponseChan == nil {
		t.Error("samplingResponseChan should be initialized")
	}
}

// TestStreamableHTTPServer_SamplingErrorHandling tests error scenarios
func TestStreamableHTTPServer_SamplingErrorHandling(t *testing.T) {
	mcpServer := NewMCPServer("test-server", "1.0.0")
	mcpServer.EnableSampling()

	httpServer := NewStreamableHTTPServer(mcpServer)
	testServer := httptest.NewServer(httpServer)
	defer testServer.Close()

	client := &http.Client{}
	baseURL := testServer.URL

	// Test sending sampling response without session ID
	samplingResponse := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"result": map[string]interface{}{
			"role": "assistant",
			"content": map[string]interface{}{
				"type": "text",
				"text": "Test response",
			},
		},
	}

	responseBody, _ := json.Marshal(samplingResponse)
	resp, err := client.Post(baseURL, "application/json", bytes.NewReader(responseBody))
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for missing session ID, got %d", resp.StatusCode)
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
	session := newStreamableHttpSession(sessionID, httpServer.sessionTools, httpServer.sessionLogLevels)

	// Verify it implements SessionWithSampling
	_, ok := interface{}(session).(SessionWithSampling)
	if !ok {
		t.Error("streamableHttpSession should implement SessionWithSampling")
	}

	// Test RequestSampling with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
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
	session := newStreamableHttpSession(sessionID, nil, nil)

	// Fill the sampling request queue
	for i := 0; i < cap(session.samplingRequestChan); i++ {
		session.samplingRequestChan <- samplingRequestItem{
			requestID: int64(i),
			request:   mcp.CreateMessageRequest{},
			response:  make(chan samplingResponseItem, 1),
		}
	}

	// Try to add another request (should fail)
	ctx := context.Background()
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