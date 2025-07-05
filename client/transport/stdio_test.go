package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func compileTestServer(outputPath string) error {
	cmd := exec.Command(
		"go",
		"build",
		"-buildmode=pie",
		"-o",
		outputPath,
		"../../testdata/mockstdio_server.go",
	)
	tmpCache, _ := os.MkdirTemp("", "gocache")
	cmd.Env = append(os.Environ(), "GOCACHE="+tmpCache)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("compilation failed: %v\nOutput: %s", err, output)
	}
	// Verify the binary was actually created
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		return fmt.Errorf("mock server binary not found at %s after compilation", outputPath)
	}
	return nil
}

func TestStdio(t *testing.T) {
	// Create a temporary file for the mock server
	tempFile, err := os.CreateTemp("", "mockstdio_server")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tempFile.Close()
	mockServerPath := tempFile.Name()

	// Add .exe suffix on Windows
	if runtime.GOOS == "windows" {
		os.Remove(mockServerPath) // Remove the empty file first
		mockServerPath += ".exe"
	}

	if compileErr := compileTestServer(mockServerPath); compileErr != nil {
		t.Fatalf("Failed to compile mock server: %v", compileErr)
	}
	defer os.Remove(mockServerPath)

	// Create a new Stdio transport
	stdio := NewStdio(mockServerPath, nil)

	// Start the transport
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	startErr := stdio.Start(ctx)
	if startErr != nil {
		t.Fatalf("Failed to start Stdio transport: %v", startErr)
	}
	defer stdio.Close()

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
		response, err := stdio.SendRequest(ctx, request)
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
					AdditionalFields: map[string]any{"test": "value"},
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
				// We received a notification from the mock server
				// The mock server sends a notification with method "debug/test" and the original request as params
				if nt.Method != "debug/test" {
					t.Errorf("Expected notification method 'debug/test', got '%s'", nt.Method)
					return
				}

				// The mock server sends the original notification request as params
				// We need to extract the original method from the nested structure
				paramsJson, _ := json.Marshal(nt.Params)
				var originalRequest struct {
					Method string         `json:"method"`
					Params map[string]any `json:"params"`
				}
				if err := json.Unmarshal(paramsJson, &originalRequest); err != nil {
					t.Errorf("Failed to unmarshal notification params: %v", err)
					return
				}

				if originalRequest.Method != "debug/echo_notification" {
					t.Errorf("Expected original method 'debug/echo_notification', got '%s'", originalRequest.Method)
					return
				}

				// Check if the original params contain our test data
				if testValue, ok := originalRequest.Params["test"]; !ok || testValue != "value" {
					t.Errorf("Expected test param 'value', got %v", originalRequest.Params["test"])
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
					ID:      mcp.NewRequestId(int64(100 + idx)),
					Method:  "debug/echo",
					Params: map[string]any{
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
			idValue, ok = result.ID.Value().(int64)
			if !ok {
				t.Errorf("Request %d: Expected ID to be int64, got %T", i, result.ID.Value())
			} else if idValue != int64(100+i) {
				t.Errorf("Request %d: Expected echoed ID %d, got %d", i, 100+i, idValue)
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
			ID:      mcp.NewRequestId(int64(100)),
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

	t.Run("SendRequestWithStringID", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		params := map[string]any{
			"string": "string id test",
			"array":  []any{4, 5, 6},
		}

		// Use a string ID instead of an integer
		request := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      mcp.NewRequestId("request-123"),
			Method:  "debug/echo",
			Params:  params,
		}

		response, err := stdio.SendRequest(ctx, request)
		if err != nil {
			t.Fatalf("SendRequest failed: %v", err)
		}

		var result struct {
			JSONRPC string         `json:"jsonrpc"`
			ID      mcp.RequestId  `json:"id"`
			Method  string         `json:"method"`
			Params  map[string]any `json:"params"`
		}

		if err := json.Unmarshal(response.Result, &result); err != nil {
			t.Fatalf("Failed to unmarshal result: %v", err)
		}

		if result.JSONRPC != "2.0" {
			t.Errorf("Expected JSONRPC value '2.0', got '%s'", result.JSONRPC)
		}

		// Verify the ID is a string and has the expected value
		idValue, ok := result.ID.Value().(string)
		if !ok {
			t.Errorf("Expected ID to be string, got %T", result.ID.Value())
		} else if idValue != "request-123" {
			t.Errorf("Expected ID 'request-123', got '%s'", idValue)
		}

		if result.Method != "debug/echo" {
			t.Errorf("Expected method 'debug/echo', got '%s'", result.Method)
		}

		if str, ok := result.Params["string"].(string); !ok || str != "string id test" {
			t.Errorf("Expected string 'string id test', got %v", result.Params["string"])
		}

		if arr, ok := result.Params["array"].([]any); !ok || len(arr) != 3 {
			t.Errorf("Expected array with 3 items, got %v", result.Params["array"])
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
		// Create a temporary file for the mock server
		tempFile, err := os.CreateTemp("", "mockstdio_server")
		if err != nil {
			t.Fatalf("Failed to create temp file: %v", err)
		}
		tempFile.Close()
		mockServerPath := tempFile.Name()

		// Add .exe suffix on Windows
		if runtime.GOOS == "windows" {
			os.Remove(mockServerPath) // Remove the empty file first
			mockServerPath += ".exe"
		}

		if compileErr := compileTestServer(mockServerPath); compileErr != nil {
			t.Fatalf("Failed to compile mock server: %v", compileErr)
		}
		defer os.Remove(mockServerPath)

		uninitiatedStdio := NewStdio(mockServerPath, nil)

		// Prepare a request
		request := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      mcp.NewRequestId(int64(99)),
			Method:  "ping",
		}

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_, reqErr := uninitiatedStdio.SendRequest(ctx, request)
		if reqErr == nil {
			t.Errorf("Expected SendRequest to panic before Start(), but it didn't")
		} else if reqErr.Error() != "stdio client not started" {
			t.Errorf("Expected error 'stdio client not started', got: %v", reqErr)
		}
	})

	t.Run("RequestAfterClose", func(t *testing.T) {
		// Create a temporary file for the mock server
		tempFile, err := os.CreateTemp("", "mockstdio_server")
		if err != nil {
			t.Fatalf("Failed to create temp file: %v", err)
		}
		tempFile.Close()
		mockServerPath := tempFile.Name()

		// Add .exe suffix on Windows
		if runtime.GOOS == "windows" {
			os.Remove(mockServerPath) // Remove the empty file first
			mockServerPath += ".exe"
		}

		if compileErr := compileTestServer(mockServerPath); compileErr != nil {
			t.Fatalf("Failed to compile mock server: %v", compileErr)
		}
		defer os.Remove(mockServerPath)

		// Create a new Stdio transport
		stdio := NewStdio(mockServerPath, nil)

		// Start the transport
		ctx := context.Background()
		if startErr := stdio.Start(ctx); startErr != nil {
			t.Fatalf("Failed to start Stdio transport: %v", startErr)
		}

		// Close the transport - ignore errors like "broken pipe" since the process might exit already
		stdio.Close()

		// Wait a bit to ensure process has exited
		time.Sleep(100 * time.Millisecond)

		// Try to send a request after close
		request := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      mcp.NewRequestId(int64(1)),
			Method:  "ping",
		}

		_, sendErr := stdio.SendRequest(ctx, request)
		if sendErr == nil {
			t.Errorf("Expected error when sending request after close, got nil")
		}
	})
}

