package transport

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSSE_Start_RetryAfterFailure verifies a failed Start can be retried and
// the retry completes with its own handshake instead of stale state.
func TestSSE_Start_RetryAfterFailure(t *testing.T) {
	var requests atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			// First attempt fails before a stream is established.
			http.Error(w, "temporary failure", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "event: endpoint\ndata: %s/messages\n\n", server.URL)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	transport, err := NewSSE(server.URL, WithEndpointTimeout(2*time.Second))
	require.NoError(t, err)
	defer transport.Close()

	require.Error(t, transport.Start(t.Context()))
	require.NoError(t, transport.Start(t.Context()))
	require.Equal(t, int32(2), requests.Load())
}

// TestSSE_HandleSSEEvent_PerAttemptEndpointIsolation verifies each Start
// attempt owns its endpoint channel: closing one attempt's channel (e.g. by a
// delayed event from a superseded reader) must not satisfy a different
// attempt's wait, and duplicate endpoint events on the same attempt must not
// panic.
func TestSSE_HandleSSEEvent_PerAttemptEndpointIsolation(t *testing.T) {
	sse := newBareSSE()
	const endpointData = "http://127.0.0.1:1/messages"

	first := make(chan struct{})
	firstOnce := &sync.Once{}
	sse.handleSSEEvent("endpoint", endpointData, first, firstOnce)
	select {
	case <-first:
	default:
		t.Fatal("first attempt's channel was not closed")
	}

	second := make(chan struct{})
	secondOnce := &sync.Once{}
	sse.handleSSEEvent("endpoint", endpointData, second, secondOnce)
	select {
	case <-second:
	default:
		t.Fatal("second attempt's channel was not closed")
	}

	// A third attempt that has not seen an endpoint yet must stay blocked.
	third := make(chan struct{})
	select {
	case <-third:
		t.Fatal("third attempt was satisfied by a superseded reader's event")
	default:
	}

	// A proxy re-sending the endpoint for the same attempt must not panic.
	require.NotPanics(t, func() {
		sse.handleSSEEvent("endpoint", endpointData, first, firstOnce)
	})
}

// TestSSE_Start_ConcurrentSingleStream verifies overlapping Start calls are
// serialized: exactly one HTTP stream is opened and both callers succeed.
func TestSSE_Start_ConcurrentSingleStream(t *testing.T) {
	var requests atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "event: endpoint\ndata: %s/messages\n\n", server.URL)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	transport, err := NewSSE(server.URL, WithEndpointTimeout(2*time.Second))
	require.NoError(t, err)
	defer transport.Close()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = transport.Start(t.Context())
		}(i)
	}
	wg.Wait()

	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, int32(1), requests.Load(), "overlapping Start opened multiple streams")
}

// TestSSE_ReadSSE_PendingEndpointAtEOF verifies an endpoint event buffered
// before EOF is processed as a pending event before the reader exits.
func TestSSE_ReadSSE_PendingEndpointAtEOF(t *testing.T) {
	sse := newBareSSE()
	ch := make(chan struct{})
	once := &sync.Once{}
	// No trailing blank line: the endpoint event is still buffered when the
	// reader hits EOF and must be processed as a pending event.
	body := "event: endpoint\ndata: http://127.0.0.1:1/messages\n"
	sse.readSSE(io.NopCloser(strings.NewReader(body)), ch, once)
	select {
	case <-ch:
	default:
		t.Fatal("pending endpoint event was not processed on EOF")
	}
}

// TestSSE_Start_FollowerCancelsIndependently verifies a concurrent Start
// waiting for the owner's in-flight handshake honors its own context and
// cancels without disturbing the owner.
func TestSSE_Start_FollowerCancelsIndependently(t *testing.T) {
	releaseEndpoint := make(chan struct{})
	var requests atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-releaseEndpoint
		fmt.Fprintf(w, "event: endpoint\ndata: %s/messages\n\n", server.URL)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	transport, err := NewSSE(server.URL, WithEndpointTimeout(5*time.Second))
	require.NoError(t, err)
	defer transport.Close()

	// Owner: blocks waiting for the endpoint.
	ownerDone := make(chan error, 1)
	go func() { ownerDone <- transport.Start(t.Context()) }()

	// Wait until the owner registered its in-flight handshake.
	require.Eventually(t, func() bool {
		transport.startMu.Lock()
		defer transport.startMu.Unlock()
		return transport.startDone != nil
	}, time.Second, 5*time.Millisecond)

	// Follower: waits on the shared handshake but cancels its own context.
	followerCtx, cancel := context.WithCancel(t.Context())
	followerDone := make(chan error, 1)
	go func() { followerDone <- transport.Start(followerCtx) }()
	time.Sleep(20 * time.Millisecond) // let the follower reach the wait
	cancel()

	select {
	case err := <-followerDone:
		require.Error(t, err)
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("follower did not honor its own context cancellation")
	}

	// Release the endpoint: the owner still completes successfully.
	close(releaseEndpoint)
	select {
	case err := <-ownerDone:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("owner did not complete after the endpoint was sent")
	}

	require.Equal(t, int32(1), requests.Load(), "concurrent Start opened extra streams")
}

// TestSSE_Start_FollowerContextCancellation verifies cancelling the caller's
// context while waiting for the endpoint aborts Start with the context error.
func TestSSE_Start_FollowerContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		// Never send the endpoint; wait for the client to give up.
		<-r.Context().Done()
	}))
	defer server.Close()

	transport, err := NewSSE(server.URL, WithEndpointTimeout(10*time.Second))
	require.NoError(t, err)
	defer transport.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancelled := make(chan struct{})
	go func() {
		<-time.After(100 * time.Millisecond)
		cancel()
		close(cancelled)
	}()

	err = transport.Start(ctx)
	<-cancelled

	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	require.Contains(t, err.Error(), "context cancelled while waiting for endpoint")
}
