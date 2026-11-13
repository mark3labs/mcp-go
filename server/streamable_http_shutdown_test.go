package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestModernProtocol_ShutdownClosesListenStreams(t *testing.T) {
	mcpServer := NewMCPServer("test", "1", WithToolCapabilities(true))
	ts := httptest.NewUnstartedServer(nil)
	handler := NewStreamableHTTPServer(mcpServer, WithStreamableHTTPServer(ts.Config))
	ts.Config.Handler = handler
	ts.Start()
	t.Cleanup(ts.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)

	streams := make([]<-chan listenMessage, 3)
	for i := range streams {
		// Modern requests have no session ID; independent streams may reuse an RPC ID.
		streams[i] = openListenStream(ctx, t, ts.URL, 7, map[string]any{"toolsListChanged": true})
		require.Equal(t, mcp.MethodNotificationSubscriptionsAcknowledged, nextListenMessage(t, streams[i]).Method)
	}

	shutdownCtx, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	require.NoError(t, handler.Shutdown(shutdownCtx))
	for _, stream := range streams {
		requireListenStreamClosed(t, shutdownCtx, stream)
	}
}

func TestModernProtocol_CloseSessionsClosesListenStreams(t *testing.T) {
	mcpServer := NewMCPServer("test", "1", WithToolCapabilities(true))
	handler := NewStreamableHTTPServer(mcpServer)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)

	stream := openListenStream(ctx, t, ts.URL, 7, map[string]any{"toolsListChanged": true})
	require.Equal(t, mcp.MethodNotificationSubscriptionsAcknowledged, nextListenMessage(t, stream).Method)
	handler.CloseSessions(ctx)
	handler.CloseSessions(ctx)
	closeCtx, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	requireListenStreamClosed(t, closeCtx, stream)

	// Closing current streams must leave an externally owned HTTP server usable.
	stream = openListenStream(ctx, t, ts.URL, 7, map[string]any{"toolsListChanged": true})
	require.Equal(t, mcp.MethodNotificationSubscriptionsAcknowledged, nextListenMessage(t, stream).Method)
	mcpServer.AddTool(mcp.NewTool("added"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	})
	require.Equal(t, mcp.MethodNotificationToolsListChanged, nextListenMessage(t, stream).Method)
	handler.CloseSessions(ctx)
	requireListenStreamClosed(t, closeCtx, stream)
}

func TestModernProtocol_ShutdownDrainsStartingListenStream(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	mcpServer := NewMCPServer("test", "1", WithToolCapabilities(true))
	ts := httptest.NewUnstartedServer(nil)
	handler := NewStreamableHTTPServer(mcpServer,
		WithStreamableHTTPServer(ts.Config),
		WithHTTPContextFunc(func(ctx context.Context, _ *http.Request) context.Context {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
			}
			return ctx
		}),
	)
	ts.Config.Handler = handler
	shutdownStarted := make(chan struct{})
	ts.Config.RegisterOnShutdown(func() { close(shutdownStarted) })
	ts.Start()
	t.Cleanup(ts.Close)
	t.Cleanup(cancel)

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      7,
		"method":  mcp.MethodSubscriptionsListen,
		"params": map[string]any{
			"notifications": map[string]any{"toolsListChanged": true},
			"_meta":         modernMeta(),
		},
	})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set(mcp.HeaderProtocolVersion, mcp.ProtocolVersion20260728)
	req.Header.Set(mcp.HeaderMethod, string(mcp.MethodSubscriptionsListen))
	requestDone := make(chan error, 1)
	go func() {
		resp, err := ts.Client().Do(req)
		if err == nil {
			_, err = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		requestDone <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("listen request did not enter the handler")
	}

	shutdownCtx, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- handler.Shutdown(shutdownCtx) }()
	select {
	case <-shutdownStarted:
	case <-ctx.Done():
		t.Fatal("HTTP server did not begin shutdown")
	}
	// Register the stream after Shutdown's first CloseSessions snapshot.
	close(release)
	require.NoError(t, <-shutdownDone)
	select {
	case err := <-requestDone:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("listen request outlived shutdown")
	}
}

func requireListenStreamClosed(t *testing.T, ctx context.Context, stream <-chan listenMessage) {
	t.Helper()
	for {
		select {
		case _, open := <-stream:
			if !open {
				return
			}
		case <-ctx.Done():
			t.Fatal("subscription stream stayed open after closing sessions")
		}
	}
}
