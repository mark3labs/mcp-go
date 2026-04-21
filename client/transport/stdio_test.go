package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
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
	tempFile, err := os.CreateTemp(t.TempDir(), "mockstdio_server")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tempFile.Close()
	mockServerPath := tempFile.Name() + ".exe"

	if compileErr := compileTestServer(mockServerPath); compileErr != nil {
		t.Fatalf("Failed to compile mock server: %v", compileErr)
	}

	// Create a new Stdio transport
	stdio := NewStdio(mockServerPath, nil)

	// Start the transport
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	startErr := stdio.Start(ctx)
	if startErr != nil {
		t.Fatalf("Failed to start Stdio transport: %v", startErr)
	}
	defer stdio.Close()

	t.Run("SendRequest", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
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
		ctx, cancel := context.WithCancel(t.Context())
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
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
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

		wg.Go(func() {
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
		})

		wg.Wait()
	})

	t.Run("MultipleRequests", func(t *testing.T) {
		var wg sync.WaitGroup
		const numRequests = 5

		// Send multiple requests concurrently
		responses := make([]*JSONRPCResponse, numRequests)
		errors := make([]error, numRequests)
		mu := sync.Mutex{}
		for i := range numRequests {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
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
		for i := range numRequests {
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
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
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
		ctx := t.Context()
		err := stdio.Start(ctx)
		if err == nil {
			t.Errorf("Expected error when starting with invalid command, got nil")
			stdio.Close()
		}
	})

	t.Run("RequestBeforeStart", func(t *testing.T) {
		// Create a temporary file for the mock server
		tempFile, err := os.CreateTemp(t.TempDir(), "mockstdio_server")
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

		uninitiatedStdio := NewStdio(mockServerPath, nil)

		// Prepare a request
		request := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      mcp.NewRequestId(int64(99)),
			Method:  "ping",
		}

		ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
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
		tempFile, err := os.CreateTemp(t.TempDir(), "mockstdio_server")
		if err != nil {
			t.Fatalf("Failed to create temp file: %v", err)
		}
		tempFile.Close()
		mockServerPath := tempFile.Name() + ".exe"

		if compileErr := compileTestServer(mockServerPath); compileErr != nil {
			t.Fatalf("Failed to compile mock server: %v", compileErr)
		}

		// Create a new Stdio transport
		stdio := NewStdio(mockServerPath, nil)

		// Start the transport
		ctx := t.Context()
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

	t.Run("StdioResponseWritingErrorLogging", func(t *testing.T) {
		logChan := make(chan string, 10)
		testLogger := &testLogger{logChan: logChan}

		_, stdinWriter := io.Pipe()
		stdoutReader, stdoutWriter := io.Pipe()
		stderrReader, stderrWriter := io.Pipe()
		t.Cleanup(func() {
			_ = stdinWriter.Close()
			_ = stdoutWriter.Close()
			_ = stderrWriter.Close()
		})

		stdio := NewIO(stdoutReader, stdinWriter, stderrReader)
		stdio.logger = testLogger

		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		t.Cleanup(cancel)

		err := stdio.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start stdio transport: %v", err)
		}
		t.Cleanup(func() { _ = stdio.Close() })

		stdio.SetRequestHandler(func(ctx context.Context, request JSONRPCRequest) (*JSONRPCResponse, error) {
			return NewJSONRPCResultResponse(request.ID, json.RawMessage(`"test response"`)), nil
		})

		doneChan := make(chan struct{})
		go func() {
			// Simulate a request coming from the server
			request := JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      mcp.NewRequestId(int64(1)),
				Method:  "test/method",
			}
			requestBytes, _ := json.Marshal(request)
			requestBytes = append(requestBytes, '\n')
			_, _ = stdoutWriter.Write(requestBytes)

			// Close stdin to trigger a write error when the response is sent
			time.Sleep(50 * time.Millisecond) // Give time for the request to be processed
			_ = stdinWriter.Close()
			doneChan <- struct{}{}
		}()

		<-doneChan

		// Wait for the error log message
		select {
		case logMsg := <-logChan:
			if !strings.Contains(logMsg, "Error writing response") {
				t.Errorf("Expected error log about writing response, got: %s", logMsg)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("Timeout waiting for error log message")
		}
	})
}

func TestStdio_StartGuaranteesReaderReady(t *testing.T) {
	stdoutReader, stdoutWriter := io.Pipe()
	stdinReader, stdinWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()
	t.Cleanup(func() {
		_ = stdoutWriter.Close()
		_ = stdinWriter.Close()
		_ = stderrWriter.Close()
	})

	stdio := NewIO(stdoutReader, stdinWriter, stderrReader)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)

	require.NoError(t, stdio.Start(ctx))
	t.Cleanup(func() { _ = stdio.Close() })

	// Mock server: echo every request back as a response immediately.
	// No sleep — responses arrive as fast as possible to stress the
	// window between Start() returning and the reader entering its loop.
	go func() {
		dec := json.NewDecoder(stdinReader)
		for {
			var req map[string]any
			if err := dec.Decode(&req); err != nil {
				return
			}
			id := req["id"]
			if id == nil {
				continue
			}
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result":  map[string]any{},
			}
			b, _ := json.Marshal(resp)
			b = append(b, '\n')
			if _, err := stdoutWriter.Write(b); err != nil {
				return
			}
		}
	}()

	// Send requests immediately after Start(), no sleep.
	// If the reader is not yet in its loop when the response arrives,
	// the response will be written to stdout before ReadString is called,
	// and will be lost — causing the request to time out.
	const N = 20
	for i := range N {
		reqCtx, reqCancel := context.WithTimeout(t.Context(), 2*time.Second)
		req := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      mcp.NewRequestId(int64(i)),
			Method:  "ping",
		}
		_, err := stdio.SendRequest(reqCtx, req)
		reqCancel()
		require.NoError(t, err, "request %d lost: reader was not ready when Start() returned", i)
	}
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
	cmd, err := stdio.cmdFunc(t.Context(), "echo", nil, []string{"hello"})
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
	ctx := t.Context()
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
	ctx := t.Context()
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
	ctx := t.Context()

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

