package transport

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestSSE_StartAfterClose ensures that calling Start after Close returns an
// error instead of silently reporting success without establishing a new
// connection. Previously Start only checked c.started (which remains true
// after Close), so a post-Close Start call returned nil while SendRequest
// on the same transport would fail with "transport has been closed",
// producing contradictory state.
func TestSSE_StartAfterClose(t *testing.T) {
	var requests atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "event: endpoint\ndata: %s/messages\n\n", srv.URL)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	transport, err := NewSSE(srv.URL)
	if err != nil {
		t.Fatalf("NewSSE failed: %v", err)
	}

	ctx := context.Background()
	if err := transport.Start(ctx); err != nil {
		t.Fatalf("first Start failed: %v", err)
	}

	if err := transport.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	requestsBeforeSecondStart := requests.Load()

	err = transport.Start(ctx)
	if err == nil {
		t.Fatal("expected Start after Close to return an error, got nil")
	}

	if requests.Load() != requestsBeforeSecondStart {
		t.Fatal("expected Start after Close to not issue a new HTTP request")
	}
}
