package transport

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pollingServer answers a POST with an SSE stream that sends only a priming
// event, with the given id and retry fields, and then ends the response
// before the JSON-RPC response, as the 2025-11-25 transport allows. Each GET
// that resumes the stream is recorded; the last of polls of them carries the
// response.
type pollingServer struct {
	postEvent string // the SSE fields the POST stream sends before ending
	abortPost bool   // break the POST stream off instead of ending it
	polls     int    // GETs until the response is ready
	getStatus int    // when set, every GET fails with this status

	mu        sync.Mutex
	closedAt  time.Time
	gets      []time.Time
	lastIDs   []string
	requestID json.RawMessage
}

func (s *pollingServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		body, _ := io.ReadAll(r.Body)
		var request struct {
			ID json.RawMessage `json:"id"`
		}
		_ = json.Unmarshal(body, &request)
		s.mu.Lock()
		s.requestID = request.ID
		s.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, s.postEvent)
		w.(http.Flusher).Flush()
		s.mu.Lock()
		s.closedAt = time.Now()
		s.mu.Unlock()
		if s.abortPost {
			panic(http.ErrAbortHandler)
		}
	case http.MethodGet:
		s.mu.Lock()
		s.gets = append(s.gets, time.Now())
		s.lastIDs = append(s.lastIDs, r.Header.Get("Last-Event-ID"))
		ready := len(s.gets) >= s.polls
		id := s.requestID
		s.mu.Unlock()
		if s.getStatus != 0 {
			w.WriteHeader(s.getStatus)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if !ready {
			// Nothing to send yet: ask for a longer wait and end again.
			_, _ = fmt.Fprint(w, "retry: 300\n\n")
			return
		}
		_, _ = fmt.Fprintf(w, "id: 2\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"ok\":true}}\n\n", id)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func sendThroughPollingServer(t *testing.T, server *pollingServer, protocolVersion string, options ...StreamableHTTPCOption) (*JSONRPCResponse, error) {
	t.Helper()
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	trans, err := NewStreamableHTTP(httpServer.URL, options...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = trans.Close() })
	require.NoError(t, trans.Start(t.Context()))
	trans.SetProtocolVersion(protocolVersion)
	return trans.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(1)),
		Method:  "tools/call",
	})
}

// Before protocol version 2026-07-28, a server may end an SSE stream before
// the response once it has sent an event ID, and the client then polls: it
// waits the retry time the server asked for and reconnects with a GET that
// carries Last-Event-ID (SEP-1699).
func TestStreamableHTTP_ResumesAnEndedSSEStream(t *testing.T) {
	server := &pollingServer{postEvent: "id: 1\nretry: 100\ndata:\n\n", polls: 2}

	response, err := sendThroughPollingServer(t, server, mcp.ProtocolVersion20251125)
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, string(response.Result))

	server.mu.Lock()
	defer server.mu.Unlock()
	require.Len(t, server.gets, 2)
	assert.Equal(t, []string{"1", "1"}, server.lastIDs)
	assert.GreaterOrEqual(t, server.gets[0].Sub(server.closedAt), 90*time.Millisecond, "the client must wait the retry time")
	assert.GreaterOrEqual(t, server.gets[1].Sub(server.gets[0]), 290*time.Millisecond, "a later retry field replaces the first")
}

// openBodies counts the GET response bodies the client hasn't closed yet,
// as each GET is sent.
type openBodies struct {
	mu        sync.Mutex
	open      int
	openAtGET []int
}

func (o *openBodies) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodGet {
		o.mu.Lock()
		o.openAtGET = append(o.openAtGET, o.open)
		o.mu.Unlock()
	}
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil || r.Method != http.MethodGet {
		return resp, err
	}
	o.mu.Lock()
	o.open++
	o.mu.Unlock()
	resp.Body = &countedBody{ReadCloser: resp.Body, owner: o}
	return resp, nil
}

type countedBody struct {
	io.ReadCloser
	owner *openBodies
	once  sync.Once
}