func TestStdio_Close_ShutsDownHungChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal-based shutdown test is not supported on Windows")
	}

	deadline := gracefulShutdownTimeout + forceKillTimeout + 2*time.Second
	tests := []struct {
		name          string
		script        string
		wantErr       bool
		wantForceKill bool
	}{
		{
			name:          "exits_on_sigterm",
			script:        "trap 'exit 0' TERM; while :; do sleep 1; done",
			wantErr:       false,
			wantForceKill: false,
		},
		{
			name:          "requires_sigkill",
			script:        "trap '' TERM; while :; do sleep 1; done",
			wantErr:       true,
			wantForceKill: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdio := NewStdio("sh", nil, "-c", tt.script)
			require.NotNil(t, stdio)

			err := stdio.spawnCommand(t.Context())
			require.NoError(t, err)

			t.Cleanup(func() {
				if stdio.cmd != nil && stdio.cmd.Process != nil {
					_ = stdio.cmd.Process.Kill()
				}
			})

			closeErrCh := make(chan error, 1)
			start := time.Now()
			go func() {
				closeErrCh <- stdio.Close()
			}()

			var closeErr error
			select {
			case closeErr = <-closeErrCh:
			case <-time.After(deadline):
				t.Fatalf("Close() did not return within %s", deadline)
			}

			elapsed := time.Since(start)
			if tt.wantErr {
				require.Error(t, closeErr)
			} else {
				require.NoError(t, closeErr)
			}
			if tt.wantForceKill {
				require.NotErrorIs(t, closeErr, ErrChildShutdownTimeout)
			}
			if tt.wantForceKill {
				require.GreaterOrEqual(t, elapsed, gracefulShutdownTimeout+forceKillTimeout)
			} else {
				require.Less(t, elapsed, gracefulShutdownTimeout+forceKillTimeout)
			}
			require.Less(t, elapsed, deadline)
			require.NotNil(t, stdio.cmd.ProcessState)
			if !tt.wantForceKill {
				require.True(t, stdio.cmd.ProcessState.Exited())
			}
		})
	}
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

