package server

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sessionWithSubscriptions is a test ClientSession that implements
// SessionWithResourceSubscriptions so the handlers can mutate it.
type sessionWithSubscriptions struct {
	sessionID           string
	notificationChannel chan mcp.JSONRPCNotification
	initialized         bool

	mu   sync.Mutex
	subs map[string]struct{}
}

func newSessionWithSubscriptions(id string) *sessionWithSubscriptions {
	return &sessionWithSubscriptions{
		sessionID:           id,
		notificationChannel: make(chan mcp.JSONRPCNotification, 16),
		subs:                map[string]struct{}{},
	}
}

func (s *sessionWithSubscriptions) SessionID() string { return s.sessionID }
func (s *sessionWithSubscriptions) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return s.notificationChannel
}
func (s *sessionWithSubscriptions) Initialize()       { s.initialized = true }
func (s *sessionWithSubscriptions) Initialized() bool { return s.initialized }

func (s *sessionWithSubscriptions) SubscribeToResource(uri string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subs[uri] = struct{}{}
}

func (s *sessionWithSubscriptions) UnsubscribeFromResource(uri string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subs, uri)
}

func (s *sessionWithSubscriptions) SubscribedResources() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.subs))
	for uri := range s.subs {
		out = append(out, uri)
	}
	return out
}

func (s *sessionWithSubscriptions) IsSubscribedToResource(uri string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.subs[uri]
	return ok
}

// Compile-time check.
var _ SessionWithResourceSubscriptions = (*sessionWithSubscriptions)(nil)

// TestMCPServer_SubscribeUnsubscribe_DispatcherWiring is the regression test
// for https://github.com/mark3labs/mcp-go/issues/865. It verifies that
// resources/subscribe and resources/unsubscribe are dispatched (not
// METHOD_NOT_FOUND) when the server advertises the resources.subscribe
// capability, and that they round-trip an empty result.
func TestMCPServer_SubscribeUnsubscribe_DispatcherWiring(t *testing.T) {
	t.Parallel()

	srv := NewMCPServer("test", "0.0.1", WithResourceCapabilities(true, false))

	subscribeMsg := []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "resources/subscribe",
		"params": {"uri": "file:///example"}
	}`)

	resp := srv.HandleMessage(t.Context(), subscribeMsg)
	require.NotNil(t, resp)
	successResp, ok := resp.(mcp.JSONRPCResponse)
	require.True(t, ok, "expected JSONRPCResponse for resources/subscribe, got %T: %#v", resp, resp)
	_, ok = successResp.Result.(mcp.EmptyResult)
	assert.True(t, ok, "subscribe result should be EmptyResult, got %T", successResp.Result)

	unsubscribeMsg := []byte(`{
		"jsonrpc": "2.0",
		"id": 2,
		"method": "resources/unsubscribe",
		"params": {"uri": "file:///example"}
	}`)

	resp = srv.HandleMessage(t.Context(), unsubscribeMsg)
	require.NotNil(t, resp)
	successResp, ok = resp.(mcp.JSONRPCResponse)
	require.True(t, ok, "expected JSONRPCResponse for resources/unsubscribe, got %T: %#v", resp, resp)
	_, ok = successResp.Result.(mcp.EmptyResult)
	assert.True(t, ok, "unsubscribe result should be EmptyResult, got %T", successResp.Result)
}

// TestMCPServer_Subscribe_WithoutCapability ensures servers that have not
// opted in to resources.subscribe still report METHOD_NOT_FOUND, matching the
// behaviour for any other unsupported capability.
func TestMCPServer_Subscribe_WithoutCapability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts []ServerOption
	}{
		{
			name: "no resource capabilities at all",
			opts: nil,
		},
		{
			name: "resources enabled but subscribe disabled",
			opts: []ServerOption{WithResourceCapabilities(false, true)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewMCPServer("test", "0.0.1", tt.opts...)

			resp := srv.HandleMessage(t.Context(), []byte(`{
				"jsonrpc": "2.0",
				"id": 1,
				"method": "resources/subscribe",
				"params": {"uri": "file:///example"}
			}`))

			errResp, ok := resp.(mcp.JSONRPCError)
			require.True(t, ok, "expected JSONRPCError, got %T: %#v", resp, resp)
			assert.Equal(t, mcp.METHOD_NOT_FOUND, errResp.Error.Code)
		})
	}
}

// TestMCPServer_Subscribe_MissingURI ensures the server rejects empty URIs
// with INVALID_PARAMS rather than silently acknowledging them.
func TestMCPServer_Subscribe_MissingURI(t *testing.T) {
	t.Parallel()

	srv := NewMCPServer("test", "0.0.1", WithResourceCapabilities(true, false))

	resp := srv.HandleMessage(t.Context(), []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "resources/subscribe",
		"params": {"uri": ""}
	}`))

	errResp, ok := resp.(mcp.JSONRPCError)
	require.True(t, ok, "expected JSONRPCError, got %T: %#v", resp, resp)
	assert.Equal(t, mcp.INVALID_PARAMS, errResp.Error.Code)
}

