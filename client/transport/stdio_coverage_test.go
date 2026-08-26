package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// newBareStdio builds a Stdio transport whose process fields are unset, so
// individual code paths (request routing, error responses, close cleanup)
// can be exercised deterministically without spawning a subprocess.
func newBareStdio() *Stdio {
	return &Stdio{
		responses: make(map[string]chan *JSONRPCResponse),
		done:      make(chan struct{}),
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// errWriteCloser is a write sink that returns a configurable error from Write
// and Close, recording whether it was closed.
type errWriteCloser struct {
	mu     sync.Mutex
	closed bool
	err    error
}

// Write reports the full write length and returns the configured error.
func (w *errWriteCloser) Write(p []byte) (int, error) { return len(p), w.err }

// Close marks the writer closed and returns the configured error.
func (w *errWriteCloser) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return w.err
}

// IsClosed reports whether Close has been called.
func (w *errWriteCloser) IsClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

// failingReadCloser returns err on every Read and closeErr on Close.
type failingReadCloser struct{ err, closeErr error }

// Read always returns the configured error.
func (r failingReadCloser) Read([]byte) (int, error) { return 0, r.err }

// Close returns the configured close error.
func (r failingReadCloser) Close() error { return r.closeErr }

// TestStdio_GetSessionId verifies the session ID accessor returns an empty
// string for the stdio transport.
func TestStdio_GetSessionId(t *testing.T) {
	require.Equal(t, "", newBareStdio().GetSessionId())
}

// TestStdio_WithCommandLogger verifies WithCommandLogger keeps a non-nil
// logger and falls back to slog.Default for nil.
func TestStdio_WithCommandLogger(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Non-nil logger is kept.
	stdio := NewStdioWithOptions("cmd", nil, nil, WithCommandLogger(logger))
	require.Same(t, logger, stdio.logger)

	// Nil logger falls back to slog.Default.
	stdio = NewStdioWithOptions("cmd", nil, nil, WithCommandLogger(nil))
	require.Same(t, slog.Default(), stdio.logger)
}

// TestStdio_SendRequest_ClosedTransport verifies SendRequest fails with
// ErrTransportClosed after the transport is closed.
func TestStdio_SendRequest_ClosedTransport(t *testing.T) {
	stdio := newBareStdio()
	close(stdio.done)

	_, err := stdio.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)),
	})
	require.ErrorIs(t, err, ErrTransportClosed)
}

// TestStdio_SendRequest_ContextCanceled verifies SendRequest fails with
// context.Canceled when the context is already canceled.
func TestStdio_SendRequest_ContextCanceled(t *testing.T) {
	stdio := newBareStdio()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := stdio.SendRequest(ctx, JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)),
	})
	require.ErrorIs(t, err, context.Canceled)
}

// TestStdio_SendRequest_NotStarted verifies SendRequest fails with
// "stdio client not started" before Start has run.
func TestStdio_SendRequest_NotStarted(t *testing.T) {
	_, err := newBareStdio().SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)),
	})
	require.EqualError(t, err, "stdio client not started")
}

// TestStdio_SendRequest_WriteError verifies SendRequest wraps a stdin write
// failure.
func TestStdio_SendRequest_WriteError(t *testing.T) {
	stdio := newBareStdio()
	writeErr := errors.New("stdin broken")
	stdio.stdin = &errWriteCloser{err: writeErr}

	_, err := stdio.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)),
	})
	require.EqualError(t, err, "failed to write request: stdin broken")
}

// doneClosingWriter closes its done channel on the first Write, letting tests
// race transport shutdown with an in-flight request write.
type doneClosingWriter struct{ done chan struct{} }

// Write closes the done channel once and reports the full write length.
func (w *doneClosingWriter) Write(p []byte) (int, error) {
	close(w.done)
	return len(p), nil
}

// Close is a no-op.
func (w *doneClosingWriter) Close() error { return nil }

// TestStdio_SendRequest_DoneWithoutResponse verifies a request whose transport
// closes before delivery fails with ErrTransportClosed and cleans up its
// pending response channel.
func TestStdio_SendRequest_DoneWithoutResponse(t *testing.T) {
	stdio := newBareStdio()
	stdio.stdin = &doneClosingWriter{done: stdio.done}

	// The write succeeds but the transport closes before a response is
	// delivered; the request must fail with ErrTransportClosed and clean
	// up its pending response channel.
	_, err := stdio.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)),
	})
	require.ErrorIs(t, err, ErrTransportClosed)

	stdio.mu.RLock()
	defer stdio.mu.RUnlock()
	require.NotContains(t, stdio.responses, mcp.NewRequestId(int64(1)).String())
}

