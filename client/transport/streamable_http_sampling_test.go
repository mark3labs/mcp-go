package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestStreamableHTTP_SamplingFlow tests the complete sampling flow with HTTP transport
func TestStreamableHTTP_SamplingFlow(t *testing.T) {
	// Create simple test server 
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Just respond OK to any requests
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	
	// Create HTTP client transport
	client, err := NewStreamableHTTP(server.URL)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	
	// Set up sampling request handler
	var handledRequest *JSONRPCRequest
	handlerCalled := make(chan struct{})
	client.SetRequestHandler(func(ctx context.Context, request JSONRPCRequest) (*JSONRPCResponse, error) {
		handledRequest = &request
		close(handlerCalled)
		
		// Simulate sampling handler response
		result := map[string]any{
			"role": "assistant",
			"content": map[string]any{
				"type": "text",
				"text": "Hello! How can I help you today?",
			},
			"model":      "test-model",
			"stopReason": "stop_sequence",
		}
		
		resultBytes, _ := json.Marshal(result)
		
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Result:  resultBytes,
		}, nil
	})
	
	// Start the client
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	err = client.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}
	
	// Test direct request handling (simulating a sampling request)
	samplingRequest := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(1),
		Method:  string(mcp.MethodSamplingCreateMessage),
		Params: map[string]any{
			"messages": []map[string]any{
				{
					"role": "user",
					"content": map[string]any{
						"type": "text",
						"text": "Hello, world!",
					},
				},
			},
		},
	}
	
	// Directly test request handling
	client.handleIncomingRequest(ctx, samplingRequest)
	
	// Wait for handler to be called
	select {
	case <-handlerCalled:
		// Handler was called
	case <-time.After(1 * time.Second):
		t.Fatal("Handler was not called within timeout")
	}
	
	// Verify the request was handled
	if handledRequest == nil {
		t.Fatal("Sampling request was not handled")
	}
	
	if handledRequest.Method != string(mcp.MethodSamplingCreateMessage) {
		t.Errorf("Expected method %s, got %s", mcp.MethodSamplingCreateMessage, handledRequest.Method)
	}
}

// TestStreamableHTTP_SamplingErrorHandling tests error handling in sampling requests
func TestStreamableHTTP_SamplingErrorHandling(t *testing.T) {
	var errorHandled sync.WaitGroup
	errorHandled.Add(1)
	
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Logf("Failed to decode body: %v", err)
				w.WriteHeader(http.StatusOK)
				return
			}
			
			// Check if this is an error response
			if errorField, ok := body["error"]; ok {
				errorMap := errorField.(map[string]any)
				if code, ok := errorMap["code"].(float64); ok && code == -32603 {
					errorHandled.Done()
					w.WriteHeader(http.StatusOK)
					return
				}
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	
	client, err := NewStreamableHTTP(server.URL)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	
	// Set up request handler that returns an error
	client.SetRequestHandler(func(ctx context.Context, request JSONRPCRequest) (*JSONRPCResponse, error) {
		return nil, fmt.Errorf("sampling failed")
	})
	
	// Start the client
	ctx := context.Background()
	err = client.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}
	
	// Simulate incoming sampling request
	samplingRequest := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(1),
		Method:  string(mcp.MethodSamplingCreateMessage),
		Params:  map[string]any{},
	}
	
	// This should trigger error handling
	client.handleIncomingRequest(ctx, samplingRequest)
	
	// Wait for error to be handled
	errorHandled.Wait()
}

// TestStreamableHTTP_NoSamplingHandler tests behavior when no sampling handler is set
func TestStreamableHTTP_NoSamplingHandler(t *testing.T) {
	var errorReceived bool
	errorReceivedChan := make(chan struct{})
	
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Logf("Failed to decode body: %v", err)
				w.WriteHeader(http.StatusOK)
				return
			}
			
			// Check if this is an error response with method not found
			if errorField, ok := body["error"]; ok {
				errorMap := errorField.(map[string]any)
				if code, ok := errorMap["code"].(float64); ok && code == -32601 {
					if message, ok := errorMap["message"].(string); ok && 
						strings.Contains(message, "no handler configured") {
						errorReceived = true
						close(errorReceivedChan)
					}
				}
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	
	client, err := NewStreamableHTTP(server.URL)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	
	// Don't set any request handler
	
	ctx := context.Background()
	err = client.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}
	
	// Simulate incoming sampling request
	samplingRequest := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(1),
		Method:  string(mcp.MethodSamplingCreateMessage),
		Params:  map[string]any{},
	}
	
	// This should trigger "method not found" error
	client.handleIncomingRequest(ctx, samplingRequest)
	
	// Wait for error to be received
	select {
	case <-errorReceivedChan:
		// Error was received
	case <-time.After(1 * time.Second):
		t.Fatal("Method not found error was not received within timeout")
	}
	
	if !errorReceived {
		t.Error("Expected method not found error, but didn't receive it")
	}
}

// TestStreamableHTTP_BidirectionalInterface verifies the interface implementation
func TestStreamableHTTP_BidirectionalInterface(t *testing.T) {
	client, err := NewStreamableHTTP("http://example.com")
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	
	// Verify it implements BidirectionalInterface
	_, ok := any(client).(BidirectionalInterface)
	if !ok {
		t.Error("StreamableHTTP should implement BidirectionalInterface")
	}
	
	// Test SetRequestHandler
	handlerSet := false
	handlerSetChan := make(chan struct{})
	client.SetRequestHandler(func(ctx context.Context, request JSONRPCRequest) (*JSONRPCResponse, error) {
		handlerSet = true
		close(handlerSetChan)
		return nil, nil
	})
	
	// Verify handler was set by triggering it
	ctx := context.Background()
	client.handleIncomingRequest(ctx, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(1),
		Method:  "test",
	})
	
	// Wait for handler to be called
	select {
	case <-handlerSetChan:
		// Handler was called
	case <-time.After(1 * time.Second):
		t.Fatal("Handler was not called within timeout")
	}
	
	if !handlerSet {
		t.Error("Request handler was not properly set or called")
	}
}

