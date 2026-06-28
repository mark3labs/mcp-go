package server

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// TestRateLimit_PanicsOnBadOpts locks the fail-fast validation at construction.
func TestRateLimit_PanicsOnBadOpts(t *testing.T) {
	bad := []RateLimitOpts{
		{RPS: 0, Burst: 5},  // RPS must be > 0
		{RPS: 10, Burst: 0}, // Burst must be >= 1
		{RPS: 10, Burst: 5, GlobalRPS: 10, GlobalBurst: 0}, // GlobalBurst must be >= 1 when GlobalRPS > 0
	}
	for _, opts := range bad {
		opts := opts
		require.Panics(t, func() { WithRateLimit(opts) })
	}
}

// TestRateLimit_NoSessionFallbackBucket verifies that calls without a session
// share the single fallback bucket (so an unauthenticated flood is still capped).
func TestRateLimit_NoSessionFallbackBucket(t *testing.T) {
	st := newRateLimitStore(RateLimitOpts{RPS: rate.Limit(0.0001), Burst: 2})
	var calls int
	h := st.middleware()(func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls++
		return &mcp.CallToolResult{}, nil
	})
	ctx := context.Background() // no session in context → fallback "-" key
	for i := 0; i < 3; i++ {
		_, _ = h(ctx, mcp.CallToolRequest{})
	}
	require.Equal(t, 2, calls, "no-session calls share the fallback bucket (burst 2)")
}

// TestRateLimit_ReaperDisabled verifies ReapInterval < 0 starts no reaper.
func TestRateLimit_ReaperDisabled(t *testing.T) {
	st := newRateLimitStore(RateLimitOpts{RPS: 10, Burst: 2, ReapInterval: -1})
	h := st.middleware()(func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})
	_, _ = h(context.Background(), mcp.CallToolRequest{})
	st.mu.Lock()
	on := st.reaperOn
	st.mu.Unlock()
	require.False(t, on, "ReapInterval < 0 must disable the reaper")
}
