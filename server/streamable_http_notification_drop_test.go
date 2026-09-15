package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

// countNotifications returns how many SSE data frames in body are JSON-RPC
// notifications for the given method. Counting frames rather than substring
// matching keeps the assertion exact: a response carrying the notification
// twice is as wrong as one carrying it not at all.
func countNotifications(t *testing.T, body, method string) int {
	t.Helper()

	n := 0
	for _, line := range strings.Split(body, "\n") {
		data, ok := strings.CutPrefix(strings.TrimSpace(line), "data:")
		if !ok {
			continue
		}
		var frame struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &frame); err != nil {
			// Not every frame is JSON-RPC we care about; skip quietly.
			continue
		}
		if frame.Method == method {
			n++
		}
	}
	return n
}

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
// existing TestStreamableHTTP_DrainNotifications — which sends 10 and only logs
// informationally when fewer than 5 survive — did not catch it.
func TestStreamableHTTP_NotificationNotDroppedWithoutEventStore(t *testing.T) {
	const (
		iterations = 30
		method     = "test/single"
	)

	tests := []struct {
		name string
		opts []StreamableHTTPOption
	}{
		{name: "without event store"},
		{name: "with event store", opts: []StreamableHTTPOption{WithEventStore(NewInMemoryEventStore())}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mcpServer := NewMCPServer("test-mcp-server", "1.0")
			mcpServer.AddTool(mcp.Tool{Name: "notifyOnce"}, func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				if srv := ServerFromContext(ctx); srv != nil {
					_ = srv.SendNotificationToClient(ctx, method, map[string]any{"n": 1})
				}
				return mcp.NewToolResultText("ok"), nil
			})

			server := NewTestStreamableHTTPServer(mcpServer, tt.opts...)
			defer server.Close()

			resp, err := postJSON(server.URL, initRequest)
			require.NoError(t, err, "initialize")
			sessionID := resp.Header.Get(HeaderKeySessionID)
			resp.Body.Close()

			body, err := json.Marshal(map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "tools/call",
				"params":  map[string]any{"name": "notifyOnce"},
			})
			require.NoError(t, err)

			delivered := 0
			for i := range iterations {
				req, err := http.NewRequest("POST", server.URL, bytes.NewReader(body))
				require.NoError(t, err)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", "application/json, text/event-stream")
				if sessionID != "" {
					req.Header.Set(HeaderKeySessionID, sessionID)
				}

				resp, err := server.Client().Do(req)
				require.NoErrorf(t, err, "iteration %d", i)
				raw, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				require.NoErrorf(t, err, "iteration %d: read body", i)

				got := countNotifications(t, string(raw), method)
				assert.Equalf(t, 1, got,
					"iteration %d: expected exactly one %s notification on the response, got %d",
					i, method, got)
				if got == 1 {
					delivered++
				}
			}

			assert.Equalf(t, iterations, delivered,
				"notification delivered on %d/%d responses; every response must carry exactly one "+
					"(the forwarder discarded the rest after taking them off the channel)",
				delivered, iterations)
		})
	}
}
