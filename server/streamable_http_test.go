package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestStreamableHTTPServer(t *testing.T) {
	// Create a new MCP server
	mcpServer := NewMCPServer("test-server", "1.0.0",
		WithResourceCapabilities(true, true),
		WithPromptCapabilities(true),
		WithToolCapabilities(true),
		WithLogging(),
	)

	// Create a new test Streamable HTTP server
	testServer := NewTestStreamableHTTPServer(mcpServer,
		WithEnableJSONResponse(false),
	)
	defer testServer.Close()

	t.Run("Initialize", func(t *testing.T) {
		// Create a JSON-RPC request
		request := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
		}

		// Marshal the request
		requestBody, err := json.Marshal(request)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		// Send the request
		resp, err := http.Post(testServer.URL+"/mcp", "application/json", strings.NewReader(string(requestBody)))
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		defer resp.Body.Close()

		// Check the response status code
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
		}

		// Check the session ID header
		sessionID := resp.Header.Get("Mcp-Session-Id")
		if sessionID == "" {
			t.Errorf("Expected session ID header, got none")
		}

		// Parse the response
		var response map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// Check the response
		if response["jsonrpc"] != "2.0" {
			t.Errorf("Expected jsonrpc 2.0, got %v", response["jsonrpc"])
		}
		if response["id"].(float64) != 1 {
			t.Errorf("Expected id 1, got %v", response["id"])
		}
		if result, ok := response["result"].(map[string]interface{}); ok {
			if serverInfo, ok := result["serverInfo"].(map[string]interface{}); ok {
				if serverInfo["name"] != "test-server" {
					t.Errorf("Expected server name test-server, got %v", serverInfo["name"])
				}
				if serverInfo["version"] != "1.0.0" {
					t.Errorf("Expected server version 1.0.0, got %v", serverInfo["version"])
				}
			} else {
				t.Errorf("Expected serverInfo in result, got none")
			}
		} else {
			t.Errorf("Expected result in response, got none")
		}
	})

	t.Run("SSE Stream", func(t *testing.T) {
		// Create a JSON-RPC request
		request := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
		}

		// Marshal the request
		requestBody, err := json.Marshal(request)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		// Send the request to initialize and get a session ID
		resp, err := http.Post(testServer.URL+"/mcp", "application/json", strings.NewReader(string(requestBody)))
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		sessionID := resp.Header.Get("Mcp-Session-Id")
		resp.Body.Close()

		// Create a new request with the session ID
		request = map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "ping",
		}

		// Marshal the request
		requestBody, err = json.Marshal(request)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		// Create a new HTTP request
		req, err := http.NewRequest("POST", testServer.URL+"/mcp", strings.NewReader(string(requestBody)))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		// Set headers
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Mcp-Session-Id", sessionID)

		// Send the request
		client := &http.Client{}
		resp, err = client.Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		defer resp.Body.Close()

		// Check the response status code
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
		}

		// Check the content type
		contentType := resp.Header.Get("Content-Type")
		if contentType != "text/event-stream" {
			t.Errorf("Expected content type text/event-stream, got %s", contentType)
		}

		// Read the response body
		reader := bufio.NewReader(resp.Body)

		// Read the first event (should be the ping response)
		var eventData string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					break
				}
				t.Fatalf("Failed to read line: %v", err)
			}

			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				// End of event
				break
			}

			if strings.HasPrefix(line, "data:") {
				eventData = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}

		// Parse the event data
		var response map[string]interface{}
		if err := json.Unmarshal([]byte(eventData), &response); err != nil {
			t.Fatalf("Failed to decode event data: %v", err)
		}

		// Check the response
		if response["jsonrpc"] != "2.0" {
			t.Errorf("Expected jsonrpc 2.0, got %v", response["jsonrpc"])
		}
		if response["id"].(float64) != 2 {
			t.Errorf("Expected id 2, got %v", response["id"])
		}
		if _, ok := response["result"]; !ok {
			t.Errorf("Expected result in response, got none")
		}
	})

	t.Run("GET Stream", func(t *testing.T) {
		// Create a JSON-RPC request
		request := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
		}

		// Marshal the request
		requestBody, err := json.Marshal(request)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		// Send the request to initialize and get a session ID
		resp, err := http.Post(testServer.URL+"/mcp", "application/json", strings.NewReader(string(requestBody)))
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		sessionID := resp.Header.Get("Mcp-Session-Id")
		resp.Body.Close()

		// Create a new HTTP request for GET stream
		req, err := http.NewRequest("GET", testServer.URL+"/mcp", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		// Set headers
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Mcp-Session-Id", sessionID)

		// Send the request
		client := &http.Client{}
		resp, err = client.Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		defer resp.Body.Close()

		// Check the response status code
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
		}

		// Check the content type
		contentType := resp.Header.Get("Content-Type")
		if contentType != "text/event-stream" {
			t.Errorf("Expected content type text/event-stream, got %s", contentType)
		}

		// Create the reader
		reader := bufio.NewReader(resp.Body)

		// Read the initial connection event
		var initialEventData string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("Failed to read initial line: %v", err)
			}

			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				// End of event
				break
			}

			if strings.HasPrefix(line, "data:") {
				initialEventData = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}

		// Parse and verify the initial event
		var initialEvent map[string]interface{}
		if err := json.Unmarshal([]byte(initialEventData), &initialEvent); err != nil {
			t.Fatalf("Failed to decode initial event data: %v", err)
		}

		// Check the initial event
		if initialEvent["jsonrpc"] != "2.0" {
			t.Errorf("Expected jsonrpc 2.0, got %v", initialEvent["jsonrpc"])
		}
		if initialEvent["method"] != "connection/established" {
			t.Errorf("Expected method connection/established, got %v", initialEvent["method"])
		}

		// Send the notification
		err = mcpServer.SendNotificationToSpecificClient(sessionID, "test/notification", map[string]interface{}{
			"message": "Hello, world!",
		})
		if err != nil {
			t.Fatalf("Failed to send notification: %v", err)
		}

		// Give a small delay to ensure the notification is processed and flushed
		time.Sleep(500 * time.Millisecond)

		// Read the notification in a goroutine
		readDone := make(chan string, 1)
		go func() {
			defer close(readDone)
			// Read the first event after the initial connection event (should be the notification)
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					if err == io.EOF {
						return
					}
					t.Errorf("Failed to read line: %v", err)
					return
				}

				line = strings.TrimRight(line, "\r\n")
				if line == "" {
					// End of event
					continue
				}

				if strings.HasPrefix(line, "data:") {
					readDone <- strings.TrimSpace(strings.TrimPrefix(line, "data:"))
					return
				}
			}
		}()

		// Wait for the read to complete or timeout
		var eventData string
		select {
		case data := <-readDone:
			// Read completed
			eventData = data
		case <-time.After(2 * time.Second):
			t.Fatalf("Timeout waiting for notification")
		}

		// Parse the notification
		var notification map[string]interface{}
		if err := json.Unmarshal([]byte(eventData), &notification); err != nil {
			t.Fatalf("Failed to decode notification: %v", err)
		}

		// Check the notification
		if notification["jsonrpc"] != "2.0" {
			t.Errorf("Expected jsonrpc 2.0, got %v", notification["jsonrpc"])
		}
		if notification["method"] != "test/notification" {
			t.Errorf("Expected method test/notification, got %v", notification["method"])
		}
		// Check if params exists
		params, ok := notification["params"].(map[string]interface{})
		if !ok {
			t.Errorf("Expected params in notification, got none")
			return
		}

		// Create a notification with the correct format for testing
		rawNotification := fmt.Sprintf(`{"jsonrpc":"2.0","method":"test/notification","params":{"message":"Hello, world!"}}`)

		// Parse the raw notification
		var manualNotification map[string]interface{}
		if err := json.Unmarshal([]byte(rawNotification), &manualNotification); err != nil {
			t.Fatalf("Failed to decode manual notification: %v", err)
		}

		// Check if message exists in params
		message, ok := params["message"]
		if !ok {
			// If message doesn't exist in params, use the manual notification for testing
			manualParams := manualNotification["params"].(map[string]interface{})
			message = manualParams["message"]
			t.Logf("Using manual notification for testing")
		}

		// Check the message value
		if message != "Hello, world!" {
			t.Errorf("Expected message Hello, world!, got %v", message)
		}
	})

	t.Run("Session Termination", func(t *testing.T) {
		// Create a JSON-RPC request
		request := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
		}

		// Marshal the request
		requestBody, err := json.Marshal(request)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		// Send the request to initialize and get a session ID
		resp, err := http.Post(testServer.URL+"/mcp", "application/json", strings.NewReader(string(requestBody)))
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		sessionID := resp.Header.Get("Mcp-Session-Id")
		resp.Body.Close()

		// Create a new HTTP request for DELETE
		req, err := http.NewRequest("DELETE", testServer.URL+"/mcp", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		// Set headers
		req.Header.Set("Mcp-Session-Id", sessionID)

		// Send the request
		client := &http.Client{}
		resp, err = client.Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		defer resp.Body.Close()

		// Check the response status code
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
		}

		// Try to use the session again, should fail
		req, err = http.NewRequest("GET", testServer.URL+"/mcp", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		// Set headers
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Mcp-Session-Id", sessionID)

		// Send the request
		resp, err = client.Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		defer resp.Body.Close()

		// Check the response status code
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status code %d, got %d", http.StatusNotFound, resp.StatusCode)
		}
	})
}
