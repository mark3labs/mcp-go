package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestStreamableHTTP_RequestNotificationsStayOnThePOSTStream proves that
// notifications a tool handler sends reach the client on the POST stream of
// the call, ahead of its response, while a standalone GET stream of the same
// session is open. On the session's shared channel the GET stream's forwarder
// competed for them and could deliver them after the response.
func TestStreamableHTTP_RequestNotificationsStayOnThePOSTStream(t *testing.T) {
	const (
		iterations = 30
		perCall    = 5
		method     = "test/progress"
	)

	mcpServer := NewMCPServer("test-mcp-server", "1.0")
	mcpServer.AddTool(mcp.Tool{Name: "notifyMany"}, func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		srv := ServerFromContext(ctx)
		for i := range perCall {
			if err := srv.SendNotificationToClient(ctx, method, map[string]any{"n": i}); err != nil {
				return nil, err
			}
		}
		return mcp.NewToolResultText("ok"), nil
	})

	server := NewTestStreamableHTTPServer(mcpServer)
	defer server.Close()

	resp, err := postJSON(server.URL, initRequest)
	require.NoError(t, err, "initialize")
	sessionID := resp.Header.Get(HeaderKeySessionID)
	resp.Body.Close()
	require.NotEmpty(t, sessionID)

	getReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	getReq.Header.Set("Accept", "text/event-stream")
	getReq.Header.Set(HeaderKeySessionID, sessionID)
	getResp, err := server.Client().Do(getReq)
	require.NoError(t, err, "open the GET stream")
	defer getResp.Body.Close()
	require.Equal(t, http.StatusOK, getResp.StatusCode)
	var getBody bytes.Buffer
	getRead := make(chan struct{})
	go func() {
		defer close(getRead)
		_, _ = io.Copy(&getBody, getResp.Body)
	}()

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": "notifyMany"},
	})
	require.NoError(t, err)

	for i := range iterations {
		req, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set(HeaderKeySessionID, sessionID)

		resp, err := server.Client().Do(req)
		require.NoErrorf(t, err, "iteration %d", i)
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		require.NoErrorf(t, err, "iteration %d: read body", i)

		assert.Equalf(t, perCall, countNotifications(t, string(raw), method),
			"iteration %d: every notification of the call belongs on its POST stream", i)
	}

	getResp.Body.Close()
	<-getRead
	assert.Zero(t, countNotifications(t, getBody.String(), method),
		"the standalone GET stream must not carry a call's notifications")
}

// TestRequestScopedSSE_NotificationForAnotherSession proves that a
// notification addressed to a session other than the one the POST serves is
// not written to that POST's stream.
func TestRequestScopedSSE_NotificationForAnotherSession(t *testing.T) {
	notifications := make(chan mcp.JSONRPCNotification, 1)
	served := fakeSession{sessionID: "served", notificationChannel: make(chan mcp.JSONRPCNotification, 1), initialized: true}
	other := fakeSession{sessionID: "other", notificationChannel: make(chan mcp.JSONRPCNotification, 1), initialized: true}
	ctx := context.WithValue(t.Context(), requestScopedSSEKey{}, &requestScopedSSE{
		session:       served,
		notifications: notifications,
		done:          make(chan struct{}),
		register:      func() {},
	})

	require.NoError(t, NewMCPServer("test", "1.0").SendElicitationComplete(ctx, other, "elicitation-1"))

	assert.Empty(t, notifications, "the served POST's stream must not carry another session's notification")
	assert.Len(t, other.notificationChannel, 1, "the notification goes to the session it is addressed to")
}

// TestRequestScopedSSE_NoSendLandsAfterClose proves that once close returns,
// no send reports success and every send that did is already queued, so the
// drain that follows close sees all of them.
func TestRequestScopedSSE_NoSendLandsAfterClose(t *testing.T) {
	const (
		iterations = 500
		senders    = 8
	)
	for range iterations {
		notifications := make(chan mcp.JSONRPCNotification, senders)
		scoped := &requestScopedSSE{notifications: notifications, done: make(chan struct{}), register: func() {}}

		var (
			wg   sync.WaitGroup
			sent atomic.Int32
		)
		for range senders {
			wg.Go(func() {
				if scoped.trySendNotification(t.Context(), mcp.JSONRPCNotification{}) {
					sent.Add(1)
				}
			})
		}
		scoped.close()
		queuedAtClose := len(notifications)
		wg.Wait()

		require.Equal(t, queuedAtClose, len(notifications), "a send landed after close")
		require.Equal(t, int(sent.Load()), queuedAtClose, "a send reported success without being queued before close")
	}
}

// TestRequestScopedSSE_CloseDoesNotWaitOnABlockedSend proves that close
// returns while a send that cannot be cancelled is blocked on a full channel
// nobody reads, and that the send then reports failure.
func TestRequestScopedSSE_CloseDoesNotWaitOnABlockedSend(t *testing.T) {
	notifications := make(chan mcp.JSONRPCNotification, 1)
	notifications <- mcp.JSONRPCNotification{}
	scoped := &requestScopedSSE{notifications: notifications, done: make(chan struct{}), register: func() {}}

	sent := make(chan bool, 1)
	go func() {
		sent <- scoped.trySendNotification(context.WithoutCancel(t.Context()), mcp.JSONRPCNotification{})
	}()
	require.Eventually(t, func() bool {
		if !scoped.sendMu.TryLock() {
			return true
		}
		scoped.sendMu.Unlock()
		return false
	}, time.Second, time.Millisecond, "the send never blocked")

	closed := make(chan struct{})
	go func() {
		scoped.close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("close waited on a send blocked on a full channel")
	}
	assert.False(t, <-sent, "a send that never queued must report failure")
}

// TestStreamableHTTP_NotificationHandlerNotificationsReachThePOSTStream proves
// that notifications a client-notification handler sends are written to the
// POST that carried the client notification, including those still queued
// when the forwarder stops.
func TestStreamableHTTP_NotificationHandlerNotificationsReachThePOSTStream(t *testing.T) {
	const (
		iterations = 30
		perCall    = 5
		trigger    = "notifications/test"
		method     = "test/progress"
	)

	mcpServer := NewMCPServer("test-mcp-server", "1.0")
	mcpServer.AddNotificationHandler(trigger, func(ctx context.Context, _ mcp.JSONRPCNotification) {
		srv := ServerFromContext(ctx)
		for i := range perCall {
			_ = srv.SendNotificationToClient(ctx, method, map[string]any{"n": i})
		}
	})

	server := NewTestStreamableHTTPServer(mcpServer)
	defer server.Close()

	resp, err := postJSON(server.URL, initRequest)
	require.NoError(t, err, "initialize")
	sessionID := resp.Header.Get(HeaderKeySessionID)
	resp.Body.Close()
	require.NotEmpty(t, sessionID)

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  trigger,
	})
	require.NoError(t, err)

	for i := range iterations {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, bytes.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set(HeaderKeySessionID, sessionID)

		resp, err := server.Client().Do(req)
		require.NoErrorf(t, err, "iteration %d", i)
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		require.NoErrorf(t, err, "iteration %d: read body", i)

		assert.Equalf(t, perCall, countNotifications(t, string(raw), method),
			"iteration %d: every notification of the handler belongs on its POST stream", i)
	}
}
