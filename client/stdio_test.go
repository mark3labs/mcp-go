package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

func compileTestServer(outputPath string) error {
	cmd := exec.Command(
		"go",
		"build",
		"-buildmode=pie",
		"-o",
		outputPath,
		"../testdata/mockstdio_server.go",
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

func TestStdioMCPClient(t *testing.T) {
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

	client, err := NewStdioMCPClient(mockServerPath, []string{})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	var logRecords []map[string]any
	var logRecordsMu sync.RWMutex
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		stderr, ok := GetStderr(client)
		if !ok {
			return
		}

		dec := json.NewDecoder(stderr)
		for {
			var record map[string]any
			if err := dec.Decode(&record); err != nil {
				return
			}
			logRecordsMu.Lock()
			logRecords = append(logRecords, record)
			logRecordsMu.Unlock()
		}
	}()

	t.Run("Initialize", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		request := mcp.InitializeRequest{}
		request.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		request.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}
		request.Params.Capabilities = mcp.ClientCapabilities{
			Roots: &struct {
				ListChanged bool `json:"listChanged,omitempty"`
			}{
				ListChanged: true,
			},
		}

		result, err := client.Initialize(ctx, request)
		if err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}

		if result.ServerInfo.Name != "mock-server" {
			t.Errorf(
				"Expected server name 'mock-server', got '%s'",
				result.ServerInfo.Name,
			)
		}
	})

	t.Run("Ping", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := client.Ping(ctx)
		if err != nil {
			t.Errorf("Ping failed: %v", err)
		}
	})

	t.Run("ListResources", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		request := mcp.ListResourcesRequest{}
		result, err := client.ListResources(ctx, request)
		if err != nil {
			t.Errorf("ListResources failed: %v", err)
		}

		if len(result.Resources) != 1 {
			t.Errorf("Expected 1 resource, got %d", len(result.Resources))
		}
	})

	t.Run("ReadResource", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		request := mcp.ReadResourceRequest{}
		request.Params.URI = "test://resource"

		result, err := client.ReadResource(ctx, request)
		if err != nil {
			t.Errorf("ReadResource failed: %v", err)
		}

		if len(result.Contents) != 1 {
			t.Errorf("Expected 1 content item, got %d", len(result.Contents))
		}
	})

	t.Run("Subscribe and Unsubscribe", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Test Subscribe
		subRequest := mcp.SubscribeRequest{}
		subRequest.Params.URI = "test://resource"
		err := client.Subscribe(ctx, subRequest)
		if err != nil {
			t.Errorf("Subscribe failed: %v", err)
		}

		// Test Unsubscribe
		unsubRequest := mcp.UnsubscribeRequest{}
		unsubRequest.Params.URI = "test://resource"
		err = client.Unsubscribe(ctx, unsubRequest)
		if err != nil {
			t.Errorf("Unsubscribe failed: %v", err)
		}
	})

	t.Run("ListPrompts", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		request := mcp.ListPromptsRequest{}
		result, err := client.ListPrompts(ctx, request)
		if err != nil {
			t.Errorf("ListPrompts failed: %v", err)
		}

		if len(result.Prompts) != 1 {
			t.Errorf("Expected 1 prompt, got %d", len(result.Prompts))
		}
	})

	t.Run("GetPrompt", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		request := mcp.GetPromptRequest{}
		request.Params.Name = "test-prompt"

		result, err := client.GetPrompt(ctx, request)
		if err != nil {
			t.Errorf("GetPrompt failed: %v", err)
		}

		if len(result.Messages) != 1 {
			t.Errorf("Expected 1 message, got %d", len(result.Messages))
		}
	})

	t.Run("ListTools", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		request := mcp.ListToolsRequest{}
		result, err := client.ListTools(ctx, request)
		if err != nil {
			t.Errorf("ListTools failed: %v", err)
		}

		if len(result.Tools) != 1 {
			t.Errorf("Expected 1 tool, got %d", len(result.Tools))
		}
	})

	t.Run("CallTool", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		request := mcp.CallToolRequest{}
		request.Params.Name = "test-tool"
		request.Params.Arguments = map[string]any{
			"param1": "value1",
		}

		result, err := client.CallTool(ctx, request)
		if err != nil {
			t.Errorf("CallTool failed: %v", err)
		}

		if len(result.Content) != 1 {
			t.Errorf("Expected 1 content item, got %d", len(result.Content))
		}
	})

	t.Run("SetLevel", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		request := mcp.SetLevelRequest{}
		request.Params.Level = mcp.LoggingLevelInfo

		err := client.SetLevel(ctx, request)
		if err != nil {
			t.Errorf("SetLevel failed: %v", err)
		}
	})

	t.Run("Complete", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		request := mcp.CompleteRequest{}
		request.Params.Ref = mcp.PromptReference{
			Type: "ref/prompt",
			Name: "test-prompt",
		}
		request.Params.Argument.Name = "test-arg"
		request.Params.Argument.Value = "test-value"

		result, err := client.Complete(ctx, request)
		if err != nil {
			t.Errorf("Complete failed: %v", err)
		}

		if len(result.Completion.Values) != 1 {
			t.Errorf(
				"Expected 1 completion value, got %d",
				len(result.Completion.Values),
			)
		}
	})

	client.Close()
	wg.Wait()

	t.Run("CheckLogs", func(t *testing.T) {
		logRecordsMu.RLock()
		defer logRecordsMu.RUnlock()

		if len(logRecords) != 1 {
			t.Errorf("Expected 1 log record, got %d", len(logRecords))
			return
		}

		msg, ok := logRecords[0][slog.MessageKey].(string)
		if !ok {
			t.Errorf("Expected log record to have message key")
		}
		if msg != "launch successful" {
			t.Errorf("Expected log message 'launch successful', got '%s'", msg)
		}
	})
}

