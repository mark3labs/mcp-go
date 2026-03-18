package server

// BenchmarkSessionRequestIDs_RetainedAfterClose measures memory retained in
// sessionRequestIDs after GET connections close without a DELETE request.
//
// This benchmark exists to detect regressions of the memory leak where
// sessionRequestIDs grew unboundedly because entries were only removed in
// handleDelete, which stateless clients rarely call.
//
// Run with:
//
//	go test ./server/ -run=^$ -bench=BenchmarkSessionRequestIDs -benchmem -v
//
// Expected output with the fix:
//
//	map_entries_retained    0        retained_heap_bytes/op   0
//
// Expected output without the fix (entries and bytes grow with b.N):
//
//	map_entries_retained   <b.N>    retained_heap_bytes/op  >0
//
// To reproduce the leak, comment out the fix in handleGet
// (streamable_http.go) and re-run this benchmark:
//
//	// defer s.sessionRequestIDs.Delete(sessionID)
//
// You should see map_entries_retained equal to b.N.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

// BenchmarkSessionRequestIDs_RetainedAfterClose measures heap objects retained
// in sessionRequestIDs after GET/SSE connections close without a DELETE request.
// map_entries_retained should be 0 with the fix and equal to b.N without it.
func BenchmarkSessionRequestIDs_RetainedAfterClose(b *testing.B) {
	mcpServer := NewMCPServer("bench", "1.0.0")
	httpServer := NewStreamableHTTPServer(mcpServer,
		// Short heartbeat so sessionRequestIDs is populated quickly.
		WithHeartbeatInterval(20*time.Millisecond),
	)
	ts := httptest.NewServer(httpServer)
	b.Cleanup(ts.Close)

	// Establish a heap baseline after warm-up GC.
	runtime.GC()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Open a GET (SSE) connection and close it without sending DELETE.
		ctx, cancel := context.WithCancel(context.Background())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL, nil)
		if err != nil {
			b.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "text/event-stream")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			b.Fatalf("GET failed: %v", err)
		}

		// Let the heartbeat goroutine tick at least once so that
		// nextRequestID populates sessionRequestIDs.
		time.Sleep(60 * time.Millisecond)

		cancel()
		_ = resp.Body.Close()

		// Give the server goroutine time to run the deferred cleanup.
		time.Sleep(30 * time.Millisecond)
	}

	b.StopTimer()

	// Force several GC cycles to reclaim anything that can be reclaimed.
	for i := 0; i < 5; i++ {
		runtime.GC()
	}

	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	// Count entries still live in the map.
	var retained int
	httpServer.sessionRequestIDs.Range(func(_, _ any) bool {
		retained++
		return true
	})

	// HeapInuse can decrease between GC cycles; clamp negative delta to 0.
	retainedBytes := int64(after.HeapInuse) - int64(before.HeapInuse)
	if retainedBytes < 0 {
		retainedBytes = 0
	}

	b.ReportMetric(float64(retained), "map_entries_retained")
	b.ReportMetric(float64(retainedBytes)/float64(b.N), "retained_heap_bytes/op")
}
