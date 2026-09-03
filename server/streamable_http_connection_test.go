package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func initializeLegacySession(t *testing.T, endpoint string) string {
	t.Helper()
	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": mcp.ProtocolVersion20250326,
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "connection-test",
				"version": "1.0.0",
			},
		},
	}
	resp, err := postJSON(endpoint, request)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	sessionID := resp.Header.Get(HeaderKeySessionID)
	require.NotEmpty(t, sessionID)
	return sessionID
}

func openListeningGet(t *testing.T, endpoint, sessionID string) (*http.Response, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set(HeaderKeySessionID, sessionID)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp, cancel
}

func activeGetDone(t *testing.T, transport *StreamableHTTPServer, sessionID string) <-chan struct{} {
	t.Helper()
	value, ok := transport.activeGetConnections.Load(sessionID)
	require.True(t, ok)
	return value.(*activeGetConnection).done
}

func requireConnectionClosed(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("listening GET handler did not exit")
	}
}

func TestStreamableHTTPListeningConnectionLifecycle(t *testing.T) {
	var unregisterCalls atomic.Int32
	var unregisterContextCanceled atomic.Bool
	hooks := &Hooks{}
	hooks.AddOnUnregisterSession(func(ctx context.Context, _ ClientSession) {
		unregisterCalls.Add(1)
		unregisterContextCanceled.Store(ctx.Err() != nil)
	})
	mcpServer := NewMCPServer("connection-test", "1.0.0", WithHooks(hooks))
	transport := NewStreamableHTTPServer(mcpServer, WithStateful(true))
	ts := httptest.NewServer(transport)
	defer ts.Close()

	sessionID := initializeLegacySession(t, ts.URL)
	first, cancelFirst := openListeningGet(t, ts.URL, sessionID)
	require.Equal(t, http.StatusOK, first.StatusCode)
	firstDone := activeGetDone(t, transport, sessionID)

	duplicateReq, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	require.NoError(t, err)
	duplicateReq.Header.Set("Accept", "text/event-stream")
	duplicateReq.Header.Set(HeaderKeySessionID, sessionID)
	duplicate, err := http.DefaultClient.Do(duplicateReq)
	require.NoError(t, err)
	duplicate.Body.Close()
	require.Equal(t, http.StatusConflict, duplicate.StatusCode)

	cancelFirst()
	first.Body.Close()
	requireConnectionClosed(t, firstDone)
	_, stillRegistered := transport.activeGetConnections.Load(sessionID)
	assert.False(t, stillRegistered)

	reconnected, cancelReconnect := openListeningGet(t, ts.URL, sessionID)
	require.Equal(t, http.StatusOK, reconnected.StatusCode)
	reconnectDone := activeGetDone(t, transport, sessionID)

	deleteReq, err := http.NewRequest(http.MethodDelete, ts.URL, nil)
	require.NoError(t, err)
	deleteReq.Header.Set(HeaderKeySessionID, sessionID)
	deleted, err := http.DefaultClient.Do(deleteReq)
	require.NoError(t, err)
	deleted.Body.Close()
	require.Equal(t, http.StatusOK, deleted.StatusCode)
	requireConnectionClosed(t, reconnectDone)
	cancelReconnect()
	reconnected.Body.Close()
	assert.Equal(t, int32(1), unregisterCalls.Load())
	assert.False(t, unregisterContextCanceled.Load())

	staleReq, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	require.NoError(t, err)
	staleReq.Header.Set("Accept", "text/event-stream")
	staleReq.Header.Set(HeaderKeySessionID, sessionID)
	stale, err := http.DefaultClient.Do(staleReq)
	require.NoError(t, err)
	defer stale.Body.Close()
	assert.Equal(t, http.StatusNotFound, stale.StatusCode)
}

func TestStreamableHTTPHeartbeatStopsWithListeningConnection(t *testing.T) {
	mcpServer := NewMCPServer("heartbeat-test", "1.0.0")
	transport := NewStreamableHTTPServer(mcpServer,
		WithStateful(true),
		WithHeartbeatInterval(5*time.Millisecond),
	)
	ts := httptest.NewServer(transport)
	defer ts.Close()

	sessionID := initializeLegacySession(t, ts.URL)
	stream, cancel := openListeningGet(t, ts.URL, sessionID)
	require.Equal(t, http.StatusOK, stream.StatusCode)
	done := activeGetDone(t, transport, sessionID)

	reader := bufio.NewReader(stream.Body)
	var payload string
	for payload == "" {
		line, err := reader.ReadString('\n')
		require.NoError(t, err)
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			payload = strings.TrimSpace(data)
		}
	}
	var heartbeat mcp.JSONRPCRequest
	require.NoError(t, json.Unmarshal([]byte(payload), &heartbeat))
	assert.Equal(t, string(mcp.MethodPing), heartbeat.Method)

	cancel()
	require.NoError(t, stream.Body.Close())
	requireConnectionClosed(t, done)
	_, stillRegistered := transport.activeGetConnections.Load(sessionID)
	assert.False(t, stillRegistered)
}

