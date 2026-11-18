package transport

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSSELeadingBOM(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
	}{
		{name: "without BOM"},
		{name: "with BOM", prefix: "\ufeff"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "%sevent: endpoint\ndata: /message\n\n", tt.prefix)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			t.Cleanup(server.Close)
			client, err := NewSSE(server.URL, WithEndpointTimeout(time.Second))
			require.NoError(t, err)
			t.Cleanup(func() { _ = client.Close() })

			require.NoError(t, client.Start(t.Context()))
			assert.Equal(t, "/message", client.GetEndpoint().Path)
		})
	}
}

func TestStreamableHTTPLeadingBOM(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		opts   []StreamableHTTPCOption
	}{
		{name: "without BOM"},
		{name: "with BOM", prefix: "\ufeff"},
		{name: "with BOM at message bound", prefix: "\ufeff", opts: []StreamableHTTPCOption{WithHTTPMaxMessageBytes(int64(len(pingResult(0))))}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := sendPing(t, func(w http.ResponseWriter) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, tt.prefix+"data: "+pingResult(0)+"\n\n")
			}, tt.opts...)

			require.NoError(t, err)
			require.NotNil(t, response)
			assert.JSONEq(t, `{"pad":""}`, string(response.Result))
		})
	}
}

func TestSSEReaderLeadingBOM(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "without BOM", input: "data: hello\n\n", want: "data: hello\n\n"},
		{name: "with BOM", input: "\ufeffdata: hello\n\n", want: "data: hello\n\n"},
		{name: "only one BOM removed", input: "\ufeff\ufeffdata: hello\n\n", want: "\ufeffdata: hello\n\n"},
		{name: "BOM in data preserved", input: "\ufeffdata: \ufeffhello\n\n", want: "data: \ufeffhello\n\n"},
		{name: "BOM on later line preserved", input: "\ufeffdata: hello\n\ufeffdata: world\n\n", want: "data: hello\n\ufeffdata: world\n\n"},
		{name: "short stream", input: ":\n", want: ":\n"},
		{name: "empty stream"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, err := newSSEReader(iotest.OneByteReader(strings.NewReader(tt.input)))
			require.NoError(t, err)
			got, err := io.ReadAll(reader)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}
}

func TestSSEReaderInitialError(t *testing.T) {
	readErr := errors.New("stream read failed")
	for _, prefix := range []string{"", "da", "\xef\xbb"} {
		t.Run(fmt.Sprintf("prefix %q", prefix), func(t *testing.T) {
			reader, err := newSSEReader(io.MultiReader(strings.NewReader(prefix), iotest.ErrReader(readErr)))
			require.ErrorIs(t, err, readErr)
			assert.Nil(t, reader)
		})
	}
}
