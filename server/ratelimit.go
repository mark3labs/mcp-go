package server

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/mark3labs/mcp-go/mcp"
)

// Default reaper tuning, applied when the corresponding RateLimitOpts field is
// left at its zero value.
const (
	defaultReapInterval = 5 * time.Minute
	defaultStaleTTL     = 10 * time.Minute

	// fallbackRateLimitKey is used when KeyFunc yields the empty string (for
	// example, an unauthenticated call with no client session). All such calls
	// share one bucket so a missing key cannot bypass the limiter.
	fallbackRateLimitKey = "-"
)

// RateLimitOpts configures the per-key rate-limit tool-handler middleware
// installed by [WithRateLimit].
//
// The middleware admits at most RPS tool calls per second per key (with bursts
// up to Burst), where the key defaults to the client session ID. An optional
// global ceiling (GlobalRPS) caps the aggregate rate across every key.
type RateLimitOpts struct {
	// RPS is the sustained per-key request rate (tokens per second). Required;
	// must be > 0. A value of [rate.Inf] disables per-key throttling.
	RPS rate.Limit

	// Burst is the per-key burst size (bucket capacity). Required; must be >= 1.
	Burst int

	// GlobalRPS is an optional ceiling on the aggregate request rate across all
	// keys. When <= 0 the global limiter is disabled and only the per-key limit
	// applies. A value of [rate.Inf] admits any rate (effectively disabled).
	GlobalRPS rate.Limit

	// GlobalBurst is the burst size for the global limiter. It is only used when
	// GlobalRPS > 0; in that case it must be >= 1.
	GlobalBurst int

	// KeyFunc derives the rate-limit key for a call. When nil, the key is the
	// client session ID (see [ClientSessionFromContext]); calls without a
	// session, or whose key is empty, share a single fallback bucket.
	KeyFunc func(ctx context.Context, req mcp.CallToolRequest) string

	// OnDeny produces the response returned to the caller when a call is denied
	// (the wrapped handler is not invoked). When nil, the default returns a
	// tool execution error result (mcp.NewToolResultError) carrying the message
	// "rate limit exceeded".
	//
	// Note on JSON-RPC error codes: a middleware that returns a non-nil error is
	// always surfaced by the server as JSON-RPC -32603 (INTERNAL_ERROR); the
	// internal requestError type that carries a custom code is unexported, so a
	// dedicated rate-limit code (e.g. -32029) cannot be produced from here. The
	// default therefore denies via an IsError CallToolResult, which keeps the
	// failure in the model's context window per the SEP-1303 convention used
	// elsewhere in this package. Callers needing different semantics can supply
	// their own OnDeny.
	OnDeny func(ctx context.Context, req mcp.CallToolRequest, key string) (*mcp.CallToolResult, error)

	// ReapInterval is how often idle per-key limiters are swept and evicted.
	// 0 selects the default of 5m. A negative value disables the reaper, in
	// which case per-key limiters are retained for the lifetime of the server
	// (suitable only when the key space is bounded).
	ReapInterval time.Duration

	// StaleTTL is how long a key may be idle before its limiter is evicted by
	// the reaper. 0 selects the default of 10m. Ignored when the reaper is
	// disabled (ReapInterval < 0).
	StaleTTL time.Duration

	// Logger, when non-nil, receives a Warn line per denied call with the key,
	// tool name, and session ID. When nil, denials are not logged.
	Logger *slog.Logger
}

// WithRateLimit installs a per-key (by default, per-session) rate-limit
// tool-handler middleware. Each tool call consumes one token from the caller's
// per-key bucket; when the bucket is empty (or the optional global bucket is
// empty) the call is denied via opts.OnDeny without invoking the wrapped
// handler.
//
// Limiters are created lazily per key and evicted after StaleTTL of inactivity
// by a single background reaper (see RateLimitOpts.ReapInterval). The reaper is
// started lazily when the first key appears and stops itself once every key has
// been evicted, so an idle server holds no background goroutine; it re-arms when
// traffic resumes. This avoids a goroutine leak even though MCPServer exposes no
// shutdown hook to tie a long-lived ticker to.
//
// WithRateLimit panics if opts is invalid (RPS <= 0, Burst < 1, or GlobalRPS > 0
// with GlobalBurst < 1), matching the fail-fast convention for misconfigured
// server options.
func WithRateLimit(opts RateLimitOpts) ServerOption {
	if opts.RPS <= 0 {
		panic(fmt.Sprintf("server.WithRateLimit: RPS must be > 0, got %v", opts.RPS))
	}
	if opts.Burst < 1 {
		panic(fmt.Sprintf("server.WithRateLimit: Burst must be >= 1, got %d", opts.Burst))
	}
	if opts.GlobalRPS > 0 && opts.GlobalBurst < 1 {
		panic(fmt.Sprintf("server.WithRateLimit: GlobalBurst must be >= 1 when GlobalRPS > 0, got %d", opts.GlobalBurst))
	}

	store := newRateLimitStore(opts)

	return func(s *MCPServer) {
		s.toolMiddlewareMu.Lock()
		s.toolHandlerMiddlewares = append(s.toolHandlerMiddlewares, store.middleware())
		s.toolMiddlewareMu.Unlock()
	}
}

