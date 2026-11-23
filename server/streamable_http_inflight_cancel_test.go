package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	requireRequestThenOneCancellation(t, sseMessages(t, output()))
}

// requireRequestThenOneCancellation checks that messages carry the roots/list
// request, then exactly one notifications/cancelled naming it, then the tool's
// response, ignoring any other notification.
func requireRequestThenOneCancellation(t *testing.T, messages []json.RawMessage) {
	t.Helper()
	var requestID int64
	var methods []string
	var cancellations []mcp.JSONRPCNotification
	for _, raw := range messages {
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

// A request still queued for a POST's stream when the POST finishes is
// settled late: with an event store it is recorded for replay. When the
// caller gives up while that record is blocked, the stream owns the
// cancellation exactly as for a live write: the caller returns without
// waiting, publishes nothing where another stream could deliver it first,
// and the recorded request is followed by exactly one cancellation.
//
// The request is forced into late settlement. The POST's forwarder is parked
// recording a notification while the request is queued, and the tool handler
// returns, so the POST closes its stream before waiting on the forwarder.
// Once that stream is closed, a queued request can no longer take the
// forwarder's write path: whichever of the forwarder or the response path's
// drain takes it settles it late.
func TestStreamableHTTPServer_CancelWhileLateSettlementRecordIsBlocked(t *testing.T) {
	const forwarderMarker = "park-the-forwarder"
	record := newBlockingEventStore(string(mcp.MethodListRoots))
	forwarder := &blockingEventStore{
		EventStore: record,
		marker:     forwarderMarker,
		blocked:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	mcpServer := NewMCPServer("test-server", "1.0.0", WithRoots())
	httpServer := NewStreamableHTTPServer(mcpServer, WithEventStore(forwarder))

	type asked struct {
		cancel  context.CancelFunc
		session *streamableHttpSession
		scoped  *requestScopedSSE
		queue   chan<- *scopedRequest
	}
	askedCh := make(chan asked, 1)
	callErr := make(chan error, 1)
	mcpServer.AddTool(mcp.NewTool("ask"), func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session, _ := ClientSessionFromContext(ctx).(*streamableHttpSession)
		scoped, _ := ctx.Value(requestScopedSSEKey{}).(*requestScopedSSE)
		if session == nil || scoped == nil {
			return nil, errors.New("the tool call has no request-scoped stream")
		}
		// The forwarder takes this notification and stays parked recording it.
		if err := mcpServer.SendNotificationToClient(ctx, "notifications/message",
			map[string]any{"level": "info", "data": forwarderMarker}); err != nil {
			return nil, err
		}
		select {
		case <-forwarder.blocked:
		case <-time.After(5 * time.Second):
			return nil, errors.New("the forwarder never parked")
		}
		// The request keeps the POST's context values, so it is queued for
		// the POST's stream, but its cancellation is its own, so this handler
		// can return while it is pending.
		reqCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		// The request reaches the POST's queue through a relay that signals
		// once it is there, so the handler returns only after the enqueue.
		// trySend reads the queue field under sendMu, which covers the swap.
		queue := scoped.requests
		relay := make(chan *scopedRequest)
		queued := make(chan struct{})
		scoped.sendMu.Lock()
		scoped.requests = relay
		scoped.sendMu.Unlock()
		go func() {
			select {
			case item := <-relay:
				// The queue is empty and buffered, and the forwarder that
				// would take from it is parked, so this never waits.
				queue <- item
				close(queued)
			case <-reqCtx.Done():
			}
		}()
		go func() {
			_, err := mcpServer.RequestRoots(reqCtx, mcp.ListRootsRequest{})
			callErr <- err
		}()
		select {
		case <-queued:
		case <-time.After(5 * time.Second):
			cancel()
			return nil, errors.New("the request was never queued")
		}
		askedCh <- asked{cancel: cancel, session: session, scoped: scoped, queue: queue}
		return mcp.NewToolResultText("done"), nil
	})

	testServer := httptest.NewServer(httpServer)
	t.Cleanup(testServer.Close)
	sessionID := initializeSession(t, testServer.URL)

	// Release every gate even when an assertion fails, so no goroutine is
	// left blocked behind the test.
	releaseForwarder := sync.OnceFunc(func() { close(forwarder.release) })
	releaseRecord := sync.OnceFunc(func() { close(record.release) })
	t.Cleanup(releaseForwarder)
	t.Cleanup(releaseRecord)

	w := newGatedSSEWriter("") // an empty marker matches every write, so gate nothing:
	w.hit.Do(func() {})        // consume the gate so writes pass straight through
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

	got := requireWithin(t, askedCh, "the tool handler to queue the request")
	t.Cleanup(got.cancel)

	// The POST closed its stream while the forwarder is parked and the
	// request is still queued, untaken: it can only be settled late now.
	requireWithin(t, got.scoped.done, "the POST to close its stream")
	require.Len(t, got.queue, 1, "the request must still be queued when the stream closes")
	releaseForwarder()
	requireWithin(t, record.blocked, "the late settlement to block recording the request")

	got.cancel()
	require.ErrorIs(t, requireWithin(t, callErr, "the caller to return while the record is blocked"), context.Canceled)
	assert.Empty(t, got.session.notificationChannel,
		"the caller must not publish the cancellation where another stream could deliver it before the request")

	releaseRecord()
	requireWithin(t, handled, "the POST to finish after the release")

	// The POST's own stream, and the record a resuming client replays, both
	// carry the request followed by exactly one cancellation naming it.
	output := w.output()
	requireRequestThenOneCancellation(t, sseMessages(t, output))
	var firstEventID string
	for line := range strings.SplitSeq(output, "\n") {
		if id, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "id: "); ok {
			firstEventID = id
			break
		}
	}
	require.NotEmpty(t, firstEventID, "the POST's stream must carry event IDs")
	var replayed []json.RawMessage
	_, err = forwarder.ReplayEventsAfter(t.Context(), sessionID, firstEventID, func(_ string, message json.RawMessage) error {
		replayed = append(replayed, message)
		return nil
	})
	require.NoError(t, err)
	requireRequestThenOneCancellation(t, replayed)

	httpServer.pendingRequests.Range(func(key, _ any) bool {
		t.Errorf("request %v still pending", key)
		return true
	})
}