// injectingWriter closes the done channel on the first Write and delivers a
// response into the request's freshly registered channel, modeling a response
// that arrives right as the transport shuts down.
type injectingWriter struct {
	done  chan struct{}
	once  sync.Once
	stdio *Stdio
	idKey string
}

// Write closes the done channel once and delivers a canned response into the
// request's freshly registered response channel.
func (w *injectingWriter) Write(p []byte) (int, error) {
	w.once.Do(func() {
		close(w.done)
		w.stdio.mu.RLock()
		ch := w.stdio.responses[w.idKey]
		w.stdio.mu.RUnlock()
		if ch != nil {
			ch <- &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      mcp.NewRequestId(int64(1)),
				Result:  json.RawMessage(`"ok"`),
			}
		}
	})
	return len(p), nil
}

// Close is a no-op.
func (w *injectingWriter) Close() error { return nil }

// TestStdio_SendRequest_DoneWithBufferedResponse verifies a response delivered
// just before close wins over ErrTransportClosed.
func TestStdio_SendRequest_DoneWithBufferedResponse(t *testing.T) {
	stdio := newBareStdio()
	idKey := mcp.NewRequestId(int64(1)).String()
	stdio.stdin = &injectingWriter{done: stdio.done, stdio: stdio, idKey: idKey}

	// A response delivered just before close must win over ErrTransportClosed.
	resp, err := stdio.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, json.RawMessage(`"ok"`), resp.Result)
}

// TestStdio_SendRequest_ContextCanceledAfterWrite verifies a canceled context
// wins once the request has been written.
func TestStdio_SendRequest_ContextCanceledAfterWrite(t *testing.T) {
	stdio := newBareStdio()
	stdio.stdin = &errWriteCloser{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := stdio.SendRequest(ctx, JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)),
	})
	require.ErrorIs(t, err, context.Canceled)
}

// TestStdio_SendNotification_NotStarted verifies SendNotification fails with
// "stdio client not started" before Start has run.
func TestStdio_SendNotification_NotStarted(t *testing.T) {
	err := newBareStdio().SendNotification(t.Context(), mcp.JSONRPCNotification{
		JSONRPC: "2.0",
		Notification: mcp.Notification{
			Method: "test",
		},
	})
	require.EqualError(t, err, "stdio client not started")
}

// TestStdio_SendNotification_ClosedTransport verifies SendNotification fails
// with ErrTransportClosed after the transport is closed.
func TestStdio_SendNotification_ClosedTransport(t *testing.T) {
	stdio := newBareStdio()
	close(stdio.done)

	err := stdio.SendNotification(t.Context(), mcp.JSONRPCNotification{
		JSONRPC: "2.0",
		Notification: mcp.Notification{
			Method: "test",
		},
	})
	require.ErrorIs(t, err, ErrTransportClosed)
}

// TestStdio_SendNotification_WriteError verifies SendNotification wraps a
// stdin write failure.
func TestStdio_SendNotification_WriteError(t *testing.T) {
	stdio := newBareStdio()
	stdio.stdin = &errWriteCloser{err: errors.New("stdin broken")}

	err := stdio.SendNotification(t.Context(), mcp.JSONRPCNotification{
		JSONRPC: "2.0",
		Notification: mcp.Notification{
			Method: "test",
		},
	})
	require.EqualError(t, err, "failed to write notification: stdin broken")
}

// TestStdio_SendNotification_Success verifies a notification is written to
// stdin without error.
func TestStdio_SendNotification_Success(t *testing.T) {
	stdio := newBareStdio()
	sink := &concurrentBuffer{}
	stdio.stdin = sink

	err := stdio.SendNotification(t.Context(), mcp.JSONRPCNotification{
		JSONRPC: "2.0",
		Notification: mcp.Notification{
			Method: "test",
		},
	})
	require.NoError(t, err)
	require.Contains(t, sink.String(), `"method":"test"`)
}

// concurrentBuffer is a mutex-guarded buffer safe for the handler goroutines
// spawned by handleIncomingRequest to write while the test reads.
type concurrentBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

// Write appends p to the guarded builder.
func (b *concurrentBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// Close is a no-op.
func (b *concurrentBuffer) Close() error { return nil }

// String returns the buffered output.
func (b *concurrentBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestStdio_HandleIncomingRequest_NoHandler verifies an incoming request
// without a handler is answered with a -32601 method-not-found error.
func TestStdio_HandleIncomingRequest_NoHandler(t *testing.T) {
	stdio := newBareStdio()
	stdio.ctx = t.Context()
	sink := &concurrentBuffer{}
	stdio.stdin = sink

	stdio.handleIncomingRequest(JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)), Method: "sample",
	})
	require.Contains(t, sink.String(), "-32601")
	require.Contains(t, sink.String(), "No request handler configured")
}