// rateLimitEntry is one per-key token bucket plus its last-seen timestamp,
// consulted by the reaper.
type rateLimitEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// rateLimitStore holds the per-key limiters, the optional shared global
// limiter, and the lazily-started reaper that evicts idle keys.
type rateLimitStore struct {
	rps   rate.Limit
	burst int

	keyFunc func(ctx context.Context, req mcp.CallToolRequest) string
	onDeny  func(ctx context.Context, req mcp.CallToolRequest, key string) (*mcp.CallToolResult, error)
	logger  *slog.Logger

	reapInterval time.Duration
	staleTTL     time.Duration

	global *rate.Limiter // nil when GlobalRPS <= 0

	mu       sync.Mutex
	entries  map[string]*rateLimitEntry
	reaperOn bool             // whether a reaper goroutine is currently running
	now      func() time.Time // injectable clock; defaults to time.Now
}

func newRateLimitStore(opts RateLimitOpts) *rateLimitStore {
	st := &rateLimitStore{
		rps:          opts.RPS,
		burst:        opts.Burst,
		keyFunc:      opts.KeyFunc,
		onDeny:       opts.OnDeny,
		logger:       opts.Logger,
		reapInterval: opts.ReapInterval,
		staleTTL:     opts.StaleTTL,
		entries:      make(map[string]*rateLimitEntry),
		now:          time.Now,
	}
	if st.keyFunc == nil {
		st.keyFunc = defaultRateLimitKey
	}
	if st.onDeny == nil {
		st.onDeny = defaultRateLimitDeny
	}
	if st.reapInterval == 0 {
		st.reapInterval = defaultReapInterval
	}
	if st.staleTTL == 0 {
		st.staleTTL = defaultStaleTTL
	}
	if opts.GlobalRPS > 0 {
		st.global = rate.NewLimiter(opts.GlobalRPS, opts.GlobalBurst)
	}
	return st
}

// defaultRateLimitKey keys on the client session ID, falling back to a single
// shared bucket when no session is present.
func defaultRateLimitKey(ctx context.Context, _ mcp.CallToolRequest) string {
	if session := ClientSessionFromContext(ctx); session != nil {
		if id := session.SessionID(); id != "" {
			return id
		}
	}
	return fallbackRateLimitKey
}

// defaultRateLimitDeny returns a tool execution error result. See the OnDeny
// field documentation for why a CallToolResult (rather than a typed JSON-RPC
// error) is the default.
func defaultRateLimitDeny(_ context.Context, _ mcp.CallToolRequest, _ string) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError("rate limit exceeded: too many tool calls, please retry later"), nil
}

// middleware returns the ToolHandlerMiddleware backed by this store.
func (st *rateLimitStore) middleware() ToolHandlerMiddleware {
	return func(next ToolHandlerFunc) ToolHandlerFunc {
		return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			key := st.keyFunc(ctx, request)
			if key == "" {
				key = fallbackRateLimitKey
			}

			if !st.allow(key) {
				if st.logger != nil {
					st.logger.LogAttrs(ctx, slog.LevelWarn, "mcp.ratelimit.denied",
						slog.String("mcp.ratelimit.key", key),
						slog.String(logKeyToolName, request.Params.Name),
					)
				}
				return st.onDeny(ctx, request, key)
			}
			return next(ctx, request)
		}
	}
}

// allow consumes one token for key (and one from the global bucket when
// configured), returning whether the call may proceed. A denial does not refund
// any token already consumed from the global bucket; under contention this can
// cost a per-key allowance, which is acceptable for a coarse safety ceiling.
func (st *rateLimitStore) allow(key string) bool {
	if st.global != nil && !st.global.Allow() {
		// Still record activity so the per-key entry's lastSeen advances and the
		// reaper does not evict an actively-denied key prematurely.
		st.touch(key)
		return false
	}

	limiter := st.touch(key)
	return limiter.Allow()
}

// touch fetches or creates the limiter for key, refreshes its lastSeen, and
// ensures the reaper is running when eviction is enabled. It returns the
// key's limiter.
func (st *rateLimitStore) touch(key string) *rate.Limiter {
	st.mu.Lock()
	defer st.mu.Unlock()

	entry, ok := st.entries[key]
	if !ok {
		entry = &rateLimitEntry{limiter: rate.NewLimiter(st.rps, st.burst)}
		st.entries[key] = entry
		st.maybeStartReaperLocked()
	}
	entry.lastSeen = st.now()
	return entry.limiter
}

// maybeStartReaperLocked starts the reaper goroutine if eviction is enabled and
// one is not already running. The caller must hold st.mu.
func (st *rateLimitStore) maybeStartReaperLocked() {
	if st.reapInterval < 0 || st.reaperOn {
		return
	}
	st.reaperOn = true
	go st.reap(st.reapInterval, st.staleTTL)
}

// reap periodically evicts idle limiters. It exits once the store is empty so an
// idle server retains no background goroutine; touch restarts it when a new key
// appears.
func (st *rateLimitStore) reap(interval, ttl time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		if st.sweep(ttl) {
			return // store drained: stop until traffic resumes
		}
	}
}

// sweep evicts entries idle longer than ttl and reports whether the store is now
// empty. When it empties the store it clears the running flag so the next touch
// re-arms the reaper.
func (st *rateLimitStore) sweep(ttl time.Duration) (drained bool) {
	st.mu.Lock()
	defer st.mu.Unlock()

	cutoff := st.now().Add(-ttl)
	for key, entry := range st.entries {
		if entry.lastSeen.Before(cutoff) {
			delete(st.entries, key)
		}
	}
	if len(st.entries) == 0 {
		st.reaperOn = false
		return true
	}
	return false
}
