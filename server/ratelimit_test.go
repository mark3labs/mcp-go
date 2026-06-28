package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// ratelimit_test.go is a BLIND, independent behavioral oracle for the
// WithRateLimit middleware described in issue #896. It is written purely from
// the dictated public API + the spec, WITHOUT reading server/ratelimit.go, so
// a wrong implementation should fail these tests.
//
// Driving model:
//   - We build an MCPServer with WithRateLimit(...) and register a tool whose
//     handler atomically counts how many invocations actually reach it.
//   - We invoke through the real server dispatch (s.handleToolCall), which is
//     the exact code path that composes toolHandlerMiddlewares around the tool
//     handler (see server.go ~line 1996-2007). This guarantees the middleware
//     under test is wired exactly as in production.
//   - A session is injected via s.WithContext(ctx, fakeSession{...}), matching
//     the repo idiom (see server_test.go fakeSession + WithContext usage). The
//     default KeyFunc is documented to key on the session ID, so distinct
//     fakeSession.sessionID values => distinct rate-limit buckets.
//
// ASSUMPTIONS (flagged for the dev/verifier):
//   - Session injection: default keying is per session ID; we set sessionID on
//     the fakeSession to control the bucket. If the default KeyFunc instead
//     keys on something else, tests 1/2/3 would need adjustment.
//   - Default-deny observable shape: spec says "rate-limit error". We assert
//     the observable form rather than an exact string: the denied call must NOT
//     reach the handler, and must return a distinguishable signal (a non-nil
//     IsError tool result, OR a non-nil Go error). See TestRateLimit_DefaultOnDeny.

// rlCall dispatches a tools/call through the server's real middleware chain
// for the given session ID and tool name. It returns the tool result and error
// exactly as the composed handler (incl. rate-limit middleware) produced them.
func rlCall(t *testing.T, s *MCPServer, sessionID, toolName string) (*mcp.CallToolResult, error) {
	t.Helper()
	ctx := s.WithContext(context.Background(), fakeSession{
		sessionID:           sessionID,
		notificationChannel: make(chan mcp.JSONRPCNotification, 1),
		initialized:         true,
	})
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: map[string]any{},
		},
	}
	res, reqErr := s.handleToolCall(ctx, 1, req)
	if reqErr != nil {
		return nil, reqErr.err
	}
	// handleToolCall returns `any` on success; for tools/call it is a
	// *mcp.CallToolResult.
	if res == nil {
		return nil, nil
	}
	ctr, ok := res.(*mcp.CallToolResult)
	require.Truef(t, ok, "expected *mcp.CallToolResult, got %T", res)
	return ctr, nil
}

// countingTool registers a tool whose handler increments *counter on each
// invocation that actually reaches it, then returns a plain success result.
func countingTool(s *MCPServer, name string, counter *int64) {
	s.AddTool(mcp.Tool{
		Name:        name,
		Description: "counting tool",
		InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]any{}},
	}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		atomic.AddInt64(counter, 1)
		return mcp.NewToolResultText("ok"), nil
	})
}

// denied reports whether a (result, err) pair represents a rate-limit denial:
// either a Go error surfaced, or an IsError tool result was returned.
func denied(res *mcp.CallToolResult, err error) bool {
	if err != nil {
		return true
	}
	if res != nil && res.IsError {
		return true
	}
	return false
}

// 1. Deny after burst: first N calls reach the handler, call N+1 is denied.
func TestRateLimit_DenyAfterBurst(t *testing.T) {
	const burst = 3
	var count int64
	s := NewMCPServer("x", "1.0",
		WithRateLimit(RateLimitOpts{
			RPS:   rate.Limit(0.0001), // effectively no refill within the test
			Burst: burst,
		}),
	)
	countingTool(s, "tool", &count)

	// First `burst` calls must all reach the handler.
	for i := 0; i < burst; i++ {
		res, err := rlCall(t, s, "sess-A", "tool")
		require.Falsef(t, denied(res, err), "call %d should be allowed (res=%v err=%v)", i+1, res, err)
	}
	require.Equal(t, int64(burst), atomic.LoadInt64(&count), "exactly burst calls should reach handler")

	// The next call must be denied and must NOT reach the handler.
	res, err := rlCall(t, s, "sess-A", "tool")
	require.True(t, denied(res, err), "call burst+1 should be denied")
	require.Equal(t, int64(burst), atomic.LoadInt64(&count), "denied call must not reach handler")
}

