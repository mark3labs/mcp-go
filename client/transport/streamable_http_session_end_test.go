package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forgetfulServer is a legacy Streamable HTTP server that can forget its
// sessions, as a server does when it restarts. It answers initialize with a
// new session, 404 to a request carrying a session it doesn't know, and 400
// to a request without a session, as the TypeScript SDK's server does. It
// keeps a log of what it gets.
type forgetfulServer struct {
	acceptUnknownGET bool // open a GET stream on any session, as mcp-go's server does
	getNotFound      bool // answer every GET with 404

	mu             sync.Mutex
	failInitialize bool          // answer initialize with a JSON-RPC error
	holdInitialize chan struct{} // stream the initialize response, and hold its event until this is closed
	holdGET        chan struct{} // hold the 404 for a GET on a session the server doesn't know until this is closed
	heldGET        chan string   // gets the session of each GET held
	session        string
	sessions       int
	log            []string
	streams        []chan struct{}
	opened         chan string // gets the session of each GET stream that opens
}

func newForgetfulServer() *forgetfulServer {
	return &forgetfulServer{opened: make(chan string, 16)}
}

func (s *forgetfulServer) record(entry string) {
	s.mu.Lock()
	s.log = append(s.log, entry)
	s.mu.Unlock()
}

func (s *forgetfulServer) entries() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.log)
}

// restart forgets every session and ends the open GET streams.
func (s *forgetfulServer) restart() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session = ""
	for _, stream := range s.streams {
		close(stream)
	}
	s.streams = nil
}

func (s *forgetfulServer) setFailInitialize(fail bool) {
	s.mu.Lock()
	s.failInitialize = fail
	s.mu.Unlock()
}

// holdInitializeEvent makes the server stream the next initialize responses
// and hold their event until release is called.
func (s *forgetfulServer) holdInitializeEvent() (release func()) {
	hold := make(chan struct{})
	s.mu.Lock()
	s.holdInitialize = hold
	s.mu.Unlock()
	return func() { close(hold) }
}

// holdUnknownGETs makes the server hold the 404 for a GET on a session it
// doesn't know until release is called. held gets the session of each GET
// held.
func (s *forgetfulServer) holdUnknownGETs() (held <-chan string, release func()) {
	hold, heldGET := make(chan struct{}), make(chan string, 4)
	s.mu.Lock()
	s.holdGET, s.heldGET = hold, heldGET
	s.mu.Unlock()
	return heldGET, func() { close(hold) }
}