func TestStdio_SendRequestReturnsWhenTransportCloses(t *testing.T) {
	// This test verifies that SendRequest unblocks automatically when the
	// server process dies (EOF on stdout). Before the fix, SendRequest only
	// selected on ctx.Done() and responseChan — it would hang forever because
	// readResponses exited silently without signaling the done channel.
	//
	// The fix has two parts:
	//   1. readResponses calls closeDone() on unexpected exit (EOF/error),
	//      so in-flight requests unblock without external intervention.
	//   2. SendRequest's select includes <-c.done so it returns immediately
	//      when the done channel is closed.
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()
	defer serverRead.Close()

	stdioTransport := transport.NewIO(clientRead, clientWrite, io.NopCloser(strings.NewReader("")))
	require.NoError(t, stdioTransport.Start(context.Background()))

	c := NewClient(stdioTransport)
	defer c.Close()

	// Drain serverRead so the write doesn't block
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := serverRead.Read(buf); err != nil {
				return
			}
		}
	}()

	// SendRequest in a goroutine — it will write the request (succeeds)
	// then block waiting for a response that will never come.
	errChan := make(chan error, 1)
	go func() {
		req := mcp.InitializeRequest{}
		req.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		req.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "1.0"}
		_, err := c.Initialize(context.Background(), req)
		errChan <- err
	}()

	// Give SendRequest time to send the request and enter the response-wait select
	time.Sleep(50 * time.Millisecond)

	// Simulate server death: close the server's write end → EOF on client read.
	// readResponses should detect EOF, call closeDone(), and SendRequest should
	// unblock automatically — no explicit Close() needed.
	serverWrite.Close()

	select {
	case err := <-errChan:
		require.ErrorIs(t, err, transport.ErrTransportClosed)
	case <-time.After(5 * time.Second):
		t.Fatal("SendRequest hung for 5s — deadlock not fixed")
	}
}

func TestStdio_SendRequestReturnsImmediatelyWhenAlreadyClosed(t *testing.T) {
	// Verify the pre-check in SendRequest catches an already-closed transport
	// before doing any work (no write attempted).
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()
	defer serverRead.Close()
	defer serverWrite.Close()

	stdioTransport := transport.NewIO(clientRead, clientWrite, io.NopCloser(strings.NewReader("")))
	require.NoError(t, stdioTransport.Start(context.Background()))

	c := NewClient(stdioTransport)
	c.Close()

	req := mcp.InitializeRequest{}
	req.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	req.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "1.0"}
	_, err := c.Initialize(context.Background(), req)
	require.ErrorIs(t, err, transport.ErrTransportClosed)
}

func TestStdio_SendNotificationReturnsWhenTransportClosed(t *testing.T) {
	// Verify SendNotification returns ErrTransportClosed after Close().
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()
	defer serverRead.Close()
	defer serverWrite.Close()

	stdioTransport := transport.NewIO(clientRead, clientWrite, io.NopCloser(strings.NewReader("")))
	require.NoError(t, stdioTransport.Start(context.Background()))

	c := NewClient(stdioTransport)
	c.Close()

	notification := mcp.JSONRPCNotification{
		JSONRPC: "2.0",
	}
	notification.Method = "notifications/initialized"

	err := stdioTransport.SendNotification(context.Background(), notification)
	require.ErrorIs(t, err, transport.ErrTransportClosed)
}

