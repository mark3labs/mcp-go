package e2e

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
)

func TestE2E_HTTPSubscriptionGracefulClose(t *testing.T) {
	tests := []struct {
		name     string
		shutdown bool
	}{
		{name: "Shutdown", shutdown: true},
		{name: "CloseSessions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core := server.NewMCPServer("test", "1", server.WithToolCapabilities(true))
			ts := httptest.NewUnstartedServer(nil)
			handler := server.NewStreamableHTTPServer(core, server.WithStreamableHTTPServer(ts.Config))
			ts.Config.Handler = handler
			ts.Start()
			t.Cleanup(ts.Close)
			c, _ := startHTTPClient(t, ts.URL)
			require.Equal(t, mcp.ProtocolVersion20260728, c.ProtocolVersion())

			acknowledged := make(chan struct{}, 1)
			c.OnNotification(func(n mcp.JSONRPCNotification) {
				if n.Method == mcp.MethodNotificationSubscriptionsAcknowledged {
					acknowledged <- struct{}{}
				}
			})
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			listenDone := make(chan error, 1)
			go func() {
				defer close(listenDone)
				listenDone <- c.Listen(ctx, mcp.SubscriptionFilter{ToolsListChanged: true})
			}()
			t.Cleanup(func() {
				cancel()
				select {
				case <-listenDone:
				case <-time.After(time.Second):
					t.Error("subscription did not stop")
				}
			})
			select {
			case <-acknowledged:
			case <-ctx.Done():
				t.Fatal("subscription was not acknowledged")
			}

			closeCtx, stop := context.WithTimeout(ctx, time.Second)
			defer stop()
			if tt.shutdown {
				require.NoError(t, handler.Shutdown(closeCtx))
			} else {
				handler.CloseSessions(closeCtx)
			}
			select {
			case err := <-listenDone:
				require.NoError(t, err, "server-initiated closure must complete the listen request")
			case <-closeCtx.Done():
				t.Fatal("subscription outlived server-initiated closure")
			}
			require.NoError(t, ctx.Err(), "the client should not need to cancel its request")
		})
	}
}