// 2. Per-key isolation: session A exhausting its bucket does not deny session B.
func TestRateLimit_PerKeyIsolation(t *testing.T) {
	const burst = 2
	var count int64
	s := NewMCPServer("x", "1.0",
		WithRateLimit(RateLimitOpts{
			RPS:   rate.Limit(0.0001),
			Burst: burst,
		}),
	)
	countingTool(s, "tool", &count)

	// Exhaust session A.
	for i := 0; i < burst; i++ {
		res, err := rlCall(t, s, "A", "tool")
		require.False(t, denied(res, err), "A allowed call %d", i+1)
	}
	res, err := rlCall(t, s, "A", "tool")
	require.True(t, denied(res, err), "A should now be denied")

	// Session B must still have its full burst available.
	for i := 0; i < burst; i++ {
		res, err := rlCall(t, s, "B", "tool")
		require.Falsef(t, denied(res, err), "B should be unaffected by A's exhaustion (call %d)", i+1)
	}
}

// 3. Global ceiling: across DIFFERENT sessions, calls are denied once the
// shared global bucket is exhausted even though each per-key bucket has room.
func TestRateLimit_GlobalCeiling(t *testing.T) {
	const globalBurst = 2
	var count int64
	s := NewMCPServer("x", "1.0",
		WithRateLimit(RateLimitOpts{
			// Generous per-key budget so per-key buckets never deny here.
			RPS:   rate.Limit(1000),
			Burst: 100,
			// Tight global ceiling.
			GlobalRPS:   rate.Limit(0.0001),
			GlobalBurst: globalBurst,
		}),
	)
	countingTool(s, "tool", &count)

	// Use a distinct session per call so per-key buckets are all fresh/full.
	allowed := 0
	deniedCount := 0
	for i := 0; i < globalBurst+3; i++ {
		res, err := rlCall(t, s, fmt.Sprintf("sess-%d", i), "tool")
		if denied(res, err) {
			deniedCount++
		} else {
			allowed++
		}
	}
	require.Equal(t, globalBurst, allowed, "only globalBurst calls should pass the global ceiling")
	require.Positive(t, deniedCount, "calls beyond the global ceiling must be denied")
	require.Equal(t, int64(globalBurst), atomic.LoadInt64(&count), "handler reached exactly globalBurst times")
}

// 4. Custom KeyFunc: a constant key collapses all callers into one bucket.
func TestRateLimit_CustomKeyFunc(t *testing.T) {
	const burst = 2
	var count int64
	s := NewMCPServer("x", "1.0",
		WithRateLimit(RateLimitOpts{
			RPS:     rate.Limit(0.0001),
			Burst:   burst,
			KeyFunc: func(ctx context.Context, req mcp.CallToolRequest) string { return "everyone" },
		}),
	)
	countingTool(s, "tool", &count)

	// Even though sessions differ, they all share one bucket.
	allowed := 0
	for i := 0; i < burst+2; i++ {
		res, err := rlCall(t, s, fmt.Sprintf("diff-%d", i), "tool")
		if !denied(res, err) {
			allowed++
		}
	}
	require.Equal(t, burst, allowed, "constant KeyFunc collapses all callers into one shared bucket")
	require.Equal(t, int64(burst), atomic.LoadInt64(&count))
}

// 5. Custom OnDeny: a custom OnDeny is invoked on denial and its returned
// result/error is what the caller sees.
func TestRateLimit_CustomOnDeny(t *testing.T) {
	const burst = 1
	var count int64
	var onDenyCalls int64
	sentinelErr := errors.New("sentinel-denied")
	var gotKey string

	s := NewMCPServer("x", "1.0",
		WithRateLimit(RateLimitOpts{
			RPS:   rate.Limit(0.0001),
			Burst: burst,
			OnDeny: func(ctx context.Context, req mcp.CallToolRequest, key string) (*mcp.CallToolResult, error) {
				atomic.AddInt64(&onDenyCalls, 1)
				gotKey = key
				return mcp.NewToolResultText("custom-denied-payload"), sentinelErr
			},
		}),
	)
	countingTool(s, "tool", &count)

	// First call allowed.
	res, err := rlCall(t, s, "k1", "tool")
	require.False(t, denied(res, err))

	// Second call denied -> our custom OnDeny must fire and surface its values.
	res, err = rlCall(t, s, "k1", "tool")
	require.Equal(t, int64(1), atomic.LoadInt64(&onDenyCalls), "custom OnDeny must be invoked exactly once on denial")
	require.ErrorIs(t, err, sentinelErr, "caller should see the error returned by custom OnDeny")
	require.Equal(t, "k1", gotKey, "OnDeny should receive the denied key")
	// The handler must not have run for the denied call.
	require.Equal(t, int64(1), atomic.LoadInt64(&count), "denied call must not reach handler")
	// If the impl surfaces the result too (non-error path), it should be our payload.
	if res != nil {
		require.Equal(t, "custom-denied-payload", textOf(res))
	}
}