func TestStreamableHTTPDisconnectUnregistersWithLiveContext(t *testing.T) {
	var unregisterCalls atomic.Int32
	var unregisterContextCanceled atomic.Bool
	hooks := &Hooks{}
	hooks.AddOnUnregisterSession(func(ctx context.Context, _ ClientSession) {
		unregisterCalls.Add(1)
		unregisterContextCanceled.Store(ctx.Err() != nil)
	})
	manager := &InsecureStatefulSessionIdManager{}
	transport := NewStreamableHTTPServer(
		NewMCPServer("disconnect-cleanup-test", "1.0.0", WithHooks(hooks)),
		WithSessionIdManager(manager),
	)
	ts := httptest.NewServer(transport)
	defer ts.Close()

	sessionID := manager.Generate()
	stream, cancel := openListeningGet(t, ts.URL, sessionID)
	require.Equal(t, http.StatusOK, stream.StatusCode)
	done := activeGetDone(t, transport, sessionID)

	cancel()
	stream.Body.Close()
	requireConnectionClosed(t, done)
	assert.Equal(t, int32(1), unregisterCalls.Load())
	assert.False(t, unregisterContextCanceled.Load())
}

func TestStreamableHTTPInvalidListeningSessionReturnsNotFound(t *testing.T) {
	transport := NewStreamableHTTPServer(NewMCPServer("invalid-test", "1.0.0"), WithStateful(true))
	ts := httptest.NewServer(transport)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set(HeaderKeySessionID, "not-a-session")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

type blockingValidateSessionManager struct {
	manager                *InsecureStatefulSessionIdManager
	blockNext              atomic.Bool
	validateReturned       atomic.Bool
	terminateAfterValidate atomic.Bool
	validateEntered        chan struct{}
	continueValidate       chan struct{}
	terminateEntered       chan struct{}
	terminateOnce          sync.Once
}

func (m *blockingValidateSessionManager) Generate() string {
	return m.manager.Generate()
}

func (m *blockingValidateSessionManager) Validate(sessionID string) (bool, error) {
	if m.blockNext.CompareAndSwap(true, false) {
		close(m.validateEntered)
		<-m.continueValidate
	}
	terminated, err := m.manager.Validate(sessionID)
	m.validateReturned.Store(true)
	return terminated, err
}

func (m *blockingValidateSessionManager) Terminate(sessionID string) (bool, error) {
	m.terminateAfterValidate.Store(m.validateReturned.Load())
	m.terminateOnce.Do(func() {
		close(m.terminateEntered)
	})
	return m.manager.Terminate(sessionID)
}

type secondLockSignaler struct {
	delegate      sessionLifecycleLocker
	attempts      atomic.Int32
	secondAttempt chan struct{}
	signalOnce    sync.Once
}

func (l *secondLockSignaler) lock(sessionID string) func() {
	if l.attempts.Add(1) == 2 {
		l.signalOnce.Do(func() {
			close(l.secondAttempt)
		})
	}
	return l.delegate.lock(sessionID)
}

func TestStreamableHTTPDeleteSerializesWithListeningRegistration(t *testing.T) {
	manager := &blockingValidateSessionManager{
		manager:          &InsecureStatefulSessionIdManager{},
		validateEntered:  make(chan struct{}),
		continueValidate: make(chan struct{}),
		terminateEntered: make(chan struct{}),
	}
	transport := NewStreamableHTTPServer(
		NewMCPServer("lifecycle-race-test", "1.0.0"),
		WithSessionIdManager(manager),
	)
	lifecycleLock := &secondLockSignaler{
		delegate:      newSessionLifecycleLocks(),
		secondAttempt: make(chan struct{}),
	}
	transport.sessionLifecycle = lifecycleLock

	sessionID := manager.Generate()
	manager.blockNext.Store(true)

	getWriter := newFlushableHTTPResponseWriter()
	getDone := make(chan struct{})
	go func() {
		defer close(getDone)
		transport.Handle(getWriter, &HTTPRequest{
			Method: http.MethodGet,
			Header: http.Header{
				"Accept":           []string{"text/event-stream"},
				HeaderKeySessionID: []string{sessionID},
			},
			Context: t.Context(),
		})
	}()

	select {
	case <-manager.validateEntered:
	case <-time.After(time.Second):
		t.Fatal("listening GET did not reach session validation")
	}

	deleteWriter := newBufferingHTTPResponseWriter()
	deleteDone := make(chan struct{})
	go func() {
		defer close(deleteDone)
		transport.Handle(deleteWriter, &HTTPRequest{
			Method: http.MethodDelete,
			Header: http.Header{
				HeaderKeySessionID: []string{sessionID},
			},
			Context: t.Context(),
		})
	}()

	select {
	case <-lifecycleLock.secondAttempt:
	case <-time.After(time.Second):
		t.Fatal("DELETE did not attempt to enter the held lifecycle lock")
	}
	select {
	case <-manager.terminateEntered:
		t.Fatal("DELETE entered Terminate before GET validation returned")
	default:
	}
	close(manager.continueValidate)

	select {
	case <-getDone:
	case <-time.After(time.Second):
		t.Fatal("listening GET did not exit after DELETE")
	}
	select {
	case <-deleteDone:
	case <-time.After(time.Second):
		t.Fatal("DELETE did not complete")
	}
	getWriter.mu.Lock()
	getStatus := getWriter.status
	getWriter.mu.Unlock()
	require.Equal(t, http.StatusOK, getStatus)
	deleteWriter.mu.Lock()
	deleteStatus := deleteWriter.status
	deleteWriter.mu.Unlock()
	require.Equal(t, http.StatusOK, deleteStatus)
	select {
	case <-manager.terminateEntered:
	case <-time.After(time.Second):
		t.Fatal("DELETE did not enter Terminate after GET validation returned")
	}
	assert.True(t, manager.terminateAfterValidate.Load())

	_, getStillActive := transport.activeGetConnections.Load(sessionID)
	assert.False(t, getStillActive)
	_, sessionStillActive := transport.activeSessions.Load(sessionID)
	assert.False(t, sessionStillActive)

	staleWriter := newFlushableHTTPResponseWriter()
	transport.Handle(staleWriter, &HTTPRequest{
		Method: http.MethodGet,
		Header: http.Header{
			"Accept":           []string{"text/event-stream"},
			HeaderKeySessionID: []string{sessionID},
		},
		Context: t.Context(),
	})
	staleWriter.mu.Lock()
	staleStatus := staleWriter.status
	staleWriter.mu.Unlock()
	assert.Equal(t, http.StatusNotFound, staleStatus)
}

func TestSessionLifecycleLocksDoNotBlockOtherSessions(t *testing.T) {
	locks := newSessionLifecycleLocks()
	unlockFirst := locks.lock("first")
	defer unlockFirst()

	secondCompleted := make(chan struct{})
	go func() {
		unlockSecond := locks.lock("second")
		unlockSecond()
		close(secondCompleted)
	}()

	select {
	case <-secondCompleted:
	case <-time.After(time.Second):
		t.Fatal("one session's lifecycle lock blocked a different session")
	}
}

type blockingStreamResponseWriter struct {
	header        http.Header
	writeStarted  chan struct{}
	writeReleased chan struct{}
	writeOnce     sync.Once
	releaseOnce   sync.Once
}

func newBlockingStreamResponseWriter() *blockingStreamResponseWriter {
	return &blockingStreamResponseWriter{
		header:        make(http.Header),
		writeStarted:  make(chan struct{}),
		writeReleased: make(chan struct{}),
	}
}

func (w *blockingStreamResponseWriter) Header() http.Header {
	return w.header
}

func (w *blockingStreamResponseWriter) WriteHeader(int) {}

func (w *blockingStreamResponseWriter) Write([]byte) (int, error) {
	w.writeOnce.Do(func() {
		close(w.writeStarted)
	})
	<-w.writeReleased
	return 0, context.DeadlineExceeded
}

func (w *blockingStreamResponseWriter) Flush() {}

func (w *blockingStreamResponseWriter) CanStream() bool {
	return true
}

func (w *blockingStreamResponseWriter) SetWriteDeadline(deadline time.Time) error {
	if !deadline.IsZero() {
		w.releaseOnce.Do(func() {
			close(w.writeReleased)
		})
	}
	return nil
}

func TestStreamableHTTPCleanupInterruptsBlockedWrite(t *testing.T) {
	manager := &InsecureStatefulSessionIdManager{}
	transport := NewStreamableHTTPServer(
		NewMCPServer("blocked-write-test", "1.0.0"),
		WithSessionIdManager(manager),
		WithHeartbeatInterval(time.Millisecond),
	)
	sessionID := manager.Generate()
	w := newBlockingStreamResponseWriter()
	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		transport.Handle(w, &HTTPRequest{
			Method: http.MethodGet,
			Header: http.Header{
				"Accept":           []string{"text/event-stream"},
				HeaderKeySessionID: []string{sessionID},
			},
			Context: t.Context(),
		})
	}()

	select {
	case <-w.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not reach the blocked SSE write")
	}

	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)
		transport.cleanupSessionState(context.WithoutCancel(t.Context()), sessionID)
	}()

	select {
	case <-cleanupDone:
	case <-time.After(time.Second):
		t.Fatal("session cleanup did not interrupt the blocked SSE write")
	}
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("listening handler did not exit after its write was interrupted")
	}
}