func TestStdio_WithCommandFunc(t *testing.T) {
	called := false
	tmpDir := t.TempDir()
	chrootDir := filepath.Join(tmpDir, "sandbox-root")
	err := os.MkdirAll(chrootDir, 0o755)
	require.NoError(t, err, "failed to create chroot dir")

	fakeCmdFunc := func(ctx context.Context, command string, args []string, env []string) (*exec.Cmd, error) {
		called = true

		// Override the args inside our command func.
		cmd := exec.CommandContext(ctx, command, "bonjour")

		// Simulate some security-related settings for test purposes.
		cmd.Env = []string{"PATH=/usr/bin", "NODE_ENV=production"}
		cmd.Dir = tmpDir

		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{
				Uid: 1001,
				Gid: 1001,
			},
			Chroot: chrootDir,
		}

		return cmd, nil
	}

	stdio := NewStdioWithOptions(
		"echo",
		[]string{"foo=bar"},
		[]string{"hello"},
		WithCommandFunc(fakeCmdFunc),
	)
	require.NotNil(t, stdio)
	require.NotNil(t, stdio.cmdFunc)

	// Manually call the cmdFunc passing the same values as in spawnCommand.
	cmd, err := stdio.cmdFunc(context.Background(), "echo", nil, []string{"hello"})
	require.NoError(t, err)
	require.True(t, called)
	require.NotNil(t, cmd)
	require.NotNil(t, cmd.SysProcAttr)
	require.Equal(t, chrootDir, cmd.SysProcAttr.Chroot)
	require.Equal(t, tmpDir, cmd.Dir)
	require.Equal(t, uint32(1001), cmd.SysProcAttr.Credential.Uid)
	require.Equal(t, "echo", filepath.Base(cmd.Path))
	require.Len(t, cmd.Args, 2)
	require.Contains(t, cmd.Args, "bonjour")
	require.Len(t, cmd.Env, 2)
	require.Contains(t, cmd.Env, "PATH=/usr/bin")
	require.Contains(t, cmd.Env, "NODE_ENV=production")
}

