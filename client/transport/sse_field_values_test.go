package transport

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestSSEColonlessEvent(t *testing.T) {
	tests := []struct {
		name  string
		reset string
	}{
		{name: "with colon", reset: "event:"},
		{name: "without colon", reset: "event"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messageReceived := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					w.WriteHeader(http.StatusAccepted)
					close(messageReceived)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "event: endpoint\ndata: /message\n\n")
				w.(http.Flusher).Flush()
				select {
				case <-messageReceived:
					_, _ = fmt.Fprintf(w, "event: ignored\n%s\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n", tt.reset)
					w.(http.Flusher).Flush()
				case <-r.Context().Done():
					return
				}
				<-r.Context().Done()
			}))
			t.Cleanup(srv.Close)

			client, err := NewSSE(srv.URL, WithResponseTimeout(time.Second))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			require.NoError(t, client.Start(ctx))
			response, err := client.SendRequest(ctx, JSONRPCRequest{
				JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)), Method: "ping",
			})
			require.NoError(t, err)
			require.JSONEq(t, "{}", string(response.Result))
		})
	}
}

func TestStreamableHTTPColonlessID(t *testing.T) {
	tests := []struct {
		name  string
		reset string
	}{
		{name: "with colon", reset: "id:"},
		{name: "without colon", reset: "id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resumes atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				if r.Method == http.MethodGet {
					resumes.Add(1)
					_, _ = fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n")
					return
				}
				_, _ = fmt.Fprintf(w, "retry: 1\nid: old-cursor\n\n%s\n\n", tt.reset)
			}))
			t.Cleanup(srv.Close)
			client, err := NewStreamableHTTP(srv.URL)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, err = client.SendRequest(ctx, JSONRPCRequest{
				JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)), Method: "ping",
			})
			require.EqualError(t, err, "SSE stream ended without a response")
			require.Zero(t, resumes.Load())
		})
	}
}
