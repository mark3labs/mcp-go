package transport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestStreamableHTTPStartAfterClose(t *testing.T) {
	tests := []struct {
		name             string
		startBeforeClose bool
	}{
		{name: "close before first start"},
		{name: "close after successful start", startBeforeClose: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requestCount atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
			}))
			defer server.Close()

			trans, err := NewStreamableHTTP(server.URL)
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
			require.ErrorIs(t, trans.Start(t.Context()), ErrTransportClosed)

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

func TestStreamableHTTPStartAfterClose_ContinuousListening(t *testing.T) {
	getOpened := make(chan struct{}, 1)
	getDone := make(chan struct{})
	var getDoneOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			select {
			case getOpened <- struct{}{}:
			default:
			}
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "streaming unsupported", http.StatusInternalServerError)
				return
			}
			flusher.Flush()
			<-r.Context().Done()
			getDoneOnce.Do(func() { close(getDone) })
			return
		}

		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&message)
		if message.Method == string(mcp.MethodInitialize) {
			// Continuous listening keeps the standalone GET stream, which only
			// exists on legacy protocol versions.
			w.Header().Set(HeaderKeySessionID, "session-1")
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w,
				`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{},"serverInfo":{"name":"s","version":"1"}}}`,
				message.ID, mcp.ProtocolVersion20251125,
			)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	trans, err := NewStreamableHTTP(server.URL, WithContinuousListening())
	require.NoError(t, err)
	require.NoError(t, trans.Start(t.Context()))

	response, err := trans.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(1)),
		Method:  string(mcp.MethodInitialize),
	})
	require.NoError(t, err)
	require.Nil(t, response.Error)

	select {
	case <-getOpened:
	case <-time.After(5 * time.Second):
		t.Fatal("GET listener did not open after initialize")
	}

	require.NoError(t, trans.Close())

	select {
	case <-getDone:
	case <-time.After(5 * time.Second):
		t.Fatal("GET listener did not terminate after Close")
	}

	require.ErrorIs(t, trans.Start(t.Context()), ErrTransportClosed)

	_, err = trans.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(int64(2)),
		Method:  "ping",
	})
	require.ErrorIs(t, err, ErrTransportClosed)
}

func TestStreamableHTTPCloseInterruptsInFlightSends(t *testing.T) {
	tests := []struct {
		name string
		send func(*testing.T, *StreamableHTTP) error
	}{
		{
			name: "SendRequest",
			send: func(t *testing.T, trans *StreamableHTTP) error {
				t.Helper()
				_, err := trans.SendRequest(t.Context(), JSONRPCRequest{
					JSONRPC: "2.0",
					ID:      mcp.NewRequestId(int64(1)),
					Method:  "ping",
				})
				return err
			},
		},
		{
			name: "SendNotification",
			send: func(t *testing.T, trans *StreamableHTTP) error {
				t.Helper()
				return trans.SendNotification(t.Context(), mcp.JSONRPCNotification{
					JSONRPC: "2.0",
					Notification: mcp.Notification{
						Method: "notifications/cancelled",
					},
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entered := make(chan struct{})
			httpClient := &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					select {
					case <-entered:
					default:
						close(entered)
					}
					<-req.Context().Done()
					return nil, req.Context().Err()
				}),
			}
			trans, err := NewStreamableHTTP("http://example.com/mcp", WithHTTPBasicClient(httpClient))
			require.NoError(t, err)
			require.NoError(t, trans.Start(t.Context()))

			errCh := make(chan error, 1)
			go func() {
				errCh <- tt.send(t, trans)
			}()

			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("send did not reach the HTTP client")
			}

			require.NoError(t, trans.Close())

			select {
			case err := <-errCh:
				require.ErrorIs(t, err, ErrTransportClosed)
			case <-time.After(5 * time.Second):
				t.Fatal("in-flight send did not return after Close")
			}
		})
	}
}
