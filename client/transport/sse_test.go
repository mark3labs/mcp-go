package transport

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/mark3labs/mcp-go/mcp"
)

// startMockSSEEchoServer starts a test HTTP server that implements
// a minimal SSE-based echo server for testing purposes.
// It returns the server URL and a function to close the server.
func startMockSSEEchoServer() (string, func()) {
	// Create handler for SSE endpoint
	var sseWriter http.ResponseWriter
	var flush func()
	var mu sync.Mutex
	sseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Setup SSE headers
		defer func() {
			mu.Lock() // for passing race test
			sseWriter = nil
			flush = nil
			mu.Unlock()
			fmt.Printf("SSEHandler ends: %v\n", r.Context().Err())
		}()

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		mu.Lock()
		sseWriter = w
		flush = flusher.Flush
		mu.Unlock()

		// Send initial endpoint event with message endpoint URL
		mu.Lock()
		fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", "/message")
		flusher.Flush()
		mu.Unlock()

		// Keep connection open
		<-r.Context().Done()
	})

	// Create handler for message endpoint
	messageHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		// Echo back the request as the response result
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      request["id"],
			"result":  request,
		}

		method := request["method"]
		switch method {
		case "debug/echo":
			response["result"] = request
		case "debug/echo_notification":
			response["result"] = request
			// send notification to client
			responseBytes, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0",
				"method":  "debug/test",
				"params":  request,
			})
			mu.Lock()
			fmt.Fprintf(sseWriter, "event: message\ndata: %s\n\n", responseBytes)
			flush()
			mu.Unlock()
		case "debug/echo_error_string":
			data, _ := json.Marshal(request)
			response["error"] = map[string]any{
				"code":    -1,
				"message": string(data),
			}
		}

		// Set response headers
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)

		go func() {
			data, _ := json.Marshal(response)
			mu.Lock()
			defer mu.Unlock()
			if sseWriter != nil && flush != nil {
				fmt.Fprintf(sseWriter, "event: message\ndata: %s\n\n", data)
				flush()
			}
		}()

	})

	// Create a router to handle different endpoints
	mux := http.NewServeMux()
	mux.Handle("/", sseHandler)
	mux.Handle("/message", messageHandler)

	// Start test server
	testServer := httptest.NewServer(mux)

	return testServer.URL, testServer.Close
}

func TestSSE(t *testing.T) {
	// Compile mock server
	url, closeF := startMockSSEEchoServer()
	defer closeF()

	trans, err := NewSSE(url)
	if err != nil {
		t.Fatal(err)
	}

	// Start the transport
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err = trans.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start transport: %v", err)
	}
	defer trans.Close()

	t.Run("SendRequest", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		params := map[string]any{
			"string": "hello world",
			"array":  []any{1, 2, 3},
		}

		request := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      1,
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
			ID      int64          `json:"id"`
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
		if result.ID != 1 {
			t.Errorf("Expected ID 1, got %d", result.ID)
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
			ID:      3,
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

		// Send a notification
		// This would trigger a notification from the server
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		notification := mcp.JSONRPCNotification{
			JSONRPC: "2.0",
			Notification: mcp.Notification{
				Method: "debug/echo_notification",
				Params: mcp.NotificationParams{
					AdditionalFields: map[string]any{"test": "value"},
				},
			},
		}
		err := trans.SendNotification(ctx, notification)
		if err != nil {
			t.Fatalf("SendNotification failed: %v", err)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case nt := <-notificationChan:
				// We received a notification
				responseJson, _ := json.Marshal(nt.Params.AdditionalFields)
				requestJson, _ := json.Marshal(notification)
				if string(responseJson) != string(requestJson) {
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
					ID:      int64(100 + idx),
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

			if responses[i] == nil || responses[i].ID == nil || *responses[i].ID != int64(100+i) {
				t.Errorf("Request %d: Expected ID %d, got %v", i, 100+i, responses[i])
				continue
			}

			// Parse the result to verify echo
			var result struct {
				JSONRPC string         `json:"jsonrpc"`
				ID      int64          `json:"id"`
				Method  string         `json:"method"`
				Params  map[string]any `json:"params"`
			}

			if err := json.Unmarshal(responses[i].Result, &result); err != nil {
				t.Errorf("Request %d: Failed to unmarshal result: %v", i, err)
				continue
			}

			// Verify data matches what was sent
			if result.ID != int64(100+i) {
				t.Errorf("Request %d: Expected echoed ID %d, got %d", i, 100+i, result.ID)
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

		// Prepare a request
		request := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      100,
			Method:  "debug/echo_error_string",
		}

		// The request should fail because the context is canceled
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
		}

		if responseError.Method != "debug/echo_error_string" {
			t.Errorf("Expected method 'debug/echo_error_string', got '%s'", responseError.Method)
		}
		if responseError.ID != 100 {
			t.Errorf("Expected ID 100, got %d", responseError.ID)
		}
		if responseError.JSONRPC != "2.0" {
			t.Errorf("Expected JSONRPC '2.0', got '%s'", responseError.JSONRPC)
		}
	})

}

