package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

// gatedSSEWriter is a streaming HTTPResponseWriter that blocks the first
// write carrying marker until release is closed, standing in for a client
// that is slow to take the request off the POST's stream.
type gatedSSEWriter struct {
	mu      sync.Mutex
	header  http.Header
	body    bytes.Buffer
	marker  string
	hit     sync.Once
	blocked chan struct{}
	release chan struct{}
}

func newGatedSSEWriter(marker string) *gatedSSEWriter {
	return &gatedSSEWriter{
		header:  make(http.Header),
		marker:  marker,
		blocked: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (g *gatedSSEWriter) Header() http.Header { return g.header }
func (g *gatedSSEWriter) WriteHeader(int)     {}
func (g *gatedSSEWriter) Flush()              {}
func (g *gatedSSEWriter) CanStream() bool     { return true }

func (g *gatedSSEWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte(g.marker)) {
		g.hit.Do(func() {
			close(g.blocked)
			<-g.release
		})
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.body.Write(p)
}

func (g *gatedSSEWriter) output() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.body.String()
}

// blockingEventStore blocks the recording of the first event carrying marker
// until release is closed, standing in for a slow event store.
type blockingEventStore struct {
	EventStore
	marker  string
	hit     sync.Once
	blocked chan struct{}
	release chan struct{}
}

func newBlockingEventStore(marker string) *blockingEventStore {
	return &blockingEventStore{
		EventStore: NewInMemoryEventStore(),
		marker:     marker,
		blocked:    make(chan struct{}),
		release:    make(chan struct{}),
	}
}

func (b *blockingEventStore) StoreEvent(ctx context.Context, sessionID, streamID string, message json.RawMessage) (string, error) {
	if bytes.Contains(message, []byte(b.marker)) {
		b.hit.Do(func() {
			close(b.blocked)
			<-b.release
		})
	}
	return b.EventStore.StoreEvent(ctx, sessionID, streamID, message)
}

// sseMessages returns the JSON message of every data line of an SSE body.
func sseMessages(t *testing.T, body string) []json.RawMessage {
	t.Helper()
	var messages []json.RawMessage
	for line := range strings.SplitSeq(body, "\n") {
		if data, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "data: "); ok {
			messages = append(messages, json.RawMessage(data))
		}
	}
	return messages
}

// requireWithin fails the test unless ch yields within five seconds.
func requireWithin[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		panic("unreachable")
	}
}

// runBlockedRequestCancellation issues a roots/list request from a tool
// handler on the POST's stream, blocks the stream's delivery of it through
// blocked, ends the request's context meanwhile, then releases the block, and
// checks the contract of a cancellation decided while the request is being
// written or recorded:
//   - the caller returns without waiting on the blocked stream;
//   - the caller publishes no cancellation of its own, neither on the shared
//     session channel, where another stream could deliver it before the
//     request, nor as a second one on the POST's stream;
//   - after the release, the request precedes exactly one cancellation that
//     names it.
func runBlockedRequestCancellation(
	t *testing.T,
	httpServer *StreamableHTTPServer,
	mcpServer *MCPServer,
	w HTTPResponseWriter,
	blocked <-chan struct{},
	release func(),
	output func() string,
) {
	t.Helper()
	type asked struct {
		cancel  context.CancelFunc
		session *streamableHttpSession
	}
	askedCh := make(chan asked, 1)
	callErr := make(chan error, 1)
	mcpServer.AddTool(mcp.NewTool("ask"), func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session, ok := ClientSessionFromContext(ctx).(*streamableHttpSession)
		if !ok {
			return nil, context.Canceled
		}
		ctx, cancel := context.WithCancel(ctx)
		askedCh <- asked{cancel: cancel, session: session}
		_, err := mcpServer.RequestRoots(ctx, mcp.ListRootsRequest{})
		callErr <- err
		return mcp.NewToolResultText("done"), nil
	})

	testServer := httptest.NewServer(httpServer)
	defer testServer.Close()
	sessionID := initializeSession(t, testServer.URL)

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 7, "method": "tools/call",
		"params": map[string]any{"name": "ask"},
	})
	require.NoError(t, err)
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		httpServer.Handle(w, &HTTPRequest{
			Method: http.MethodPost,
			URL:    &url.URL{Path: "/mcp"},
			Header: http.Header{
				"Content-Type":     []string{"application/json"},
				"Accept":           []string{"application/json, text/event-stream"},
				HeaderKeySessionID: []string{sessionID},
			},
			Body:    body,
			Context: t.Context(),
		})
	}()

	// The stream is now delivering the request, and stuck doing it.
	got := requireWithin(t, askedCh, "the tool handler to ask")
	requireWithin(t, blocked, "the request's delivery to block")

	got.cancel()
	require.ErrorIs(t, requireWithin(t, callErr, "the caller to return while the stream is blocked"), context.Canceled)
	assert.Empty(t, got.session.notificationChannel,
		"the caller must not publish the cancellation where another stream could deliver it before the request")

	release()
	requireWithin(t, handled, "the POST to finish after the release")

	var requestID int64
	var methods []string
	var cancellations []mcp.JSONRPCNotification
	for _, raw := range sseMessages(t, output()) {
		var probe struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		require.NoError(t, json.Unmarshal(raw, &probe))
		switch probe.Method {
		case "":
			methods = append(methods, "response")
		case string(mcp.MethodListRoots):
			methods = append(methods, probe.Method)
			require.NoError(t, json.Unmarshal(probe.ID, &requestID))
		case string(mcp.MethodNotificationCancelled):
			methods = append(methods, probe.Method)
			var n mcp.JSONRPCNotification
			require.NoError(t, json.Unmarshal(raw, &n))
			cancellations = append(cancellations, n)
		}
	}
	require.Equal(t, []string{string(mcp.MethodListRoots), string(mcp.MethodNotificationCancelled), "response"}, methods,
		"the request must precede exactly one cancellation, ahead of the tool's response")
	require.Len(t, cancellations, 1)
	requireCancels(t, cancellations[0], requestID)
}

// A request whose write to the POST's stream is blocked when the caller gives
// up is withdrawn by the stream itself, once, right behind the request.
func TestStreamableHTTPServer_CancelWhileRequestWriteIsBlocked(t *testing.T) {
	mcpServer := NewMCPServer("test-server", "1.0.0", WithRoots())
	httpServer := NewStreamableHTTPServer(mcpServer)
	w := newGatedSSEWriter(string(mcp.MethodListRoots))
	runBlockedRequestCancellation(t, httpServer, mcpServer, w, w.blocked, func() { close(w.release) }, w.output)
}

// A request being recorded in the event store when the caller gives up is
// withdrawn the same way: the stream forwards the cancellation behind the
// recorded request, and no other stream can deliver it first.
func TestStreamableHTTPServer_CancelWhileRequestRecordIsBlocked(t *testing.T) {
	mcpServer := NewMCPServer("test-server", "1.0.0", WithRoots())
	store := newBlockingEventStore(string(mcp.MethodListRoots))
	httpServer := NewStreamableHTTPServer(mcpServer, WithEventStore(store))
	w := newGatedSSEWriter("") // an empty marker matches every write, so gate nothing:
	w.hit.Do(func() {})        // consume the gate so writes pass straight through
	runBlockedRequestCancellation(t, httpServer, mcpServer, w, store.blocked, func() { close(store.release) }, w.output)
}
