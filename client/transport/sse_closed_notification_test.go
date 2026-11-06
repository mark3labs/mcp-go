package transport

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestSSENotificationAfterClose(t *testing.T) {
	tests := []struct {
		name  string
		start bool
	}{
		{"before start", false},
		{"after start", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var posts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts.Add(1)
					w.WriteHeader(http.StatusAccepted)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "event: endpoint\ndata: /messages\n\n")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			t.Cleanup(srv.Close)
			tr, err := NewSSE(srv.URL, WithEndpointTimeout(time.Second))
			require.NoError(t, err)
			t.Cleanup(func() { _ = tr.Close() })
			notification := mcp.JSONRPCNotification{
				JSONRPC:      mcp.JSONRPC_VERSION,
				Notification: mcp.Notification{Method: "notifications/roots/list_changed"},
			}
			var wantPosts int32
			if tt.start {
				require.NoError(t, tr.Start(t.Context()))
				require.NoError(t, tr.SendNotification(t.Context(), notification))
				wantPosts = 1
				require.Equal(t, wantPosts, posts.Load())
			}
			require.NoError(t, tr.Close())
			assert.ErrorIs(t, tr.SendNotification(t.Context(), notification), ErrTransportClosed)
			assert.Equal(t, wantPosts, posts.Load(), "closed transport must not POST notifications")
		})
	}
}
