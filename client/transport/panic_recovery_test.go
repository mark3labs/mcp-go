package transport

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// panicTestLogger captures log output for test assertions.
type panicTestLogger struct {
	mu       sync.Mutex
	messages []string
}

// Infof is a no-op for test logging.
func (l *panicTestLogger) Infof(_ string, _ ...any) {}

// Errorf captures formatted error messages for later assertion.
func (l *panicTestLogger) Errorf(format string, v ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, fmt.Sprintf(format, v...))
}

// hasMessageContaining reports whether any captured message contains substr.
// Safe for concurrent use.
func (l *panicTestLogger) hasMessageContaining(substr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, m := range l.messages {
		if strings.Contains(m, substr) {
			return true
		}
	}
	return false
}

// waitForMessage polls until a message containing substr appears or timeout expires.
func (l *panicTestLogger) waitForMessage(substr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if l.hasMessageContaining(substr) {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// TestPanicRecovery_HandleIncomingRequest verifies that a panicking request
// handler is recovered, logged, and does not crash the process.
func TestPanicRecovery_HandleIncomingRequest(t *testing.T) {
	logger := &panicTestLogger{}

	c := &StreamableHTTP{
		logger: logger,
		closed: make(chan struct{}),
	}

	// Set a handler that panics
	c.requestHandler = func(_ context.Context, _ JSONRPCRequest) (*JSONRPCResponse, error) {
		panic("test panic in request handler")
	}

	request := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(int64(1)),
	}
	request.Method = "sampling/createMessage"

	c.handleIncomingRequest(t.Context(), request)

	// Wait for the goroutine to recover and log (no fixed sleep)
	require.True(t, logger.waitForMessage("panic handling server request", 2*time.Second),
		"expected panic recovery log message within timeout")
}

// TestContextAwareOfClientClose_CleanShutdown verifies that the context-close
// watcher goroutine completes without crashing when the closed channel fires.
func TestContextAwareOfClientClose_CleanShutdown(t *testing.T) {
	logger := &panicTestLogger{}

	c := &StreamableHTTP{
		logger: logger,
		closed: make(chan struct{}),
	}

	childCtx, childCancel := c.contextAwareOfClientClose(t.Context())
	defer childCancel()

	// Close the client channel to trigger the goroutine's select case
	close(c.closed)

	// Wait for the goroutine to cancel the child context (proves it ran)
	select {
	case <-childCtx.Done():
		// Goroutine fired cancel() as expected
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for context cancellation")
	}

	// No panic means the recovery defer is in place and the goroutine exits cleanly
	assert.False(t, logger.hasMessageContaining("panic"),
		"unexpected panic logged during clean shutdown")
}
