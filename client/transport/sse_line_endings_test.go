package transport

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestStreamableHTTP_SendRequestWithSSELineEndings(t *testing.T) {
	tests := []struct {
		name    string
		endings [3]string
	}{
		{name: "LF", endings: [3]string{"\n", "\n", "\n"}},
		{name: "CRLF", endings: [3]string{"\r\n", "\r\n", "\r\n"}},
		{name: "CR", endings: [3]string{"\r", "\r", "\r"}},
		{name: "mixed", endings: [3]string{"\r", "\r\n", "\n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "event: message"+tt.endings[0]+"data: "+pingResult(0)+tt.endings[1]+tt.endings[2])
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			t.Cleanup(server.Close)

			c, err := NewStreamableHTTP(server.URL)
			require.NoError(t, err)
			t.Cleanup(func() { _ = c.Close() })

			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			response, err := c.SendRequest(ctx, JSONRPCRequest{
				JSONRPC: mcp.JSONRPC_VERSION,
				ID:      mcp.NewRequestId(int64(1)),
				Method:  "ping",
			})
			require.NoError(t, err, "a complete response event must be delivered while the stream remains open")
			require.NotNil(t, response)
			assert.Nil(t, response.Error)
			assert.JSONEq(t, `{"pad":""}`, string(response.Result))
		})
	}
}
