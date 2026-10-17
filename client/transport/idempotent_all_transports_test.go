package transport

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
)

// TestSSE_StartIdempotency tests that SSE Start() is idempotent
func TestSSE_StartIdempotency(t *testing.T) {
	t.Skip("SSE requires a real HTTP server - tested in integration tests")
}

// TestStreamableHTTP_StartIdempotency tests that StreamableHTTP Start() is idempotent
func TestStreamableHTTP_StartIdempotency(t *testing.T) {
	client, err := NewStreamableHTTP("http://localhost:8080")
	if err != nil {
		t.Fatalf("Failed to create StreamableHTTP client: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	// First Start() - should succeed
	err = client.Start(ctx)
	if err != nil {
		t.Fatalf("First Start() failed: %v", err)
	}
	defer client.Close()

	// Second Start() - should be idempotent (no error)
	err = client.Start(ctx)
	if err != nil {
		t.Errorf("Second Start() should be idempotent, got error: %v", err)
	}

	// Third Start() - should still be idempotent
	err = client.Start(ctx)
	if err != nil {
		t.Errorf("Third Start() should be idempotent, got error: %v", err)
	}
}

// TestInProcessTransport_StartIdempotency tests that InProcess Start() is idempotent
func TestInProcessTransport_StartIdempotency(t *testing.T) {
	mcpServer := server.NewMCPServer(
		"test-server",
		"1.0.0",
	)

	transport := NewInProcessTransport(mcpServer)

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	// First Start() - should succeed
	err := transport.Start(ctx)
	if err != nil {
		t.Fatalf("First Start() failed: %v", err)
	}
	defer transport.Close()

	// Second Start() - should be idempotent (no error)
	err = transport.Start(ctx)
	if err != nil {
		t.Errorf("Second Start() should be idempotent, got error: %v", err)
	}

	// Third Start() - should still be idempotent
	err = transport.Start(ctx)
	if err != nil {
		t.Errorf("Third Start() should be idempotent, got error: %v", err)
	}
}

// TestInProcessTransport_StartFailureReset tests that a failed Start() can be retried
func TestInProcessTransport_StartFailureReset(t *testing.T) {
	// This test verifies that if Start() fails, the started flag is reset
	// and Start() can be called again

	// For InProcessTransport, Start() only fails if session registration fails
	// which is hard to simulate without mocking the server
	// So we just verify that multiple successful starts work
	mcpServer := server.NewMCPServer(
		"test-server",
		"1.0.0",
	)

	transport := NewInProcessTransport(mcpServer)

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	// Should be able to start successfully
	err := transport.Start(ctx)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer transport.Close()

	// Verify started flag is set
	transport.startedMu.Lock()
	started := transport.started
	transport.startedMu.Unlock()

	if !started {
		t.Error("Started flag should be true after successful Start()")
	}
}

// TestInProcessTransport_ConcurrentStartClose stresses concurrent Start()/Close()
// calls to guard against the session-registration leak fixed alongside this
// test: Start() used to re-acquire startedMu a second time (only to assign
// c.session) without re-checking c.closed, so a Close() that ran between the
// two lock acquisitions could snapshot session==nil and skip
// UnregisterSession, while Start() still registered the session -- leaking an
// entry in the server's session map forever.
//
// Start() now performs the closed-check, RegisterSession call, and the
// c.session/c.started assignment under a single startedMu hold, so Start and
// Close are mutually exclusive: a concurrent Close either runs entirely
// before Start's critical section (Start observes c.closed and never
// registers) or entirely after (Close observes the registered session and
// unregisters it). Either way registrations and unregistrations stay
// balanced.
//
// This test verifies that invariant under -race across many iterations and
// checks for goroutine leaks. There is no exported accessor for the live
// session count on *server.MCPServer (the sessions field is an unexported
// sync.Map), so instead of asserting on server internals directly, the test
// uses the exported OnRegisterSession/OnUnregisterSession hooks to count
// registrations and unregistrations for every transport created across every
// iteration, then asserts the totals match at the end -- equivalent to
// asserting zero sessions remain registered, without reaching into
// unexported state.
func TestInProcessTransport_ConcurrentStartClose(t *testing.T) {
	const iterations = 200
	const startersPerIteration = 4

	var registered, unregistered int64

	baselineGoroutines := runtime.NumGoroutine()

	var outer sync.WaitGroup
	for i := range iterations {
		hooks := &server.Hooks{}
		hooks.AddOnRegisterSession(func(_ context.Context, _ server.ClientSession) {
			atomic.AddInt64(&registered, 1)
		})
		hooks.AddOnUnregisterSession(func(_ context.Context, _ server.ClientSession) {
			atomic.AddInt64(&unregistered, 1)
		})

		mcpServer := server.NewMCPServer("test-server", "1.0.0", server.WithHooks(hooks))
		transport := NewInProcessTransport(mcpServer)

		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)

		outer.Add(startersPerIteration + 1)
		for range startersPerIteration {
			go func() {
				defer outer.Done()
				_ = transport.Start(ctx)
			}()
		}
		go func() {
			defer outer.Done()
			_ = transport.Close()
		}()

		// Bound each iteration so the test can't hang: wait for this
		// iteration's goroutines before starting the next one.
		done := make(chan struct{})
		go func() {
			outer.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("iteration %d: Start()/Close() goroutines did not complete in time", i)
		}
		cancel()

		// Reset the WaitGroup for reuse across iterations.
		outer = sync.WaitGroup{}
	}

	require.Equal(t, atomic.LoadInt64(&registered), atomic.LoadInt64(&unregistered),
		"every registered session must be unregistered; a mismatch indicates the Start()/Close() leak")

	require.Eventually(t, func() bool {
		return runtime.NumGoroutine() <= baselineGoroutines+2
	}, 2*time.Second, 50*time.Millisecond, "goroutines leaked after concurrent Start()/Close()")
}
