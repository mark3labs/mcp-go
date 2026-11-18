package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestSSEServerStartAndHandlersRejectAfterShutdown(t *testing.T) {
	mcpServer := NewMCPServer("test", "1.0.0")
	var notes atomic.Int32
	mcpServer.AddNotificationHandler("notifications/initialized", func(context.Context, mcp.JSONRPCNotification) {
		notes.Add(1)
	})
	sseServer := NewSSEServer(mcpServer, WithHTTPServer(&http.Server{Addr: "127.0.0.1:0"}))

	require.NoError(t, sseServer.Shutdown(t.Context()))
	require.ErrorIs(t, sseServer.Start("127.0.0.1:0"), http.ErrServerClosed)

	getReq := httptest.NewRequest(http.MethodGet, "/sse", nil)
	getRec := httptest.NewRecorder()
	sseServer.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusServiceUnavailable, getRec.Code)

	postReq := httptest.NewRequest(http.MethodPost, "/message?sessionId=missing", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	postRec := httptest.NewRecorder()
	sseServer.ServeHTTP(postRec, postReq)
	require.Equal(t, http.StatusServiceUnavailable, postRec.Code)
	require.Zero(t, notes.Load(), "a shut down server must not handle messages")
}

func TestSSEServerShutdownBeforeHTTPServerIsNotTerminal(t *testing.T) {
	sseServer := NewSSEServer(NewMCPServer("test", "1.0.0"))
	require.NoError(t, sseServer.Shutdown(t.Context()))

	ctx, cancel := context.WithCancel(t.Context())
	req := httptest.NewRequest(http.MethodGet, "/sse", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		sseServer.ServeHTTP(rec, req)
		close(done)
	}()

	select {
	case <-done:
		t.Fatalf("handler returned before cancel: status %d body %q", rec.Code, rec.Body.String())
	case <-time.After(50 * time.Millisecond):
		// The handler is serving the stream, so Shutdown did not close it.
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE handler did not return after the request was canceled")
	}
}

func TestSSEServerRejectsMessageForClosedSession(t *testing.T) {
	mcpServer := NewMCPServer("test", "1.0.0")
	var notes atomic.Int32
	mcpServer.AddNotificationHandler("notifications/initialized", func(context.Context, mcp.JSONRPCNotification) {
		notes.Add(1)
	})
	sseServer := NewSSEServer(mcpServer)
	session := &sseSession{
		done:                make(chan struct{}),
		eventQueue:          make(chan string, 1),
		sessionID:           "sess",
		notificationChannel: make(chan mcp.JSONRPCNotification, 1),
	}
	close(session.done)
	sseServer.sessions.Store(session.sessionID, session)

	req := httptest.NewRequest(
		http.MethodPost,
		"/message?sessionId=sess",
		strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`),
	)
	rec := httptest.NewRecorder()
	sseServer.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "session closed")
	require.Zero(t, notes.Load(), "a closed session must not handle messages")
}

func TestSSEServerShutdownDuringRegistration(t *testing.T) {
	hooks := &Hooks{}
	var sseServer *SSEServer
	hooks.AddOnRegisterSession(func(context.Context, ClientSession) {
		require.NoError(t, sseServer.Shutdown(t.Context()))
	})
	sseServer = NewSSEServer(
		NewMCPServer("test", "1.0.0", WithHooks(hooks)),
		WithHTTPServer(&http.Server{}),
	)

	rec := httptest.NewRecorder()
	sseServer.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sse", nil))

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
	require.Equal(t, "server closed\n", rec.Body.String())
	sseServer.sessions.Range(func(_, _ any) bool {
		t.Error("shutdown left an SSE session registered")
		return true
	})
}