func (s *forgetfulServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	session := r.Header.Get(HeaderKeySessionID)
	if r.Method == http.MethodGet {
		s.record("GET " + session)
		s.mu.Lock()
		known := session != "" && (session == s.session || s.acceptUnknownGET)
		hold, heldGET := s.holdGET, s.heldGET
		s.mu.Unlock()
		if !known && hold != nil {
			heldGET <- session
			<-hold
		}
		if s.getNotFound || !known {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		stream := make(chan struct{})
		s.mu.Lock()
		s.streams = append(s.streams, stream)
		s.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		s.opened <- session
		select {
		case <-stream:
		case <-r.Context().Done():
		}
		return
	}

	var message struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	_ = json.NewDecoder(r.Body).Decode(&message)
	s.record(fmt.Sprintf("%s %s", message.Method, session))

	s.mu.Lock()
	if message.Method == string(mcp.MethodInitialize) {
		if s.failInitialize {
			s.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"not now"}}`, message.ID)
			return
		}
		s.sessions++
		s.session = fmt.Sprintf("s%d", s.sessions)
		session = s.session
		hold := s.holdInitialize
		s.mu.Unlock()
		result := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{},"serverInfo":{"name":"s","version":"1"}}}`,
			message.ID, mcp.ProtocolVersion20251125)
		w.Header().Set(HeaderKeySessionID, session)
		if hold != nil {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			select {
			case <-hold:
			case <-r.Context().Done():
				return
			}
			_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", result)
			w.(http.Flusher).Flush()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, result)
		return
	}
	known := session == s.session
	s.mu.Unlock()
	switch {
	case session == "":
		http.Error(w, "Bad Request: Mcp-Session-Id header is required", http.StatusBadRequest)
	case !known:
		http.Error(w, "Session not found", http.StatusNotFound)
	case len(message.ID) == 0:
		w.WriteHeader(http.StatusAccepted)
	default:
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"tools":[]}}`, message.ID)
	}
}

// recordingHandler is a slog.Handler that keeps the messages logged.
type recordingHandler struct {
	mu       sync.Mutex
	messages []string
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.messages = append(h.messages, r.Message)
	h.mu.Unlock()
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordingHandler) logged() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.messages)
}

// connectToForgetful connects a transport to server, with continuous listening
// if listen is set, and initializes it.
func connectToForgetful(t *testing.T, server http.Handler, listen bool, options ...StreamableHTTPCOption) *StreamableHTTP {
	t.Helper()
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	if listen {
		options = append(options, WithContinuousListening())
	}
	trans, err := NewStreamableHTTP(httpServer.URL, options...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = trans.Close() })
	require.NoError(t, trans.Start(t.Context()))
	initializeOn(t, trans)
	return trans
}

// initializeOn runs the initialize handshake on trans.
func initializeOn(t *testing.T, trans *StreamableHTTP) {
	t.Helper()
	response, err := trans.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(1)),
		Method:  string(mcp.MethodInitialize),
	})
	require.NoError(t, err)
	require.Nil(t, response.Error)
	require.NoError(t, trans.SendNotification(t.Context(), mcp.JSONRPCNotification{
		JSONRPC:      mcp.JSONRPC_VERSION,
		Notification: mcp.Notification{Method: string(mcp.MethodNotificationInitialized)},
	}))
}

func listToolsOn(trans *StreamableHTTP) error {
	response, err := trans.SendRequest(context.Background(), JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(2)),
		Method:  string(mcp.MethodToolsList),
	})
	if err == nil && response.Error != nil {
		err = response.Error.AsError()
	}
	return err
}

// waitForStream waits for a GET stream to open, and returns its session.
func (s *forgetfulServer) waitForStream(t *testing.T) string {
	t.Helper()
	select {
	case session := <-s.opened:
		return session
	case <-time.After(5 * time.Second):
		t.Fatalf("no GET stream opened; the server got %v", s.entries())
		return ""
	}
}

// count returns how many times the server has logged entry.
func (s *forgetfulServer) count(entry string) int {
	n := 0
	for _, e := range s.entries() {
		if e == entry {
			n++
		}
	}
	return n
}

// waitForSessionEnd waits until the transport has dropped the session.
func waitForSessionEnd(t *testing.T, trans *StreamableHTTP) {
	t.Helper()
	require.Eventually(t, func() bool { return trans.GetSessionId() == "" }, 5*time.Second, time.Millisecond, "the session never ended")
}

// Once the GET stream finds the session gone, requests fail with
// ErrSessionTerminated until the caller re-initializes, rather than go out
// without a session ID. The GET stream is the first to find out when the
// server restarts, as it reconnects on its own.
func TestStreamableHTTP_ReportsAnEndedSessionUntilReinitialized(t *testing.T) {
	server := newForgetfulServer()
	trans := connectToForgetful(t, server, true)
	server.waitForStream(t)

	server.restart()
	waitForSessionEnd(t, trans) // the GET stream reconnected and got 404
	require.Equal(t, 2, server.count("GET s1"))

	err := listToolsOn(trans)
	assert.ErrorIs(t, err, ErrSessionTerminated)
	err = trans.SendNotification(t.Context(), mcp.JSONRPCNotification{
		JSONRPC:      mcp.JSONRPC_VERSION,
		Notification: mcp.Notification{Method: "notifications/roots/list_changed"},
	})
	assert.ErrorIs(t, err, ErrSessionTerminated)
	for _, entry := range server.entries() {
		assert.NotEqual(t, "tools/list ", entry, "a request went out without a session ID")
		assert.NotEqual(t, "notifications/roots/list_changed ", entry, "a notification went out without a session ID")
	}

	initializeOn(t, trans)
	require.NoError(t, listToolsOn(trans))
	assert.Contains(t, server.entries(), "tools/list s2")
}

// A failed initialize doesn't start a new session, so requests keep failing
// with ErrSessionTerminated.
func TestStreamableHTTP_FailedReinitializationKeepsTheSessionEnded(t *testing.T) {
	server := newForgetfulServer()
	trans := connectToForgetful(t, server, false)

	server.restart()
	require.ErrorIs(t, listToolsOn(trans), ErrSessionTerminated)

	server.setFailInitialize(true)
	response, err := trans.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(3)),
		Method:  string(mcp.MethodInitialize),
	})
	require.NoError(t, err)
	require.NotNil(t, response.Error)
	assert.ErrorIs(t, listToolsOn(trans), ErrSessionTerminated)

	server.setFailInitialize(false)
	initializeOn(t, trans)
	assert.NoError(t, listToolsOn(trans))
}

// After the caller re-initializes, the GET stream listens on the new session.
func TestStreamableHTTP_ListenerResumesOnANewSession(t *testing.T) {
	server := newForgetfulServer()
	logs := &recordingHandler{}
	trans := connectToForgetful(t, server, true, WithLogger(slog.New(logs)))
	require.Equal(t, "s1", server.waitForStream(t))

	server.restart()
	waitForSessionEnd(t, trans)
	require.ErrorIs(t, listToolsOn(trans), ErrSessionTerminated)
	time.Sleep(50 * time.Millisecond)

	initializeOn(t, trans)
	assert.Equal(t, "s2", server.waitForStream(t))
	// The listener waited for the new session rather than trying again.
	waits := 0
	for _, message := range logs.logged() {
		if strings.Contains(message, "listening again") {
			waits++
		}
	}
	assert.Equal(t, 1, waits, "logged: %v", logs.logged())
}

// A server may accept a GET for a session it doesn't know, as mcp-go's does,
// so after a restart the stream reconnects on the forgotten session. When
// the caller re-initializes, the stream moves to the new session.
func TestStreamableHTTP_ListenerLeavesAForgottenSession(t *testing.T) {
	server := newForgetfulServer()
	server.acceptUnknownGET = true
	logs := &recordingHandler{}
	trans := connectToForgetful(t, server, true, WithLogger(slog.New(logs)))
	require.Equal(t, "s1", server.waitForStream(t))

	server.restart()
	require.Equal(t, "s1", server.waitForStream(t)) // reconnected on the forgotten session
	require.ErrorIs(t, listToolsOn(trans), ErrSessionTerminated)

	before := len(logs.logged())
	initializeOn(t, trans)
	assert.Equal(t, "s2", server.waitForStream(t))
	// Moving to the new session isn't a failure to listen.
	assert.Empty(t, logs.logged()[before:])
}

// A server that answers GET with 404 from the start doesn't offer the stream
// (it should answer 405), so the listener stops, and the session goes on.
// Starting new sessions won't change that, so the listener doesn't try again
// after the caller re-initializes.
func TestStreamableHTTP_ListenerStopsWhenTheStreamNeverOpened(t *testing.T) {
	server := newForgetfulServer()
	server.getNotFound = true
	trans := connectToForgetful(t, server, true)
	require.Eventually(t, func() bool { return server.count("GET s1") == 1 }, 5*time.Second, time.Millisecond)

	require.NoError(t, listToolsOn(trans))
	assert.Contains(t, server.entries(), "tools/list s1")
	initializeOn(t, trans)
	require.NoError(t, listToolsOn(trans))
	time.Sleep(200 * time.Millisecond)
	gets := server.count("GET s1") + server.count("GET s2") + server.count("GET ")
	assert.Equal(t, 1, gets, "the server got %v", server.entries())
}

// A server without sessions that answers GET with 404 doesn't offer the
// stream either: the listener stops, and requests go on.
func TestStreamableHTTP_ListenerStopsOnASessionlessServer(t *testing.T) {
	var gets atomic.Int32
	server := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets.Add(1)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&message)
		if len(message.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if message.Method == string(mcp.MethodInitialize) {
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{},"serverInfo":{"name":"s","version":"1"}}}`,
				message.ID, mcp.ProtocolVersion20251125)
			return
		}
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"tools":[]}}`, message.ID)
	})
	trans := connectToForgetful(t, server, true)

	require.Eventually(t, func() bool { return gets.Load() == 1 }, 5*time.Second, time.Millisecond)
	require.NoError(t, listToolsOn(trans))
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, int32(1), gets.Load())
}

// An initialize response streamed over SSE starts the session only when its
// event arrives. If the session ends before that, it stays ended.
func TestStreamableHTTP_StreamedInitializeAfterTheSessionEnded(t *testing.T) {
	server := newForgetfulServer()
	release := server.holdInitializeEvent()
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	trans, err := NewStreamableHTTP(httpServer.URL)
	require.NoError(t, err)
	defer trans.Close()

	initialized := make(chan error, 1)
	go func() {
		_, err := trans.SendRequest(t.Context(), JSONRPCRequest{
			JSONRPC: mcp.JSONRPC_VERSION,
			ID:      mcp.NewRequestId(int64(1)),
			Method:  string(mcp.MethodInitialize),
		})
		initialized <- err
	}()
	require.Eventually(t, func() bool { return trans.GetSessionId() == "s1" }, 5*time.Second, time.Millisecond)

	// While the response is still coming, the server forgets the session.
	server.restart()
	err = trans.SendNotification(t.Context(), mcp.JSONRPCNotification{
		JSONRPC:      mcp.JSONRPC_VERSION,
		Notification: mcp.Notification{Method: string(mcp.MethodNotificationInitialized)},
	})
	require.ErrorIs(t, err, ErrSessionTerminated)
	release()
	require.NoError(t, <-initialized)

	assert.ErrorIs(t, listToolsOn(trans), ErrSessionTerminated)
	assert.NotContains(t, server.entries(), "tools/list ", "a request went out without a session ID")
}

// A 404 without a session ID says nothing about a session, so requests keep
// going out.
func TestStreamableHTTP_NotFoundWithoutASession(t *testing.T) {
	var calls int
	var mu sync.Mutex
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var message struct {
			ID json.RawMessage `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&message)
		mu.Lock()
		calls++
		first := calls == 1
		mu.Unlock()
		if first {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"tools":[]}}`, message.ID)
	}))
	defer httpServer.Close()
	trans, err := NewStreamableHTTP(httpServer.URL)
	require.NoError(t, err)
	defer trans.Close()

	assert.ErrorIs(t, listToolsOn(trans), ErrSessionTerminated)
	assert.NoError(t, listToolsOn(trans))
}

// A response to a server's request isn't sent on an ended session either.
func TestStreamableHTTP_NoResponseOnAnEndedSession(t *testing.T) {
	server := newForgetfulServer()
	trans := connectToForgetful(t, server, false)
	server.restart()
	require.ErrorIs(t, listToolsOn(trans), ErrSessionTerminated)

	trans.sendResponseToServer(t.Context(), &JSONRPCResponse{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(7)),
		Result:  json.RawMessage(`{}`),
	})
	// A response has no method, so the server logs it as " " and the session.
	assert.Zero(t, server.count(" "), "the response went out: %v", server.entries())
}

// If the caller re-initializes before the GET stream's 404 for the old
// session arrives, as when its own request failed with a connection error
// during the restart, the stream moves to the new session.
func TestStreamableHTTP_ListenerFollowsAReinitializationBeforeThe404(t *testing.T) {
	server := newForgetfulServer()
	logs := &recordingHandler{}
	trans := connectToForgetful(t, server, true, WithLogger(slog.New(logs)))
	require.Equal(t, "s1", server.waitForStream(t))

	held, releaseGET := server.holdUnknownGETs()
	server.restart()
	select {
	case session := <-held:
		require.Equal(t, "s1", session)
	case <-time.After(5 * time.Second):
		t.Fatal("the GET stream didn't reconnect")
	}

	// Re-initialize, and hold the response once the new session ID is in.
	releaseInitialize := server.holdInitializeEvent()
	initialized := make(chan error, 1)
	go func() {
		_, err := trans.SendRequest(t.Context(), JSONRPCRequest{
			JSONRPC: mcp.JSONRPC_VERSION,
			ID:      mcp.NewRequestId(int64(1)),
			Method:  string(mcp.MethodInitialize),
		})
		initialized <- err
	}()
	require.Eventually(t, func() bool { return trans.GetSessionId() == "s2" }, 5*time.Second, time.Millisecond)

	releaseGET() // the GET on s1 gets its 404
	assert.Equal(t, "s2", server.waitForStream(t))
	releaseInitialize()
	require.NoError(t, <-initialized)
	for _, message := range logs.logged() {
		assert.NotContains(t, message, "stopping listener")
	}
}

// server/discover starts a new connection only on 2026-07-28. On an earlier
// version it doesn't go out after the session has ended.
func TestStreamableHTTP_DiscoverOnALegacyConnectionAfterTheSessionEnded(t *testing.T) {
	server := newForgetfulServer()
	trans := connectToForgetful(t, server, false)
	trans.SetProtocolVersion(mcp.ProtocolVersion20251125)
	server.restart()
	require.ErrorIs(t, listToolsOn(trans), ErrSessionTerminated)

	_, err := trans.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(3)),
		Method:  string(mcp.MethodServerDiscover),
	})
	assert.ErrorIs(t, err, ErrSessionTerminated)
	assert.NotContains(t, server.entries(), "server/discover ", "server/discover went out without a session ID")
}

// On 2026-07-28 no session ID is sent, so a 404 says nothing about the
// session ID the transport may still hold, and requests go on.
func TestStreamableHTTP_NotFoundOnTheModernProtocol(t *testing.T) {
	var calls atomic.Int32
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var message struct {
			ID json.RawMessage `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&message)
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"tools":[]}}`, message.ID)
	}))
	defer httpServer.Close()
	trans, err := NewStreamableHTTP(httpServer.URL, WithSession("s1"))
	require.NoError(t, err)
	defer trans.Close()
	trans.SetProtocolVersion(mcp.ProtocolVersion20260728)

	assert.ErrorIs(t, listToolsOn(trans), ErrSessionTerminated)
	assert.NoError(t, listToolsOn(trans))
}