func TestStdio_SendNotificationReturnsWhenContextCancelled(t *testing.T) {
	// Verify SendNotification returns ctx.Err() when context is already cancelled.
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()
	defer serverRead.Close()
	defer serverWrite.Close()

	stdioTransport := transport.NewIO(clientRead, clientWrite, io.NopCloser(strings.NewReader("")))
	require.NoError(t, stdioTransport.Start(context.Background()))

	defer stdioTransport.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	notification := mcp.JSONRPCNotification{
		JSONRPC: "2.0",
	}
	notification.Method = "notifications/initialized"

	err := stdioTransport.SendNotification(ctx, notification)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

func TestStdio_ConcurrentCloseDoesNotPanic(t *testing.T) {
	// Verify that calling Close() concurrently from multiple goroutines
	// does not panic (sync.Once protects the done channel close).
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()
	defer serverRead.Close()
	defer serverWrite.Close()

	stdioTransport := transport.NewIO(clientRead, clientWrite, io.NopCloser(strings.NewReader("")))
	require.NoError(t, stdioTransport.Start(context.Background()))

	c := NewClient(stdioTransport)

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Close()
		}()
	}
	wg.Wait()
	// If we get here without a panic, sync.Once is working correctly.
}

func TestStdio_CloseCleanupRunsAfterReadResponsesCloseDone(t *testing.T) {
	// Verify that Close() still performs resource cleanup (stdin, stderr)
	// even when readResponses has already called closeDone() on EOF.
	// Before the fix, Close() had an early-return guard on <-c.done that
	// would skip cleanup entirely, leaking FDs and creating zombie processes.
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()

	stdioTransport := transport.NewIO(clientRead, clientWrite, io.NopCloser(strings.NewReader("")))
	require.NoError(t, stdioTransport.Start(context.Background()))

	// Drain writes so nothing blocks
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := serverRead.Read(buf); err != nil {
				return
			}
		}
	}()

	// Simulate server death — readResponses will call closeDone()
	serverWrite.Close()
	time.Sleep(50 * time.Millisecond)

	// Close() should still run cleanup without error, even though done is
	// already closed. If the old early-return guard were still present,
	// Close() would return nil immediately and skip stdin cleanup.
	err := stdioTransport.Close()
	// stdin (clientWrite) should now be closed by cleanup
	require.NoError(t, err)

	// Verify stdin is actually closed by trying to write to it
	_, writeErr := clientWrite.Write([]byte("test\n"))
	require.Error(t, writeErr, "stdin should be closed after Close()")
}

func TestStdio_ConcurrentRequestsAllReceiveResponses(t *testing.T) {
	// Stress test: fire N concurrent requests through the transport and verify
	// every request receives its matching response. This tests for race conditions
	// in the response routing (map access, channel delivery) and concurrent
	// stdin writes. If any response is dropped or misrouted, the test fails.
	//
	// This is a regression test for issue #65 (MCP server hang after rapid tool
	// calls) — the theory is that concurrent requests can trigger a race in the
	// transport that causes a response to be lost.
	const numRequests = 50

	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()

	stdioTransport := transport.NewIO(clientRead, clientWrite, io.NopCloser(strings.NewReader("")))
	require.NoError(t, stdioTransport.Start(context.Background()))
	defer stdioTransport.Close()

	// Mock server: read JSON-RPC requests, respond with matching IDs after
	// a small random delay to simulate real-world server behavior.
	go func() {
		defer serverWrite.Close()
		scanner := json.NewDecoder(serverRead)
		for {
			var req map[string]any
			if err := scanner.Decode(&req); err != nil {
				return
			}

			id := req["id"]
			method, _ := req["method"].(string)

			// Only respond to non-notification messages (those with an id)
			if id == nil {
				continue
			}

			// Simulate varying server response times (0-5ms)
			if idNum, ok := id.(float64); ok {
				time.Sleep(time.Duration(int(idNum)%5) * time.Millisecond)
			}

			var resp map[string]any
			if method == "initialize" {
				resp = map[string]any{
					"jsonrpc": "2.0",
					"id":      id,
					"result": map[string]any{
						"protocolVersion": "2024-11-05",
						"capabilities":    map[string]any{},
						"serverInfo": map[string]any{
							"name":    "stress-test-server",
							"version": "1.0.0",
						},
					},
				}
			} else {
				resp = map[string]any{
					"jsonrpc": "2.0",
					"id":      id,
					"result": map[string]any{
						"tools": []any{},
					},
				}
			}

			respBytes, _ := json.Marshal(resp)
			respBytes = append(respBytes, '\n')
			if _, err := serverWrite.Write(respBytes); err != nil {
				return
			}
		}
	}()

	c := NewClient(stdioTransport)
	defer c.Close()

	// Initialize first (required before other requests)
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "stress-test", Version: "1.0"}
	_, err := c.Initialize(context.Background(), initReq)
	require.NoError(t, err)

	// Fire N concurrent ListTools requests
	var wg sync.WaitGroup
	errCh := make(chan error, numRequests)

	for i := range numRequests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := c.ListTools(ctx, mcp.ListToolsRequest{})
			if err != nil {
				errCh <- fmt.Errorf("request %d failed: %w", i, err)
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	require.Empty(t, errs, "Some concurrent requests failed: %v", errs)
}

