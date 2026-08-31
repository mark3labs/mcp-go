package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestStreamableHTTP_NotificationNotDroppedWithoutEventStore pins that a
// notification sent from a tool handler always reaches the POST response,
// whether or not an event store is configured.
//
// The forwarder goroutine and the response path race for it. The forwarder
// takes a notification off session.notificationChannel and THEN acquires mu;
// meanwhile the response path acquires mu, runs its drain loop — which finds
// the channel empty, because the forwarder already took the value — closes
// done, and unlocks. The forwarder then acquires mu, sees done closed, and
// returns. The notification it is holding is discarded: it is no longer in the
// channel, so the drain loop could not have rescued it either.
//
// The resumable path is immune because it does `close(done); <-forwarderExited`
// before draining, leaving exactly one consumer. Without an event store the
// forwarder is never awaited, so the race is live and costs roughly half of all
// notifications.
//
// One notification per call, so the assertion is exact rather than statistical.
// Iterating converts a coin flip into a near-certain failure when the bug is
// present: a single run reproduces only ~50% of the time, which is why the
// existing TestStreamableHTTP_DrainNotifications — which sends 10 and only
// logs informationally when fewer than 5 survive — did not catch it.
func TestStreamableHTTP_NotificationNotDroppedWithoutEventStore(t *testing.T) {
	const iterations = 30

	run := func(t *testing.T, opts ...StreamableHTTPOption) (delivered, total int) {
		t.Helper()

		mcpServer := NewMCPServer("test-mcp-server", "1.0")
		mcpServer.AddTool(mcp.Tool{Name: "notifyOnce"}, func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if srv := ServerFromContext(ctx); srv != nil {
				_ = srv.SendNotificationToClient(ctx, "test/single", map[string]any{"n": 1})
			}
			return mcp.NewToolResultText("ok"), nil
		})

		server := NewTestStreamableHTTPServer(mcpServer, opts...)
		defer server.Close()

		resp, err := postJSON(server.URL, initRequest)
		if err != nil {
			t.Fatalf("initialize: %v", err)
		}
		sessionID := resp.Header.Get(HeaderKeySessionID)
		resp.Body.Close()

		body, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params":  map[string]any{"name": "notifyOnce"},
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		for i := 0; i < iterations; i++ {
			req, err := http.NewRequest("POST", server.URL, bytes.NewReader(body))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			if sessionID != "" {
				req.Header.Set(HeaderKeySessionID, sessionID)
			}

			resp, err := server.Client().Do(req)
			if err != nil {
				t.Fatalf("iteration %d: %v", i, err)
			}
			raw, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatalf("iteration %d: read body: %v", i, err)
			}

			total++
			if strings.Contains(string(raw), "test/single") {
				delivered++
			}
		}
		return delivered, total
	}

	t.Run("without event store", func(t *testing.T) {
		delivered, total := run(t)
		if delivered != total {
			t.Errorf("notification delivered on %d/%d responses; every response must carry it "+
				"(the forwarder discarded the rest after taking them off the channel)",
				delivered, total)
		}
	})

	t.Run("with event store", func(t *testing.T) {
		delivered, total := run(t, WithEventStore(NewInMemoryEventStore()))
		if delivered != total {
			t.Errorf("notification delivered on %d/%d responses with an event store", delivered, total)
		}
	})
}