func TestStdio_LargeMessages(t *testing.T) {
	tempFile, err := os.CreateTemp(t.TempDir(), "mockstdio_server")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tempFile.Close()
	mockServerPath := tempFile.Name() + ".exe"

	if compileErr := compileTestServer(mockServerPath); compileErr != nil {
		t.Fatalf("Failed to compile mock server: %v", compileErr)
	}

	stdio := NewStdio(mockServerPath, nil)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	startErr := stdio.Start(ctx)
	if startErr != nil {
		t.Fatalf("Failed to start Stdio transport: %v", startErr)
	}
	defer stdio.Close()

	testCases := []struct {
		name        string
		dataSize    int
		description string
	}{
		{"SmallMessage_1KB", 1024, "Small message under scanner default limit"},
		{"MediumMessage_32KB", 32 * 1024, "Medium message under scanner default limit"},
		{"AtLimit_64KB", 64 * 1024, "Message at default scanner limit"},
		{"OverLimit_128KB", 128 * 1024, "Message over default scanner limit - would fail with Scanner"},
		{"Large_256KB", 256 * 1024, "Large message well over scanner limit"},
		{"VeryLarge_1MB", 1024 * 1024, "Very large message"},
		{"Huge_5MB", 5 * 1024 * 1024, "Huge message to stress test"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()

			largeString := generateRandomString(tc.dataSize)

			params := map[string]any{
				"data": largeString,
				"size": len(largeString),
			}

			request := JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      mcp.NewRequestId(int64(1)),
				Method:  "debug/echo",
				Params:  params,
			}

			response, err := stdio.SendRequest(ctx, request)
			if err != nil {
				t.Fatalf("SendRequest failed for %s: %v", tc.description, err)
			}

			var result struct {
				JSONRPC string         `json:"jsonrpc"`
				ID      mcp.RequestId  `json:"id"`
				Method  string         `json:"method"`
				Params  map[string]any `json:"params"`
			}

			if err := json.Unmarshal(response.Result, &result); err != nil {
				t.Fatalf("Failed to unmarshal result for %s: %v", tc.description, err)
			}

			if result.JSONRPC != "2.0" {
				t.Errorf("Expected JSONRPC value '2.0', got '%s'", result.JSONRPC)
			}

			returnedData, ok := result.Params["data"].(string)
			if !ok {
				t.Fatalf("Expected data to be string, got %T", result.Params["data"])
			}

			if returnedData != largeString {
				t.Errorf("Data mismatch for %s: expected length %d, got length %d",
					tc.description, len(largeString), len(returnedData))
			}

			returnedSize, ok := result.Params["size"].(float64)
			if !ok {
				t.Fatalf("Expected size to be number, got %T", result.Params["size"])
			}

			if int(returnedSize) != tc.dataSize {
				t.Errorf("Size mismatch for %s: expected %d, got %d",
					tc.description, tc.dataSize, int(returnedSize))
			}

			t.Logf("Successfully handled %s message of size %d bytes", tc.name, tc.dataSize)
		})
	}
}

// TestStdio_ConcurrentWritesDoNotInterleave verifies that concurrent calls to
// SendRequest and SendNotification do not interleave bytes on the subprocess's
// stdin. Without the stdinMu serialization, large JSON-RPC frames written from
// different goroutines can be fragmented by the Go runtime or the OS pipe
// buffer (POSIX guarantees atomicity only up to PIPE_BUF == 4096 bytes), which
// corrupts messages read line-by-line by the subprocess.
//
// Regression coverage for the client-side equivalent of #528 (PR #529 fixed
// the server-side only).
func TestStdio_ConcurrentWritesDoNotInterleave(t *testing.T) {
	tempFile, err := os.CreateTemp(t.TempDir(), "mockstdio_server")
	require.NoError(t, err)
	tempFile.Close()
	mockServerPath := tempFile.Name() + ".exe"
	require.NoError(t, compileTestServer(mockServerPath))

	stdio := NewStdio(mockServerPath, nil)
	require.NoError(t, stdio.Start(t.Context()))
	t.Cleanup(func() { _ = stdio.Close() })

	// Payload well above PIPE_BUF so any interleaving would corrupt the frame.
	const (
		numGoroutines   = 20
		requestsPerGo   = 5
		payloadByteSize = 16 * 1024
	)

	payload := generateRandomString(payloadByteSize)

	var wg sync.WaitGroup
	errCh := make(chan error, numGoroutines*requestsPerGo)
	for g := range numGoroutines {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for r := range requestsPerGo {
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				id := int64(10_000 + g*requestsPerGo + r)
				req := JSONRPCRequest{
					JSONRPC: "2.0",
					ID:      mcp.NewRequestId(id),
					Method:  "debug/echo",
					Params: map[string]any{
						"requestIndex": id,
						"payload":      payload,
					},
				}
				resp, err := stdio.SendRequest(ctx, req)
				cancel()
				if err != nil {
					errCh <- fmt.Errorf("goroutine %d req %d: %w", g, r, err)
					continue
				}
				if resp == nil {
					errCh <- fmt.Errorf("goroutine %d req %d: nil response", g, r)
					continue
				}
				gotID, ok := resp.ID.Value().(int64)
				if !ok || gotID != id {
					errCh <- fmt.Errorf("goroutine %d req %d: id mismatch got=%v want=%d", g, r, resp.ID.Value(), id)
				}
			}
		}(g)
	}
	wg.Wait()
	close(errCh)

	var firstErr error
	errCount := 0
	for e := range errCh {
		errCount++
		if firstErr == nil {
			firstErr = e
		}
	}
	if errCount > 0 {
		t.Fatalf("%d concurrent requests failed (first error: %v)", errCount, firstErr)
	}
}

func generateRandomString(size int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 "

	b := make([]byte, size)
	for i := range b {
		b[i] = charset[i%len(charset)]
	}
	return string(b)
}