func TestStdio_SpawnCommand(t *testing.T) {
	ctx := context.Background()
	t.Setenv("TEST_ENVIRON_VAR", "true")

	// Explicitly not passing any environment, so we can see if it
	// is picked up by spawn command merging the os.Environ.
	stdio := NewStdio("echo", nil, "hello")
	require.NotNil(t, stdio)

	err := stdio.spawnCommand(ctx)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = stdio.cmd.Process.Kill()
	})

	require.Equal(t, "echo", filepath.Base(stdio.cmd.Path))
	require.Contains(t, stdio.cmd.Args, "hello")
	require.Contains(t, stdio.cmd.Env, "TEST_ENVIRON_VAR=true")
}

func TestStdio_SpawnCommand_UsesCommandFunc(t *testing.T) {
	ctx := context.Background()
	t.Setenv("TEST_ENVIRON_VAR", "true")

	stdio := NewStdioWithOptions(
		"echo",
		nil,
		[]string{"test"},
		WithCommandFunc(func(ctx context.Context, cmd string, args []string, env []string) (*exec.Cmd, error) {
			c := exec.CommandContext(ctx, cmd, "hola")
			c.Env = env
			return c, nil
		}),
	)
	require.NotNil(t, stdio)
	err := stdio.spawnCommand(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = stdio.cmd.Process.Kill()
	})

	require.Equal(t, "echo", filepath.Base(stdio.cmd.Path))
	require.Contains(t, stdio.cmd.Args, "hola")
	require.NotContains(t, stdio.cmd.Env, "TEST_ENVIRON_VAR=true")
	require.NotNil(t, stdio.stdin)
	require.NotNil(t, stdio.stdout)
	require.NotNil(t, stdio.stderr)
}

func TestStdio_SpawnCommand_UsesCommandFunc_Error(t *testing.T) {
	ctx := context.Background()

	stdio := NewStdioWithOptions(
		"echo",
		nil,
		[]string{"test"},
		WithCommandFunc(func(ctx context.Context, cmd string, args []string, env []string) (*exec.Cmd, error) {
			return nil, errors.New("test error")
		}),
	)
	require.NotNil(t, stdio)
	err := stdio.spawnCommand(ctx)
	require.Error(t, err)
	require.EqualError(t, err, "test error")
}

func TestStdio_NewStdioWithOptions_AppliesOptions(t *testing.T) {
	configured := false

	opt := func(s *Stdio) {
		configured = true
	}

	stdio := NewStdioWithOptions("echo", nil, []string{"test"}, opt)
	require.NotNil(t, stdio)
	require.True(t, configured, "option was not applied")
}
