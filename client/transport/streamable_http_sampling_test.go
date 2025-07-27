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
		result := map[string]interface{}{
			"role": "assistant",
			"content": map[string]interface{}{
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
		Params: map[string]interface{}{
			"messages": []map[string]interface{}{
				{
					"role": "user",
					"content": map[string]interface{}{
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
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Logf("Failed to decode body: %v", err)
				w.WriteHeader(http.StatusOK)
				return
			}
			
			// Check if this is an error response
			if errorField, ok := body["error"]; ok {
				errorMap := errorField.(map[string]interface{})
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
		Params:  map[string]interface{}{},
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
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Logf("Failed to decode body: %v", err)
				w.WriteHeader(http.StatusOK)
				return
			}
			
			// Check if this is an error response with method not found
			if errorField, ok := body["error"]; ok {
				errorMap := errorField.(map[string]interface{})
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
		Params:  map[string]interface{}{},
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
	_, ok := interface{}(client).(BidirectionalInterface)
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