// TestStdio_HandleIncomingRequest_HandlerResponds verifies a handler's
// response is written back to the client.
func TestStdio_HandleIncomingRequest_HandlerResponds(t *testing.T) {
	stdio := newBareStdio()
	stdio.ctx = t.Context()
	sink := &concurrentBuffer{}
	stdio.stdin = sink
	stdio.SetRequestHandler(func(ctx context.Context, request JSONRPCRequest) (*JSONRPCResponse, error) {
		return &JSONRPCResponse{JSONRPC: "2.0", ID: request.ID, Result: json.RawMessage(`"done"`)}, nil
	})

	stdio.handleIncomingRequest(JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)), Method: "sample",
	})

	// The handler runs asynchronously; wait for its response to be written.
	require.Eventually(t, func() bool {
		return strings.Contains(sink.String(), `"result":"done"`)
	}, 5*time.Second, 10*time.Millisecond)
}

// TestStdio_HandleIncomingRequest_HandlerError verifies a handler error is
// answered with a -32603 internal error.
func TestStdio_HandleIncomingRequest_HandlerError(t *testing.T) {
	stdio := newBareStdio()
	stdio.ctx = t.Context()
	sink := &concurrentBuffer{}
	stdio.stdin = sink
	stdio.SetRequestHandler(func(ctx context.Context, request JSONRPCRequest) (*JSONRPCResponse, error) {
		return nil, errors.New("handler exploded")
	})

	stdio.handleIncomingRequest(JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)), Method: "sample",
	})

	require.Eventually(t, func() bool {
		out := sink.String()
		return strings.Contains(out, "-32603") &&
			strings.Contains(out, "handler exploded")
	}, 5*time.Second, 10*time.Millisecond)
}

// TestStdio_HandleIncomingRequest_HandlerNilResponse verifies a nil handler
// response is not written back.
func TestStdio_HandleIncomingRequest_HandlerNilResponse(t *testing.T) {
	stdio := newBareStdio()
	stdio.ctx = t.Context()
	sink := &concurrentBuffer{}
	stdio.stdin = sink
	called := make(chan struct{})
	stdio.SetRequestHandler(func(ctx context.Context, request JSONRPCRequest) (*JSONRPCResponse, error) {
		close(called)
		return nil, nil
	})

	stdio.handleIncomingRequest(JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)), Method: "sample",
	})

	// A nil response must not be written back.
	select {
	case <-called:
	case <-t.Context().Done():
		t.Fatal("handler was not invoked")
	}
	require.NotContains(t, sink.String(), "result")
}

// TestStdio_HandleIncomingRequest_CtxCanceled verifies the handler is not
// invoked with a canceled context and an internal error is answered instead.
func TestStdio_HandleIncomingRequest_CtxCanceled(t *testing.T) {
	stdio := newBareStdio()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	stdio.ctx = ctx
	sink := &concurrentBuffer{}
	stdio.stdin = sink
	stdio.SetRequestHandler(func(ctx context.Context, request JSONRPCRequest) (*JSONRPCResponse, error) {
		t.Fatal("handler must not run with a canceled context")
		return nil, nil
	})

	stdio.handleIncomingRequest(JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)), Method: "sample",
	})

	require.Eventually(t, func() bool {
		out := sink.String()
		return strings.Contains(out, "-32603") &&
			strings.Contains(out, context.Canceled.Error())
	}, 5*time.Second, 10*time.Millisecond)
}

// TestStdio_SendResponse_MarshalError verifies sendResponse skips writing a
// response that cannot be marshaled.
func TestStdio_SendResponse_MarshalError(t *testing.T) {
	stdio := newBareStdio()
	sink := &concurrentBuffer{}
	stdio.stdin = sink

	// A channel cannot be marshaled; sendResponse must log and skip the write.
	stdio.sendResponse(JSONRPCResponse{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)), Result: json.RawMessage("{invalid"),
	})
	require.Empty(t, sink.String())
}

// TestStdio_SendResponse_WriteError verifies sendResponse logs rather than
// propagates a stdin write failure.
func TestStdio_SendResponse_WriteError(t *testing.T) {
	stdio := newBareStdio()
	stdio.stdin = &errWriteCloser{err: errors.New("stdin broken")}

	// The write error must be logged, not propagated.
	stdio.sendResponse(JSONRPCResponse{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)), Result: json.RawMessage(`"ok"`),
	})
}

