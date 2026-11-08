package transport

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestStreamableHTTPStartAfterClose(t *testing.T) {
	tests := []struct {
		name             string
		startBeforeClose bool
		continuous       bool
	}{
		{name: "close before first start"},
		{name: "close after successful start", startBeforeClose: true},
		{name: "close before first start with continuous listening", continuous: true},
		{name: "close after successful start with continuous listening", startBeforeClose: true, continuous: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requestCount atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount.Add(1)
				if r.Method == http.MethodGet {
					w.Header().Set("Content-Type", "text/event-stream")
					flusher, ok := w.(http.Flusher)
					if !ok {
						http.Error(w, "streaming unsupported", http.StatusInternalServerError)
						return
					}
					flusher.Flush()
					<-r.Context().Done()
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
			}))
			defer server.Close()

			var opts []StreamableHTTPCOption
			if tt.continuous {
				opts = append(opts, WithContinuousListening())
			}
			trans, err := NewStreamableHTTP(server.URL, opts...)
			require.NoError(t, err)

			if tt.startBeforeClose {
				require.NoError(t, trans.Start(t.Context()))
				_, err = trans.SendRequest(t.Context(), JSONRPCRequest{
					JSONRPC: "2.0",
					ID:      mcp.NewRequestId(int64(1)),
					Method:  "ping",
				})
				require.NoError(t, err)
			}
			require.NoError(t, trans.Close())

			hitsBeforeRestart := requestCount.Load()
			err = trans.Start(t.Context())
			require.ErrorIs(t, err, ErrTransportClosed)

			_, err = trans.SendRequest(t.Context(), JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      mcp.NewRequestId(int64(2)),
				Method:  "ping",
			})
			require.ErrorIs(t, err, ErrTransportClosed)

			err = trans.SendNotification(t.Context(), mcp.JSONRPCNotification{
				JSONRPC: "2.0",
				Notification: mcp.Notification{
					Method: "notifications/cancelled",
				},
			})
			require.ErrorIs(t, err, ErrTransportClosed)
			require.Equal(t, hitsBeforeRestart, requestCount.Load())
		})
	}
}
