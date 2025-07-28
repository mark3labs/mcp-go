package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// startMockStreamableHTTPServer starts a test HTTP server that implements
// a minimal Streamable HTTP server for testing purposes.
// It returns the server URL and a function to close the server.
func startMockStreamableHTTPServer() (string, func()) {
	var sessionID string
	var mu sync.Mutex

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle only POST requests
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse incoming JSON-RPC request
		var request map[string]any
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&request); err != nil {
			http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
			return
		}

		method := request["method"]
		switch method {
		case "initialize":
			// Generate a new session ID
			mu.Lock()
			sessionID = fmt.Sprintf("test-session-%d", time.Now().UnixNano())
			mu.Unlock()
			w.Header().Set("Mcp-Session-Id", sessionID)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			if err := json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      request["id"],
				"result":  "initialized",
			}); err != nil {
				http.Error(w, "Failed to encode response", http.StatusInternalServerError)
				return
			}

		case "debug/echo":
			// Check session ID
			if r.Header.Get("Mcp-Session-Id") != sessionID {
				http.Error(w, "Invalid session ID", http.StatusNotFound)
				return
			}

			// Echo back the request as the response result
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if err := json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      request["id"],
				"result":  request,
			}); err != nil {
				http.Error(w, "Failed to encode response", http.StatusInternalServerError)
				return
			}

		case "debug/echo_notification":
			// Check session ID
			if r.Header.Get("Mcp-Session-Id") != sessionID {
				http.Error(w, "Invalid session ID", http.StatusNotFound)
				return
			}

			// Send response and notification
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			notification := map[string]any{
				"jsonrpc": "2.0",
				"method":  "debug/test",
				"params":  request,
			}
			notificationData, _ := json.Marshal(notification)
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", notificationData)
			response := map[string]any{
				"jsonrpc": "2.0",
				"id":      request["id"],
				"result":  request,
			}
			responseData, _ := json.Marshal(response)
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", responseData)

		case "debug/echo_error_string":
			// Check session ID
			if r.Header.Get("Mcp-Session-Id") != sessionID {
				http.Error(w, "Invalid session ID", http.StatusNotFound)
				return
			}

			// Return an error response
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			data, _ := json.Marshal(request)
			if err := json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      request["id"],
				"error": map[string]any{
					"code":    -1,
					"message": string(data),
				},
			}); err != nil {
				http.Error(w, "Failed to encode response", http.StatusInternalServerError)
				return
			}
		}
	})

	// Start test server
	testServer := httptest.NewServer(handler)
	return testServer.URL, testServer.Close
}

