package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSSEMCPClient_StartWithSSELineEndings(t *testing.T) {
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
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "event: endpoint"+tt.endings[0]+"data: /messages"+tt.endings[1]+tt.endings[2])
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			t.Cleanup(server.Close)

			c, err := NewSSEMCPClient(server.URL)
			require.NoError(t, err)
			t.Cleanup(func() { _ = c.Close() })

			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			require.NoError(t, c.Start(ctx), "a complete endpoint event must start the client while the stream remains open")
			assert.Equal(t, server.URL+"/messages", GetEndpoint(c).String())
		})
	}
}