// 6. Default OnDeny shape: with no OnDeny set, a denied call returns a
// distinguishable rate-limit signal and does not reach the handler.
func TestRateLimit_DefaultOnDeny(t *testing.T) {
	const burst = 1
	var count int64
	s := NewMCPServer("x", "1.0",
		WithRateLimit(RateLimitOpts{
			RPS:   rate.Limit(0.0001),
			Burst: burst,
		}),
	)
	countingTool(s, "tool", &count)

	// Consume the single token.
	res, err := rlCall(t, s, "k", "tool")
	require.False(t, denied(res, err), "first call allowed")

	// Denied call: must be distinguishable from success AND must not reach the
	// handler. We assert the observable form (IsError result OR Go error)
	// rather than an exact message string.
	res, err = rlCall(t, s, "k", "tool")
	require.True(t, denied(res, err), "default-deny must surface an error-ish signal (IsError result or Go error)")
	require.Equal(t, int64(burst), atomic.LoadInt64(&count), "denied call must not reach handler")

	// And it must be observably different from the allowed success result.
	if err == nil {
		require.NotNil(t, res)
		require.True(t, res.IsError, "default-deny result should be marked IsError")
	}
}

// 7. Concurrency / -race: hammer the middleware from many goroutines across
// several sessions; total-allowed per key must never exceed that key's burst
// (no refill within the test window) and the race detector must stay quiet.
func TestRateLimit_Concurrency(t *testing.T) {
	const (
		burst      = 5
		sessions   = 4
		perSession = 50
		goroutines = sessions * perSession
	)
	var allowed [sessions]int64
	var handlerCount int64

	s := NewMCPServer("x", "1.0",
		WithRateLimit(RateLimitOpts{
			RPS:   rate.Limit(0.0001), // no meaningful refill during the test
			Burst: burst,
		}),
	)
	s.AddTool(mcp.Tool{
		Name:        "tool",
		Description: "counting tool",
		InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]any{}},
	}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		atomic.AddInt64(&handlerCount, 1)
		return mcp.NewToolResultText("ok"), nil
	})

	var wg sync.WaitGroup
	for si := 0; si < sessions; si++ {
		sessID := fmt.Sprintf("s-%d", si)
		for c := 0; c < perSession; c++ {
			wg.Add(1)
			go func(si int, sessID string) {
				defer wg.Done()
				res, err := rlCall(t, s, sessID, "tool")
				if !denied(res, err) {
					atomic.AddInt64(&allowed[si], 1)
				}
			}(si, sessID)
		}
	}
	wg.Wait()

	var totalAllowed int64
	for si := 0; si < sessions; si++ {
		got := atomic.LoadInt64(&allowed[si])
		require.LessOrEqualf(t, got, int64(burst),
			"session %d allowed %d > burst %d", si, got, burst)
		totalAllowed += got
	}
	// Total allowed across all keys must never exceed the summed capacity.
	require.LessOrEqual(t, totalAllowed, int64(sessions*burst),
		"total allowed must not exceed combined per-key burst capacity")
	require.Equal(t, totalAllowed, atomic.LoadInt64(&handlerCount),
		"every allowed call (and only those) should have reached the handler")
}

// 8. Reaper (best-effort): with a very short ReapInterval + StaleTTL, an idle
// key's limiter should eventually be evicted, observable as the bucket
// resetting (a previously-exhausted key regains its full burst after the TTL).
// Written tolerantly: we poll with a timeout because eviction timing is
// inherently non-deterministic.
func TestRateLimit_ReaperEvictsIdleKey(t *testing.T) {
	const burst = 2
	var count int64
	s := NewMCPServer("x", "1.0",
		WithRateLimit(RateLimitOpts{
			// No natural refill, so a fresh allowance can only come from the
			// limiter being evicted and recreated by the reaper.
			RPS:          rate.Limit(0.0001),
			Burst:        burst,
			ReapInterval: 10 * time.Millisecond,
			StaleTTL:     20 * time.Millisecond,
		}),
	)
	countingTool(s, "tool", &count)

	// Exhaust the key's bucket.
	for i := 0; i < burst; i++ {
		res, err := rlCall(t, s, "idle-key", "tool")
		require.False(t, denied(res, err), "warmup call %d allowed", i+1)
	}
	res, err := rlCall(t, s, "idle-key", "tool")
	require.True(t, denied(res, err), "key should be exhausted before idle period")

	// Stay idle long enough for the reaper to evict the stale limiter, then a
	// brand-new request should get a fresh full burst. Poll tolerantly.
	deadline := time.Now().Add(2 * time.Second)
	regained := false
	for time.Now().Before(deadline) {
		time.Sleep(30 * time.Millisecond)
		res, err := rlCall(t, s, "idle-key", "tool")
		if !denied(res, err) {
			regained = true
			break
		}
	}
	assert.True(t, regained,
		"after StaleTTL idle, the reaper should evict the limiter so the key regains its burst")
}

// textOf extracts the first text content from a tool result, for assertions.
func textOf(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}