func TestStreamableHTTP(t *testing.T) {
	// Start mock server
	url, closeF := startMockStreamableHTTPServer()
	defer closeF()

	// Create transport
	trans, err := NewStreamableHTTP(url)
	if err != nil {
		t.Fatal(err)
	}
	defer trans.Close()

	// Initialize the transport first
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	initRequest := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(int64(0)),
		Method:  "initialize",
	}

	_, err = trans.SendRequest(ctx, initRequest)
	if err != nil {
		t.Fatal(err)
	}

	// Now run the tests
	t.Run("SendRequest", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		params := map[string]any{
			"string": "hello world",
			"array":  []any{1, 2, 3},
		}

		request := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      mcp.NewRequestId(int64(1)),
			Method:  "debug/echo",
			Params:  params,
		}

		// Send the request
		response, err := trans.SendRequest(ctx, request)
		if err != nil {
			t.Fatalf("SendRequest failed: %v", err)
		}

		// Parse the result to verify echo
		var result struct {
			JSONRPC string         `json:"jsonrpc"`
			ID      mcp.RequestId  `json:"id"`
			Method  string         `json:"method"`
			Params  map[string]any `json:"params"`
		}

		if err := json.Unmarshal(response.Result, &result); err != nil {
			t.Fatalf("Failed to unmarshal result: %v", err)
		}

		// Verify response data matches what was sent
		if result.JSONRPC != "2.0" {
			t.Errorf("Expected JSONRPC value '2.0', got '%s'", result.JSONRPC)
		}
		idValue, ok := result.ID.Value().(int64)
		if !ok {
			t.Errorf("Expected ID to be int64, got %T", result.ID.Value())
		} else if idValue != 1 {
			t.Errorf("Expected ID 1, got %d", idValue)
		}
		if result.Method != "debug/echo" {
			t.Errorf("Expected method 'debug/echo', got '%s'", result.Method)
		}

		if str, ok := result.Params["string"].(string); !ok || str != "hello world" {
			t.Errorf("Expected string 'hello world', got %v", result.Params["string"])
		}

		if arr, ok := result.Params["array"].([]any); !ok || len(arr) != 3 {
			t.Errorf("Expected array with 3 items, got %v", result.Params["array"])
		}
	})

	t.Run("SendRequestWithTimeout", func(t *testing.T) {
		// Create a context that's already canceled
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel the context immediately

		// Prepare a request
		request := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      mcp.NewRequestId(int64(3)),
			Method:  "debug/echo",
		}

		// The request should fail because the context is canceled
		_, err := trans.SendRequest(ctx, request)
		if err == nil {
			t.Errorf("Expected context canceled error, got nil")
		} else if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled error, got: %v", err)
		}
	})

	t.Run("SendNotification & NotificationHandler", func(t *testing.T) {
		var wg sync.WaitGroup
		notificationChan := make(chan mcp.JSONRPCNotification, 1)

		// Set notification handler
		trans.SetNotificationHandler(func(notification mcp.JSONRPCNotification) {
			notificationChan <- notification
		})

		// Send a request that triggers a notification
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		request := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      mcp.NewRequestId(int64(1)),
			Method:  "debug/echo_notification",
		}

		_, err := trans.SendRequest(ctx, request)
		if err != nil {
			t.Fatalf("SendRequest failed: %v", err)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case notification := <-notificationChan:
				// We received a notification
				got := notification.Params.AdditionalFields
				if got == nil {
					t.Errorf("Notification handler did not send the expected notification: got nil")
				}
				if int64(got["id"].(float64)) != request.ID.Value().(int64) ||
					got["jsonrpc"] != request.JSONRPC ||
					got["method"] != request.Method {

					responseJson, _ := json.Marshal(got)
					requestJson, _ := json.Marshal(request)
					t.Errorf("Notification handler did not send the expected notification: \ngot %s\nexpect %s", responseJson, requestJson)
				}

			case <-time.After(1 * time.Second):
				t.Errorf("Expected notification, got none")
			}
		}()

		wg.Wait()
	})

	t.Run("MultipleRequests", func(t *testing.T) {
		var wg sync.WaitGroup
		const numRequests = 5

		// Send multiple requests concurrently
		mu := sync.Mutex{}
		responses := make([]*JSONRPCResponse, numRequests)
		errors := make([]error, numRequests)

		for i := 0; i < numRequests; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				// Each request has a unique ID and payload
				request := JSONRPCRequest{
					JSONRPC: "2.0",
					ID:      mcp.NewRequestId(int64(100 + idx)),
					Method:  "debug/echo",
					Params: map[string]any{
						"requestIndex": idx,
						"timestamp":    time.Now().UnixNano(),
					},
				}

				resp, err := trans.SendRequest(ctx, request)
				mu.Lock()
				responses[idx] = resp
				errors[idx] = err
				mu.Unlock()
			}(i)
		}

		wg.Wait()

		// Check results
		for i := 0; i < numRequests; i++ {
			if errors[i] != nil {
				t.Errorf("Request %d failed: %v", i, errors[i])
				continue
			}

			if responses[i] == nil {
				t.Errorf("Request %d: Response is nil", i)
				continue
			}

			expectedId := int64(100 + i)
			idValue, ok := responses[i].ID.Value().(int64)
			if !ok {
				t.Errorf("Request %d: Expected ID to be int64, got %T", i, responses[i].ID.Value())
				continue
			} else if idValue != expectedId {
				t.Errorf("Request %d: Expected ID %d, got %d", i, expectedId, idValue)
				continue
			}

			// Parse the result to verify echo
			var result struct {
				JSONRPC string         `json:"jsonrpc"`
				ID      mcp.RequestId  `json:"id"`
				Method  string         `json:"method"`
				Params  map[string]any `json:"params"`
			}

			if err := json.Unmarshal(responses[i].Result, &result); err != nil {
				t.Errorf("Request %d: Failed to unmarshal result: %v", i, err)
				continue
			}

			// Verify data matches what was sent
			if result.ID.Value().(int64) != expectedId {
				t.Errorf("Request %d: Expected echoed ID %d, got %d", i, expectedId, result.ID.Value().(int64))
			}

			if result.Method != "debug/echo" {
				t.Errorf("Request %d: Expected method 'debug/echo', got '%s'", i, result.Method)
			}

			// Verify the requestIndex parameter
			if idx, ok := result.Params["requestIndex"].(float64); !ok || int(idx) != i {
				t.Errorf("Request %d: Expected requestIndex %d, got %v", i, i, result.Params["requestIndex"])
			}
		}
	})

	t.Run("ResponseError", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Prepare a request
		request := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      mcp.NewRequestId(int64(100)),
			Method:  "debug/echo_error_string",
		}

		reps, err := trans.SendRequest(ctx, request)
		if err != nil {
			t.Errorf("SendRequest failed: %v", err)
		}

		if reps.Error == nil {
			t.Errorf("Expected error, got nil")
		}

		var responseError JSONRPCRequest
		if err := json.Unmarshal([]byte(reps.Error.Message), &responseError); err != nil {
			t.Errorf("Failed to unmarshal result: %v", err)
			return
		}

		if responseError.Method != "debug/echo_error_string" {
			t.Errorf("Expected method 'debug/echo_error_string', got '%s'", responseError.Method)
		}
		idValue, ok := responseError.ID.Value().(int64)
		if !ok {
			t.Errorf("Expected ID to be int64, got %T", responseError.ID.Value())
		} else if idValue != 100 {
			t.Errorf("Expected ID 100, got %d", idValue)
		}
		if responseError.JSONRPC != "2.0" {
			t.Errorf("Expected JSONRPC '2.0', got '%s'", responseError.JSONRPC)
		}
	})

	t.Run("SSEEventWithoutEventField", func(t *testing.T) {
		// Test that SSE events with only data field (no event field) are processed correctly
		// This tests the fix for issue #369

		// Create a custom mock server that sends SSE events without event field
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}

			// Parse incoming JSON-RPC request
			var request map[string]any
			decoder := json.NewDecoder(r.Body)
			if err := decoder.Decode(&request); err != nil {
				http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
				return
			}

			// Send response via SSE WITHOUT event field (only data field)
			// This should be processed as a "message" event according to SSE spec
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)

			response := map[string]any{
				"jsonrpc": "2.0",
				"id":      request["id"],
				"result":  "test response without event field",
			}
			responseBytes, _ := json.Marshal(response)
			// Note: No "event:" field, only "data:" field
			fmt.Fprintf(w, "data: %s\n\n", responseBytes)
		})

		// Create test server
		testServer := httptest.NewServer(handler)
		defer testServer.Close()

		// Create StreamableHTTP transport
		trans, err := NewStreamableHTTP(testServer.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer trans.Close()

		// Send a request
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		request := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      mcp.NewRequestId(int64(1)),
			Method:  "test",
		}

		// This should succeed because the SSE event without event field should be processed
		response, err := trans.SendRequest(ctx, request)
		if err != nil {
			t.Fatalf("SendRequest failed: %v", err)
		}

		if response == nil {
			t.Fatal("Expected response, got nil")
		}

		// Verify the response
		var result string
		if err := json.Unmarshal(response.Result, &result); err != nil {
			t.Fatalf("Failed to unmarshal result: %v", err)
		}

		if result != "test response without event field" {
			t.Errorf("Expected 'test response without event field', got '%s'", result)
		}
	})
}

