package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

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
	go func() { _, _ = io.Copy(io.Discard, getResp.Body) }()

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
}
