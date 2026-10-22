package transport

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestInProcessNotificationsCarryClientSession(t *testing.T) {
	s := server.NewMCPServer("test", "1")
	var got server.ClientSession
	s.AddNotificationHandler("notifications/roots/list_changed", func(ctx context.Context, _ mcp.JSONRPCNotification) { got = server.ClientSessionFromContext(ctx) })
	tr := NewInProcessTransport(s)
	require.NoError(t, tr.Start(t.Context()))
	defer tr.Close()
	require.NoError(t, tr.SendNotification(t.Context(), mcp.JSONRPCNotification{JSONRPC: "2.0", Notification: mcp.Notification{Method: "notifications/roots/list_changed"}}))
	require.NotNil(t, got)
	require.Equal(t, tr.sessionID, got.SessionID())
}