func TestStreamableHTTPErrors(t *testing.T) {
	t.Run("InvalidURL", func(t *testing.T) {
		// Create a new StreamableHTTP transport with an invalid URL
		_, err := NewStreamableHTTP("://invalid-url")
		if err == nil {
			t.Errorf("Expected error when creating with invalid URL, got nil")
		}
	})

	t.Run("NonExistentURL", func(t *testing.T) {
		// Create a new StreamableHTTP transport with a non-existent URL
		trans, err := NewStreamableHTTP("http://localhost:1")
		if err != nil {
			t.Fatalf("Failed to create StreamableHTTP transport: %v", err)
		}

		// Send request should fail
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		request := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      mcp.NewRequestId(int64(1)),
			Method:  "initialize",
		}

		_, err = trans.SendRequest(ctx, request)
		if err == nil {
			t.Errorf("Expected error when sending request to non-existent URL, got nil")
		}
	})

}

// ---- continuous listening tests ----

// startMockStreamableWithGETSupport starts a test HTTP server that implements
// a minimal Streamable HTTP server for testing purposes with support for GET requests
// to test the continuous listening feature.
func startMockStreamableWithGETSupport(getSupport bool) (string, func(), chan bool, int) {
	var sessionID string
	var mu sync.Mutex
	disconnectCh := make(chan bool, 1)
	notificationCount := 0
	var notificationMu sync.Mutex

	sendNotification := func() {
		notificationMu.Lock()
		notificationCount++
		notificationMu.Unlock()
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle POST requests for initialization
		if r.Method == http.MethodPost {
			// Parse incoming JSON-RPC request
			var request map[string]any
			decoder := json.NewDecoder(r.Body)
			if err := decoder.Decode(&request); err != nil {
				http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
				return
			}

			method := request["method"]
			if method == "initialize" {
				// Generate a new session ID
				mu.Lock()
				sessionID = fmt.Sprintf("test-session-%d", time.Now().UnixNano())
				mu.Unlock()
				w.Header().Set("Mcp-Session-Id", sessionID)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusAccepted)
				if err := json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      request["id"],
					"result":  "initialized",
				}); err != nil {
					http.Error(w, "Failed to encode response", http.StatusInternalServerError)
					return
				}
			}
			return
		}

		// Handle GET requests for continuous listening
		if r.Method == http.MethodGet {
			if !getSupport {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}

			// Check session ID
			if recvSessionID := r.Header.Get("Mcp-Session-Id"); recvSessionID != sessionID {
				http.Error(w, "Invalid session ID", http.StatusNotFound)
				return
			}

			// Setup SSE connection
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "Streaming not supported", http.StatusInternalServerError)
				return
			}

			// Send a notification
			notification := map[string]any{
				"jsonrpc": "2.0",
				"method":  "test/notification",
				"params":  map[string]any{"message": "Hello from server"},
			}
			notificationData, _ := json.Marshal(notification)
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", notificationData)
			flusher.Flush()
			sendNotification()

			// Keep the connection open or disconnect as requested
			select {
			case <-disconnectCh:
				// Force disconnect
				return
			case <-r.Context().Done():
				// Client disconnected
				return
			case <-time.After(50 * time.Millisecond):
				// Send another notification
				notification = map[string]any{
					"jsonrpc": "2.0",
					"method":  "test/notification",
					"params":  map[string]any{"message": "Second notification"},
				}
				notificationData, _ = json.Marshal(notification)
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", notificationData)
				flusher.Flush()
				sendNotification()
				return
			}
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
	})

	// Start test server
	testServer := httptest.NewServer(handler)

	notificationMu.Lock()
	defer notificationMu.Unlock()

	return testServer.URL, testServer.Close, disconnectCh, notificationCount
}

