package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func compileTestServer(outputPath string) error {
	cmd := exec.Command(
		"go",
		"build",
		"-o",
		outputPath,
		"../../testdata/mockstdio_server.go",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("compilation failed: %v\nOutput: %s", err, output)
	}
	return nil
}

func TestStdio(t *testing.T) {
	// Compile mock server
	mockServerPath := filepath.Join(os.TempDir(), "mockstdio_server")
	// Add .exe suffix on Windows
	if runtime.GOOS == "windows" {
		mockServerPath += ".exe"
	}
	if err := compileTestServer(mockServerPath); err != nil {
		t.Fatalf("Failed to compile mock server: %v", err)
	}
	defer os.Remove(mockServerPath)

	// Create a new Stdio transport
	stdio := NewStdio(mockServerPath, nil)

	// Start the transport
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := stdio.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start Stdio transport: %v", err)
	}
	defer stdio.Close()

	t.Run("SendRequest", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5000000000*time.Second)
		defer cancel()

		params := map[string]interface{}{
			"string": "hello world",
			"array":  []interface{}{1, 2, 3},
		}

		request := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "debug/echo",
			Params:  params,
		}

		// Send the request
		response, err := stdio.SendRequest(ctx, request)
		if err != nil {
			t.Fatalf("SendRequest failed: %v", err)
		}

		// Parse the result to verify echo
		var result struct {
			JSONRPC string                 `json:"jsonrpc"`
			ID      int64                  `json:"id"`
			Method  string                 `json:"method"`
			Params  map[string]interface{} `json:"params"`
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

		if arr, ok := result.Params["array"].([]interface{}); !ok || len(arr) != 3 {
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
		_, err := stdio.SendRequest(ctx, request)
		if err == nil {
			t.Errorf("Expected context canceled error, got nil")
		} else if err != context.Canceled {
			t.Errorf("Expected context.Canceled error, got: %v", err)
		}
	})

	t.Run("SendNotification & NotificationHandler", func(t *testing.T) {

		var wg sync.WaitGroup
		notificationChan := make(chan mcp.JSONRPCNotification, 1)

		// Set notification handler
		stdio.SetNotificationHandler(func(notification mcp.JSONRPCNotification) {
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
					AdditionalFields: map[string]interface{}{"test": "value"},
				},
			},
		}
		err := stdio.SendNotification(ctx, notification)
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
		responses := make([]*JSONRPCResponse, numRequests)
		errors := make([]error, numRequests)
		mu := sync.Mutex{}
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
					Params: map[string]interface{}{
						"requestIndex": idx,
						"timestamp":    time.Now().UnixNano(),
					},
				}

				resp, err := stdio.SendRequest(ctx, request)
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
				JSONRPC string                 `json:"jsonrpc"`
				ID      int64                  `json:"id"`
				Method  string                 `json:"method"`
				Params  map[string]interface{} `json:"params"`
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
		reps, err := stdio.SendRequest(ctx, request)
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

func TestStdioErrors(t *testing.T) {
	t.Run("InvalidCommand", func(t *testing.T) {
		// Create a new Stdio transport with a non-existent command
		stdio := NewStdio("non_existent_command", nil)

		// Start should fail
		ctx := context.Background()
		err := stdio.Start(ctx)
		if err == nil {
			t.Errorf("Expected error when starting with invalid command, got nil")
			stdio.Close()
		}
	})

	t.Run("RequestBeforeStart", func(t *testing.T) {
		mockServerPath := filepath.Join(os.TempDir(), "mockstdio_server")
		// Add .exe suffix on Windows
		if runtime.GOOS == "windows" {
			mockServerPath += ".exe"
		}
		if err := compileTestServer(mockServerPath); err != nil {
			t.Fatalf("Failed to compile mock server: %v", err)
		}
		defer os.Remove(mockServerPath)

		uninitiatedStdio := NewStdio(mockServerPath, nil)

		// Prepare a request
		request := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      99,
			Method:  "ping",
		}

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_, err := uninitiatedStdio.SendRequest(ctx, request)
		if err == nil {
			t.Errorf("Expected SendRequest to panic before Start(), but it didn't")
		} else if err.Error() != "stdio client not started" {
			t.Errorf("Expected error 'stdio client not started', got: %v", err)
		}
	})

	t.Run("RequestAfterClose", func(t *testing.T) {
		// Compile mock server
		mockServerPath := filepath.Join(os.TempDir(), "mockstdio_server")
		// Add .exe suffix on Windows
		if runtime.GOOS == "windows" {
			mockServerPath += ".exe"
		}
		if err := compileTestServer(mockServerPath); err != nil {
			t.Fatalf("Failed to compile mock server: %v", err)
		}
		defer os.Remove(mockServerPath)

		// Create a new Stdio transport
		stdio := NewStdio(mockServerPath, nil)

		// Start the transport
		ctx := context.Background()
		if err := stdio.Start(ctx); err != nil {
			t.Fatalf("Failed to start Stdio transport: %v", err)
		}

		// Close the transport - ignore errors like "broken pipe" since the process might exit already
		stdio.Close()

		// Wait a bit to ensure process has exited
		time.Sleep(100 * time.Millisecond)

		// Try to send a request after close
		request := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "ping",
		}

		_, err := stdio.SendRequest(ctx, request)
		if err == nil {
			t.Errorf("Expected error when sending request after close, got nil")
		}
	})

}