// TestStreamableHTTP_ConcurrentSamplingRequests tests concurrent sampling requests
// where the second request completes faster than the first request
func TestStreamableHTTP_ConcurrentSamplingRequests(t *testing.T) {
	var receivedResponses []map[string]any
	var responseMutex sync.Mutex
	responseComplete := make(chan struct{}, 2)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Logf("Failed to decode body: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			// Check if this is a response from client (not a request)
			if _, ok := body["result"]; ok {
				responseMutex.Lock()
				receivedResponses = append(receivedResponses, body)
				responseMutex.Unlock()
				responseComplete <- struct{}{}
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewStreamableHTTP(server.URL)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Track which requests have been received and their completion order
	var requestOrder []int
	var orderMutex sync.Mutex
	
	// Set up request handler that simulates different processing times
	client.SetRequestHandler(func(ctx context.Context, request JSONRPCRequest) (*JSONRPCResponse, error) {
		// Extract request ID to determine processing time
		requestIDValue := request.ID.Value()
		
		var delay time.Duration
		var responseText string
		var requestNum int
		
		// First request (ID 1) takes longer, second request (ID 2) completes faster
		if requestIDValue == int64(1) {
			delay = 100 * time.Millisecond
			responseText = "Response from slow request 1"
			requestNum = 1
		} else if requestIDValue == int64(2) {
			delay = 10 * time.Millisecond
			responseText = "Response from fast request 2"
			requestNum = 2
		} else {
			t.Errorf("Unexpected request ID: %v", requestIDValue)
			return nil, fmt.Errorf("unexpected request ID")
		}

		// Simulate processing time
		time.Sleep(delay)
		
		// Record completion order
		orderMutex.Lock()
		requestOrder = append(requestOrder, requestNum)
		orderMutex.Unlock()

		// Return response with correct request ID
		result := map[string]any{
			"role": "assistant",
			"content": map[string]any{
				"type": "text",
				"text": responseText,
			},
			"model":      "test-model",
			"stopReason": "stop_sequence",
		}

		resultBytes, _ := json.Marshal(result)

		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Result:  resultBytes,
		}, nil
	})

	// Start the client
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = client.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}

	// Create two sampling requests with different IDs
	request1 := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(int64(1)),
		Method:  string(mcp.MethodSamplingCreateMessage),
		Params: map[string]any{
			"messages": []map[string]any{
				{
					"role": "user",
					"content": map[string]any{
						"type": "text",
						"text": "Slow request 1",
					},
				},
			},
		},
	}

	request2 := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(int64(2)),
		Method:  string(mcp.MethodSamplingCreateMessage),
		Params: map[string]any{
			"messages": []map[string]any{
				{
					"role": "user",
					"content": map[string]any{
						"type": "text",
						"text": "Fast request 2",
					},
				},
			},
		},
	}

	// Send both requests concurrently
	go client.handleIncomingRequest(ctx, request1)
	go client.handleIncomingRequest(ctx, request2)

	// Wait for both responses to complete
	for i := 0; i < 2; i++ {
		select {
		case <-responseComplete:
			// Response received
		case <-time.After(2 * time.Second):
			t.Fatal("Timeout waiting for response")
		}
	}

	// Verify completion order: request 2 should complete first
	orderMutex.Lock()
	defer orderMutex.Unlock()
	
	if len(requestOrder) != 2 {
		t.Fatalf("Expected 2 completed requests, got %d", len(requestOrder))
	}

	if requestOrder[0] != 2 {
		t.Errorf("Expected request 2 to complete first, but request %d completed first", requestOrder[0])
	}

	if requestOrder[1] != 1 {
		t.Errorf("Expected request 1 to complete second, but request %d completed second", requestOrder[1])
	}

	// Verify responses are correctly associated
	responseMutex.Lock()
	defer responseMutex.Unlock()

	if len(receivedResponses) != 2 {
		t.Fatalf("Expected 2 responses, got %d", len(receivedResponses))
	}

	// Find responses by ID
	var response1, response2 map[string]any
	for _, resp := range receivedResponses {
		if id, ok := resp["id"]; ok {
			switch id {
			case int64(1), float64(1):
				response1 = resp
			case int64(2), float64(2):
				response2 = resp
			}
		}
	}

	if response1 == nil {
		t.Error("Response for request 1 not found")
	}
	if response2 == nil {
		t.Error("Response for request 2 not found")
	}

	// Verify each response contains the correct content
	if response1 != nil {
		if result, ok := response1["result"].(map[string]any); ok {
			if content, ok := result["content"].(map[string]any); ok {
				if text, ok := content["text"].(string); ok {
					if !strings.Contains(text, "slow request 1") {
						t.Errorf("Response 1 should contain 'slow request 1', got: %s", text)
					}
				}
			}
		}
	}

	if response2 != nil {
		if result, ok := response2["result"].(map[string]any); ok {
			if content, ok := result["content"].(map[string]any); ok {
				if text, ok := content["text"].(string); ok {
					if !strings.Contains(text, "fast request 2") {
						t.Errorf("Response 2 should contain 'fast request 2', got: %s", text)
					}
				}
			}
		}
	}
}