func TestContinuousListening(t *testing.T) {
	retryInterval = 10 * time.Millisecond
	// Start mock server with GET support
	url, closeServer, disconnectCh, _ := startMockStreamableWithGETSupport(true)

	// Create transport with continuous listening enabled
	trans, err := NewStreamableHTTP(url, WithContinuousListening())
	if err != nil {
		t.Fatal(err)
	}

	// Ensure transport is closed before server to avoid connection refused errors
	defer func() {
		trans.Close()
		closeServer()
	}()

	// Setup notification handler
	notificationReceived := make(chan struct{}, 10)
	trans.SetNotificationHandler(func(notification mcp.JSONRPCNotification) {
		notificationReceived <- struct{}{}
	})

	// Start the transport - this will launch listenForever in a goroutine
	if err := trans.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Initialize the transport first
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	initRequest := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(int64(0)),
		Method:  "initialize",
	}

	_, err = trans.SendRequest(ctx, initRequest)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for notifications to be received
	notificationCount := 0
	for notificationCount < 2 {
		select {
		case <-notificationReceived:
			notificationCount++
		case <-time.After(3 * time.Second):
			t.Fatalf("Timed out waiting for notifications, received %d", notificationCount)
			return
		}
	}

	// Test server disconnect and reconnect
	disconnectCh <- true
	time.Sleep(50 * time.Millisecond) // Allow time for reconnection

	// Verify reconnect occurred by receiving more notifications
	reconnectNotificationCount := 0
	for reconnectNotificationCount < 2 {
		select {
		case <-notificationReceived:
			reconnectNotificationCount++
		case <-time.After(3 * time.Second):
			t.Fatalf("Timed out waiting for notifications after reconnect")
			return
		}
	}
}

func TestContinuousListeningMethodNotAllowed(t *testing.T) {

	// Start a server that doesn't support GET
	url, closeServer, _, _ := startMockStreamableWithGETSupport(false)

	// Setup logger to capture log messages
	logChan := make(chan string, 10)
	testLogger := &testLogger{logChan: logChan}

	// Create transport with continuous listening enabled and custom logger
	trans, err := NewStreamableHTTP(url, WithContinuousListening(), WithLogger(testLogger))
	if err != nil {
		t.Fatal(err)
	}

	// Ensure transport is closed before server to avoid connection refused errors
	defer func() {
		trans.Close()
		closeServer()
	}()

	// Initialize the transport first
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start the transport
	if err := trans.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	initRequest := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(int64(0)),
		Method:  "initialize",
	}

	_, err = trans.SendRequest(ctx, initRequest)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the error log message that server doesn't support listening
	select {
	case logMsg := <-logChan:
		if !strings.Contains(logMsg, "server does not support listening") {
			t.Errorf("Expected error log about server not supporting listening, got: %s", logMsg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for log message")
	}
}

// testLogger is a simple logger for testing
type testLogger struct {
	logChan chan string
}

func (l *testLogger) Infof(format string, args ...any) {
	// Intentionally left empty
}

func (l *testLogger) Errorf(format string, args ...any) {
	l.logChan <- fmt.Sprintf(format, args...)
}
