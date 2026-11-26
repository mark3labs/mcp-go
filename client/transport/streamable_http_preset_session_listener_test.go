package transport

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// TestStreamableHTTP_PresetSessionStartsContinuousListener verifies that a
// transport that adopts a session with WithSession opens the GET stream on
// Start. Such a session is not initialized again, so no initialize request
// opens the stream.
func TestStreamableHTTP_PresetSessionStartsContinuousListener(t *testing.T) {
	const sessionID = "preset-session"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "unexpected method", http.StatusBadRequest)
			return
		}
		if r.Header.Get(HeaderKeySessionID) != sessionID {
			http.Error(w, "invalid session ID", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"test/notification\"}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	trans, err := NewStreamableHTTP(server.URL, WithSession(sessionID), WithContinuousListening())
	require.NoError(t, err)
	defer trans.Close()

	received := make(chan string, 1)
	trans.SetNotificationHandler(func(notification mcp.JSONRPCNotification) {
		select {
		case received <- notification.Method:
		default:
		}
	})

	require.NoError(t, trans.Start(t.Context()))
	// Start stays idempotent.
	require.NoError(t, trans.Start(t.Context()))

	select {
	case method := <-received:
		require.Equal(t, "test/notification", method)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a notification on the GET stream of the preset session")
	}
}