// TestMCPServer_Subscribe_TracksSessionState verifies that when the active
// session implements SessionWithResourceSubscriptions, the dispatcher records
// and clears subscription state on the session.
func TestMCPServer_Subscribe_TracksSessionState(t *testing.T) {
	t.Parallel()

	srv := NewMCPServer("test", "0.0.1", WithResourceCapabilities(true, false))
	session := newSessionWithSubscriptions("sess-1")
	require.NoError(t, srv.RegisterSession(t.Context(), session))

	ctx := srv.WithContext(t.Context(), session)

	subscribe := func(uri string) {
		t.Helper()
		msg, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "resources/subscribe",
			"params":  map[string]string{"uri": uri},
		})
		require.NoError(t, err)
		resp := srv.HandleMessage(ctx, msg)
		_, ok := resp.(mcp.JSONRPCResponse)
		require.True(t, ok, "subscribe(%q) should succeed, got %#v", uri, resp)
	}

	unsubscribe := func(uri string) {
		t.Helper()
		msg, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "resources/unsubscribe",
			"params":  map[string]string{"uri": uri},
		})
		require.NoError(t, err)
		resp := srv.HandleMessage(ctx, msg)
		_, ok := resp.(mcp.JSONRPCResponse)
		require.True(t, ok, "unsubscribe(%q) should succeed, got %#v", uri, resp)
	}

	subscribe("file:///a")
	subscribe("file:///b")
	// Duplicate subscribe is idempotent.
	subscribe("file:///a")

	got := session.SubscribedResources()
	slices.Sort(got)
	assert.Equal(t, []string{"file:///a", "file:///b"}, got)
	assert.True(t, session.IsSubscribedToResource("file:///a"))

	unsubscribe("file:///a")
	// Unsubscribing an unknown URI is a no-op, not an error.
	unsubscribe("file:///never-subscribed")

	got = session.SubscribedResources()
	assert.Equal(t, []string{"file:///b"}, got)
	assert.False(t, session.IsSubscribedToResource("file:///a"))
}

// TestMCPServer_Subscribe_HooksFire confirms the generator wired up
// before/after hooks for the new methods.
func TestMCPServer_Subscribe_HooksFire(t *testing.T) {
	t.Parallel()

	var (
		beforeSubscribe, afterSubscribe     int
		beforeUnsubscribe, afterUnsubscribe int
	)
	hooks := &Hooks{}
	hooks.AddBeforeSubscribe(func(_ context.Context, _ any, _ *mcp.SubscribeRequest) {
		beforeSubscribe++
	})
	hooks.AddAfterSubscribe(func(_ context.Context, _ any, _ *mcp.SubscribeRequest, _ *mcp.EmptyResult) {
		afterSubscribe++
	})
	hooks.AddBeforeUnsubscribe(func(_ context.Context, _ any, _ *mcp.UnsubscribeRequest) {
		beforeUnsubscribe++
	})
	hooks.AddAfterUnsubscribe(func(_ context.Context, _ any, _ *mcp.UnsubscribeRequest, _ *mcp.EmptyResult) {
		afterUnsubscribe++
	})

	srv := NewMCPServer("test", "0.0.1",
		WithResourceCapabilities(true, false),
		WithHooks(hooks),
	)

	_ = srv.HandleMessage(t.Context(), []byte(`{
		"jsonrpc": "2.0", "id": 1, "method": "resources/subscribe",
		"params": {"uri": "file:///a"}
	}`))
	_ = srv.HandleMessage(t.Context(), []byte(`{
		"jsonrpc": "2.0", "id": 2, "method": "resources/unsubscribe",
		"params": {"uri": "file:///a"}
	}`))

	assert.Equal(t, 1, beforeSubscribe)
	assert.Equal(t, 1, afterSubscribe)
	assert.Equal(t, 1, beforeUnsubscribe)
	assert.Equal(t, 1, afterUnsubscribe)
}