func (b *countedBody) Close() error {
	b.once.Do(func() {
		b.owner.mu.Lock()
		b.owner.open--
		b.owner.mu.Unlock()
	})
	return b.ReadCloser.Close()
}

// Each resumed connection is released when it ends, not when the request
// does, however long the client polls.
func TestStreamableHTTP_ReleasesEachResumedConnection(t *testing.T) {
	server := &pollingServer{postEvent: "id: 1\nretry: 10\ndata:\n\n", polls: 3}
	bodies := &openBodies{}

	_, err := sendThroughPollingServer(t, server, mcp.ProtocolVersion20251125, WithHTTPBasicClient(&http.Client{Transport: bodies}))
	require.NoError(t, err)

	bodies.mu.Lock()
	defer bodies.mu.Unlock()
	assert.Equal(t, []int{0, 0, 0}, bodies.openAtGET)
}

// A stream that breaks off, rather than being ended by the server, fails the
// call as before, and so does one whose last event was never finished: its
// id isn't taken as received.
func TestStreamableHTTP_ResumesOnlyFromFinishedEvents(t *testing.T) {
	t.Run("broken off", func(t *testing.T) {
		server := &pollingServer{postEvent: "id: 1\nretry: 10\ndata:\n\n", abortPost: true, polls: 1}
		_, err := sendThroughPollingServer(t, server, mcp.ProtocolVersion20251125)
		require.Error(t, err)
		server.mu.Lock()
		defer server.mu.Unlock()
		assert.Empty(t, server.gets)
	})
	t.Run("unfinished event", func(t *testing.T) {
		server := &pollingServer{postEvent: "id: 1\nretry: 10\ndata:\n\nid: 2\n", polls: 1}
		_, err := sendThroughPollingServer(t, server, mcp.ProtocolVersion20251125)
		require.NoError(t, err)
		server.mu.Lock()
		defer server.mu.Unlock()
		assert.Equal(t, []string{"1"}, server.lastIDs)
	})
}

// A server that asks for no wait at all is still not polled in a tight loop.
func TestStreamableHTTP_WaitsAMinimumBeforeResuming(t *testing.T) {
	server := &pollingServer{postEvent: "id: 1\nretry: 0\ndata:\n\n", polls: 1}
	_, err := sendThroughPollingServer(t, server, mcp.ProtocolVersion20251125)
	require.NoError(t, err)
	server.mu.Lock()
	defer server.mu.Unlock()
	require.Len(t, server.gets, 1)
	assert.GreaterOrEqual(t, server.gets[0].Sub(server.closedAt), minSSEReconnectDelay-time.Millisecond)
}

// Without an event ID there's nothing to resume from, and 2026-07-28 removed
// resumption altogether, so in both cases the call fails as before.
func TestStreamableHTTP_DoesNotResumeWithoutAnEventIDOrOnModernSessions(t *testing.T) {
	tests := []struct {
		name            string
		postEvent       string
		protocolVersion string
	}{
		{name: "no event ID", postEvent: "retry: 10\ndata:\n\n", protocolVersion: mcp.ProtocolVersion20251125},
		{name: "2026-07-28", postEvent: "id: 1\nretry: 10\ndata:\n\n", protocolVersion: mcp.ProtocolVersion20260728},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &pollingServer{postEvent: tt.postEvent, polls: 1}
			_, err := sendThroughPollingServer(t, server, tt.protocolVersion)
			require.Error(t, err)
			server.mu.Lock()
			defer server.mu.Unlock()
			assert.Empty(t, server.gets)
		})
	}
}

// A server that can't resume the stream makes the call fail with the reason.
func TestStreamableHTTP_ReportsAFailedResume(t *testing.T) {
	server := &pollingServer{postEvent: "id: 1\nretry: 10\ndata:\n\n", polls: 1, getStatus: http.StatusMethodNotAllowed}
	_, err := sendThroughPollingServer(t, server, mcp.ProtocolVersion20251125)
	require.ErrorContains(t, err, "failed to resume the SSE stream")
	require.ErrorContains(t, err, "405")
}