// TestStdio_Close_CleanupErrors verifies Close captures stdin close errors and
// is a no-op on the second call.
func TestStdio_Close_CleanupErrors(t *testing.T) {
	// Both stdin and stderr close errors must be captured by Close.
	stdio := newBareStdio()
	stdio.stdin = &errWriteCloser{err: errors.New("stdin close failed")}
	stdio.stderr = failingReadCloser{err: io.EOF, closeErr: errors.New("stderr close failed")}

	err := stdio.Close()
	require.ErrorContains(t, err, "stdin close failed")

	// Second call is a no-op.
	require.NoError(t, stdio.Close())
}

// TestStdio_ReadResponses_DoneBeforeRead verifies readResponses returns
// immediately when done is already closed.
func TestStdio_ReadResponses_DoneBeforeRead(t *testing.T) {
	stdio := newBareStdio()
	close(stdio.done)
	stdio.stdout = nil // never touched because done wins the select

	require.NotPanics(t, func() {
		stdio.readResponses(nil)
	})
}

// TestStdio_ReadResponses_EOFClosesDone verifies readResponses closes done on
// stdout EOF so in-flight requests unblock.
func TestStdio_ReadResponses_EOFClosesDone(t *testing.T) {
	stdio := newBareStdio()
	stdio.stdout = bufio.NewReader(strings.NewReader(""))

	stdio.readResponses(nil)

	// EOF signals the server died; done must be closed so in-flight
	// requests unblock.
	select {
	case <-stdio.done:
	default:
		t.Fatal("done channel was not closed on stdout EOF")
	}
}

// TestStdio_ReadResponses_ReadErrorLogsAndClosesDone verifies a stdout read
// error closes done without panicking.
func TestStdio_ReadResponses_ReadErrorLogsAndClosesDone(t *testing.T) {
	stdio := newBareStdio()
	stdio.stdout = bufio.NewReader(failingReadCloser{err: errors.New("stdout exploded")})

	require.NotPanics(t, func() {
		stdio.readResponses(nil)
	})

	select {
	case <-stdio.done:
	default:
		t.Fatal("done channel was not closed on read error")
	}
}

// TestStdio_ReadResponses_NotifiesHandler verifies notifications read from
// stdout are delivered to the registered handler.
func TestStdio_ReadResponses_NotifiesHandler(t *testing.T) {
	stdio := newBareStdio()
	stdio.stdout = bufio.NewReader(strings.NewReader(
		`{"jsonrpc":"2.0","method":"notify","params":{}}` + "\n"))

	got := make(chan mcp.JSONRPCNotification, 1)
	stdio.SetNotificationHandler(func(n mcp.JSONRPCNotification) {
		got <- n
	})

	stdio.readResponses(nil)

	select {
	case n := <-got:
		require.Equal(t, "notify", n.Method)
	case <-t.Context().Done():
		t.Fatal("notification handler was not invoked")
	}
}

// blockingWriter blocks inside its first Write until release is closed,
// modeling a custom stderr destination that stalls without returning an error.
type blockingWriter struct {
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

// Write signals entered on the first call and then blocks until release is
// closed.
func (w *blockingWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(p), nil
}

// TestStdio_BlockingStderrWriterDoesNotStopDrain verifies that a stderr writer
// stuck inside Write cannot stop the pipe drain: once the mirror queue fills,
// further chunks are dropped and the drain keeps consuming the pipe, so a
// stderr-heavy server still answers Ping.
func TestStdio_BlockingStderrWriterDoesNotStopDrain(t *testing.T) {
	tempFile, err := os.CreateTemp(t.TempDir(), "mockstdio_stderr_flood_server")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tempFile.Close()
	mockServerPath := tempFile.Name() + ".exe"

	if compileErr := compileTestServerFromSource(mockServerPath, "../../testdata/mockstdio_stderr_flood_server.go"); compileErr != nil {
		t.Fatalf("Failed to compile mock server: %v", compileErr)
	}

	writer := &blockingWriter{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	stdio := NewStdioWithOptions(mockServerPath, nil, nil, WithCommandStderrWriter(writer))

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	if err := stdio.Start(ctx); err != nil {
		t.Fatalf("Failed to start Stdio transport: %v", err)
	}
	defer stdio.Close()

	// The server writes 320KB of stderr before answering; wait until the
	// mirror goroutine is stuck in the writer, then let the server finish
	// and verify the transport still answers requests.
	select {
	case <-writer.entered:
	case <-ctx.Done():
		t.Fatal("stderr writer was never called")
	}

	for i := range 3 {
		reqCtx, reqCancel := context.WithTimeout(ctx, 5*time.Second)
		_, err := stdio.SendRequest(reqCtx, JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      mcp.NewRequestId(int64(i + 1)),
			Method:  "ping",
		})
		reqCancel()
		if err != nil {
			t.Fatalf("SendRequest %d failed with blocked stderr writer: %v", i+1, err)
		}
	}

	close(writer.release)
}
