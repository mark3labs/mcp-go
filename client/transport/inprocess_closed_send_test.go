package transport

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestInProcessSendAfterClose(t *testing.T) {
	tests := []struct {
		name  string
		start bool
	}{
		{"before start", false},
		{"after start", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := server.NewMCPServer("test", "1")
			calls, notifications := 0, 0
			s.AddTool(mcp.NewTool("count"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				calls++
				return mcp.NewToolResultText("done"), nil
			})
			s.AddNotificationHandler("notifications/roots/list_changed", func(context.Context, mcp.JSONRPCNotification) { notifications++ })
			tr := NewInProcessTransport(s)
			t.Cleanup(func() { _ = tr.Close() })
			if tt.start {
				require.NoError(t, tr.Start(t.Context()))
			}
			request := JSONRPCRequest{JSONRPC: "2.0", ID: mcp.NewRequestId(1), Method: "tools/call", Params: mcp.CallToolParams{Name: "count"}}
			notification := mcp.JSONRPCNotification{JSONRPC: "2.0", Notification: mcp.Notification{Method: "notifications/roots/list_changed"}}
			response, err := tr.SendRequest(t.Context(), request)
			require.NoError(t, err)
			require.Nil(t, response.Error)
			require.NoError(t, tr.SendNotification(t.Context(), notification))
			require.Equal(t, 1, calls)
			require.Equal(t, 1, notifications)
			require.NoError(t, tr.Close())

			response, err = tr.SendRequest(t.Context(), request)
			assert.ErrorIs(t, err, ErrTransportClosed)
			assert.Nil(t, response)
			assert.ErrorIs(t, tr.SendNotification(t.Context(), notification), ErrTransportClosed)
			assert.Equal(t, 1, calls, "closed transport must not invoke tool handlers")
			assert.Equal(t, 1, notifications, "closed transport must not invoke notification handlers")
		})
	}
}

func TestInProcessConcurrentStartAndSend(t *testing.T) {
	for range 20 {
		s := server.NewMCPServer("test", "1")
		tr := NewInProcessTransport(s)
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Go(func() {
			<-start
			assert.NoError(t, tr.Start(t.Context()))
		})
		wg.Go(func() {
			<-start
			for range 20 {
				_, err := tr.SendRequest(t.Context(), JSONRPCRequest{JSONRPC: "2.0", ID: mcp.NewRequestId(1), Method: "ping"})
				assert.NoError(t, err)
				assert.NoError(t, tr.SendNotification(t.Context(), mcp.JSONRPCNotification{JSONRPC: "2.0", Notification: mcp.Notification{Method: "notifications/roots/list_changed"}}))
			}
		})
		close(start)
		wg.Wait()
		require.NoError(t, tr.Close())
	}
}