func TestStdio_ConcurrentRequestsUnblockOnServerDeath(t *testing.T) {
	// Stress test: fire N concurrent requests, then kill the server mid-flight.
	// Every in-flight request must unblock with an error (not hang). This tests
	// that readResponses calling closeDone() on EOF correctly unblocks all
	// pending SendRequest calls, not just one.
	const numRequests = 20

	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()

	stdioTransport := transport.NewIO(clientRead, clientWrite, io.NopCloser(strings.NewReader("")))
	require.NoError(t, stdioTransport.Start(context.Background()))
	defer stdioTransport.Close()

	// Mock server: read requests but only respond to "initialize", then
	// stop responding (simulating a slow/hanging server).
	go func() {
		scanner := json.NewDecoder(serverRead)
		for {
			var req map[string]any
			if err := scanner.Decode(&req); err != nil {
				return
			}

			id := req["id"]
			method, _ := req["method"].(string)
			if id == nil {
				continue
			}

			if method == "initialize" {
				resp := map[string]any{
					"jsonrpc": "2.0",
					"id":      id,
					"result": map[string]any{
						"protocolVersion": "2024-11-05",
						"capabilities":    map[string]any{},
						"serverInfo": map[string]any{
							"name":    "death-test-server",
							"version": "1.0.0",
						},
					},
				}
				respBytes, _ := json.Marshal(resp)
				respBytes = append(respBytes, '\n')
				_, _ = serverWrite.Write(respBytes)
			}
			// Non-initialize requests: no response (simulates hang)
		}
	}()

	c := NewClient(stdioTransport)
	defer c.Close()

	// Initialize first
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "death-test", Version: "1.0"}
	_, err := c.Initialize(context.Background(), initReq)
	require.NoError(t, err)

	// Fire N concurrent requests that will never get a response
	var wg sync.WaitGroup
	errCh := make(chan error, numRequests)

	for i := range numRequests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := c.ListTools(ctx, mcp.ListToolsRequest{})
			errCh <- err
		}(i)
	}

	// Let all requests enter the response-wait select
	time.Sleep(50 * time.Millisecond)

	// Kill the server
	serverWrite.Close()

	// All requests must unblock within the timeout
	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.Error(t, err, "Expected error from server death, got nil")
	}
}

func TestStdio_NewStdioMCPClientWithOptions_CreatesAndStartsClient(t *testing.T) {
	called := false

	fakeCmdFunc := func(ctx context.Context, command string, args []string, env []string) (*exec.Cmd, error) {
		called = true
		return exec.CommandContext(ctx, "echo", "started"), nil
	}

	client, err := NewStdioMCPClientWithOptions(
		"echo",
		[]string{"FOO=bar"},
		[]string{"hello"},
		transport.WithCommandFunc(fakeCmdFunc),
	)
	require.NoError(t, err)
	require.NotNil(t, client)
	t.Cleanup(func() {
		_ = client.Close()
	})
	require.True(t, called)
}

func TestStdio_NewStdioMCPClientWithOptions_FailsToStart(t *testing.T) {
	// Create a commandFunc that points to a nonexistent binary
	badCmdFunc := func(ctx context.Context, command string, args []string, env []string) (*exec.Cmd, error) {
		return exec.CommandContext(ctx, "/nonexistent/bar", args...), nil
	}

	client, err := NewStdioMCPClientWithOptions(
		"foo",
		nil,
		nil,
		transport.WithCommandFunc(badCmdFunc),
	)

	require.Error(t, err)
	require.EqualError(t, err, "failed to start stdio transport: failed to start command: fork/exec /nonexistent/bar: no such file or directory")
	require.Nil(t, client)
}
