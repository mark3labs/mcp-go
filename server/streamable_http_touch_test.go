package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newTouchTestServer builds a streamable server with the idle sweeper on, so
// touchSession is live; the caller shuts it down.
func newTouchTestServer() *StreamableHTTPServer {
	return NewStreamableHTTPServer(NewMCPServer("test", "1.0.0"), WithSessionIdleTTL(time.Hour))
}

// TestTouchSessionKnownSessionAllocatesNothing pins that a session that is
// already tracked is touched without allocating: touchSession runs on every
// request once WithSessionIdleTTL is set.
func TestTouchSessionKnownSessionAllocatesNothing(t *testing.T) {
	s := newTouchTestServer()
	defer func() { _ = s.Shutdown(t.Context()) }()
	const sessionID = "mcp-session-known"
	s.touchSession(sessionID)

	allocs := testing.AllocsPerRun(1000, func() { s.touchSession(sessionID) })
	require.Zero(t, allocs, "touchSession on a known session must not allocate")
}

// BenchmarkTouchSessionKnown measures touchSession on a session that is
// already tracked, the per-request path once WithSessionIdleTTL is set.
func BenchmarkTouchSessionKnown(b *testing.B) {
	s := newTouchTestServer()
	defer func() { _ = s.Shutdown(b.Context()) }()
	const sessionID = "mcp-session-known"
	s.touchSession(sessionID)

	b.ReportAllocs()
	for b.Loop() {
		s.touchSession(sessionID)
	}
}