func TestSSEErrors(t *testing.T) {
	t.Run("InvalidURL", func(t *testing.T) {
		// Create a new SSE transport with an invalid URL
		_, err := NewSSE("://invalid-url")
		if err == nil {
			t.Errorf("Expected error when creating with invalid URL, got nil")
		}
	})

	t.Run("NonExistentURL", func(t *testing.T) {
		// Create a new SSE transport with a non-existent URL
		sse, err := NewSSE("http://localhost:1")
		if err != nil {
			t.Fatalf("Failed to create SSE transport: %v", err)
		}

		// Start should fail
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err = sse.Start(ctx)
		if err == nil {
			t.Errorf("Expected error when starting with non-existent URL, got nil")
			sse.Close()
		}
	})

	t.Run("WithHTTPClient", func(t *testing.T) {
		// Create a custom client with a very short timeout
		customClient := &http.Client{Timeout: 1 * time.Nanosecond}

		url, closeF := startMockSSEEchoServer()
		defer closeF()
		// Initialize SSE transport with the custom HTTP client
		trans, err := NewSSE(url, WithHTTPClient(customClient))
		if err != nil {
			t.Fatalf("Failed to create SSE with custom client: %v", err)
		}

		// Starting should immediately error due to timeout
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = trans.Start(ctx)
		if err == nil {
			t.Error("Expected Start to fail with custom timeout, got nil")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Expected error 'context deadline exceeded', got '%s'", err.Error())
		}
		trans.Close()
	})

	t.Run("RequestBeforeStart", func(t *testing.T) {
		url, closeF := startMockSSEEchoServer()
		defer closeF()

		// Create a new SSE instance without calling Start method
		sse, err := NewSSE(url)
		if err != nil {
			t.Fatalf("Failed to create SSE transport: %v", err)
		}

		// Prepare a request
		request := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      99,
			Method:  "ping",
		}

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		_, err = sse.SendRequest(ctx, request)
		if err == nil {
			t.Errorf("Expected SendRequest to fail before Start(), but it didn't")
		}
	})

	t.Run("RequestAfterClose", func(t *testing.T) {
		// Start a mock server
		url, closeF := startMockSSEEchoServer()
		defer closeF()

		// Create a new SSE transport
		sse, err := NewSSE(url)
		if err != nil {
			t.Fatalf("Failed to create SSE transport: %v", err)
		}

		// Start the transport
		ctx := context.Background()
		if err := sse.Start(ctx); err != nil {
			t.Fatalf("Failed to start SSE transport: %v", err)
		}

		// Close the transport
		sse.Close()

		// Wait a bit to ensure connection has closed
		time.Sleep(100 * time.Millisecond)

		// Try to send a request after close
		request := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "ping",
		}

		_, err = sse.SendRequest(ctx, request)
		if err == nil {
			t.Errorf("Expected error when sending request after close, got nil")
		}
	})

}
