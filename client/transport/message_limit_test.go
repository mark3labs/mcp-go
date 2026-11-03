package transport

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestBoundedReader checks that a reader bounded at a limit yields a source of
// up to limit bytes and fails a longer one with ErrMessageTooLarge.
func TestBoundedReader(t *testing.T) {
	tests := []struct {
		name    string
		size    int
		limit   int64
		wantErr error
	}{
		{name: "under the bound", size: 10, limit: 16},
		{name: "at the bound", size: 16, limit: 16},
		{name: "over the bound", size: 17, limit: 16, wantErr: ErrMessageTooLarge},
		{name: "empty", size: 0, limit: 16},
		{name: "no bound when disabled", size: 1 << 16, limit: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := strings.Repeat("x", tt.size)
			got, err := io.ReadAll(newBoundedReader(strings.NewReader(src), tt.limit))
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, src, string(got))
		})
	}
}

// TestReadSSELine checks that a line may carry the bound's worth of data
// besides its field name and line ending, across bufio buffer refills, and
// that a longer one is ErrMessageTooLarge.
func TestReadSSELine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		limit   int64
		wantErr error
	}{
		{name: "under the bound", line: "data: " + strings.Repeat("x", 10) + "\n", limit: 16},
		{name: "data at the bound", line: "data: " + strings.Repeat("x", 16) + "\r\n", limit: 16},
		{name: "event field at the bound", line: "event: " + strings.Repeat("x", 16) + "\r\n", limit: 16},
		{name: "LF event field at the bound", line: "event: " + strings.Repeat("x", 16) + "\n", limit: 16},
		{name: "LF event field one byte over", line: "event: " + strings.Repeat("x", 17) + "\n", limit: 16, wantErr: ErrMessageTooLarge},
		{name: "CRLF event field one byte over", line: "event: " + strings.Repeat("x", 17) + "\r\n", limit: 16, wantErr: ErrMessageTooLarge},
		{name: "CR data at the bound", line: "data: " + strings.Repeat("x", 16) + "\r", limit: 16},
		{name: "CR event field at the bound", line: "event: " + strings.Repeat("x", 16) + "\r", limit: 16},
		{name: "CR event field one byte over", line: "event: " + strings.Repeat("x", 17) + "\r", limit: 16, wantErr: ErrMessageTooLarge},
		{name: "CR over the bound", line: "data: " + strings.Repeat("x", 32) + "\r", limit: 16, wantErr: ErrMessageTooLarge},
		{name: "over the bound", line: "data: " + strings.Repeat("x", 32) + "\n", limit: 16, wantErr: ErrMessageTooLarge},
		{name: "longer than the read buffer", line: "data: " + strings.Repeat("x", 3*4096) + "\n", limit: 4 * 4096},
		{name: "over the bound past the read buffer", line: "data: " + strings.Repeat("x", 3*4096) + "\n", limit: 4096, wantErr: ErrMessageTooLarge},
		{name: "no bound when disabled", line: "data: " + strings.Repeat("x", 3*4096) + "\n", limit: 0},
		{name: "no bound when negative", line: "data: " + strings.Repeat("x", 3*4096) + "\r", limit: -1},
		{name: "maximum bound", line: "data: content\r", limit: math.MaxInt64},
		{name: "larger than scanner limit", line: "data: " + strings.Repeat("x", 128*1024) + "\r\n", limit: 128 * 1024},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := newSSELineReader(bufio.NewReaderSize(strings.NewReader(tt.line+"data: next\n"), 4096))
			got, err := lines.readLine(tt.limit)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, strings.TrimRight(tt.line, "\r\n"), got)
			next, err := lines.readLine(tt.limit)
			require.NoError(t, err)
			assert.Equal(t, "data: next", next)
		})
	}
}

// pingResult is a JSON-RPC response to request 1 whose result carries padding
// bytes.
func pingResult(padding int) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"result":{"pad":%q}}`, strings.Repeat("x", padding))
}

// sendPing sends request 1 over a Streamable HTTP client of a server that
// answers with respond.
func sendPing(t *testing.T, respond func(w http.ResponseWriter), opts ...StreamableHTTPCOption) (*JSONRPCResponse, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		respond(w)
	}))
	t.Cleanup(server.Close)
	client, err := NewStreamableHTTP(server.URL, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client.SendRequest(t.Context(), JSONRPCRequest{JSONRPC: mcp.JSONRPC_VERSION, ID: mcp.NewRequestId(int64(1)), Method: "ping"})
}

// TestStreamableHTTP_ResponseIsBounded checks that a JSON response or SSE
// event over the bound fails the request with ErrMessageTooLarge.
func TestStreamableHTTP_ResponseIsBounded(t *testing.T) {
	jsonBody := func(body string) func(http.ResponseWriter) {
		return func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, body)
		}
	}
	eventStream := func(body string) func(http.ResponseWriter) {
		return func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, body)
		}
	}
	// splitData spreads a response over data lines of at most 512 bytes,
	// which the client joins with newlines into one event.
	splitData := func(body string) string {
		var b strings.Builder
		b.WriteString("event: message\n")
		for len(body) > 0 {
			n := min(512, len(body))
			b.WriteString("data: " + body[:n] + "\n")
			body = body[n:]
		}
		b.WriteString("\n")
		return b.String()
	}
	bound := WithHTTPMaxMessageBytes(4096)
	tests := []struct {
		name    string
		respond func(http.ResponseWriter)
		opts    []StreamableHTTPCOption
		wantErr error
	}{
		{name: "JSON under the bound", respond: jsonBody(pingResult(100)), opts: []StreamableHTTPCOption{bound}},
		{name: "JSON over the bound", respond: jsonBody(pingResult(8192)), opts: []StreamableHTTPCOption{bound}, wantErr: ErrMessageTooLarge},
		{name: "JSON over the default bound", respond: jsonBody(pingResult(int(DefaultMaxMessageBytes))), wantErr: ErrMessageTooLarge},
		{name: "JSON with no bound when disabled", respond: jsonBody(pingResult(8192)), opts: []StreamableHTTPCOption{WithHTTPMaxMessageBytes(0)}},
		{name: "event under the bound", respond: eventStream("event: message\ndata: " + pingResult(100) + "\n\n"), opts: []StreamableHTTPCOption{bound}},
		{name: "event line over the bound", respond: eventStream("event: message\ndata: " + pingResult(8192) + "\n\n"), opts: []StreamableHTTPCOption{bound}, wantErr: ErrMessageTooLarge},
		{name: "event line without an end", respond: eventStream("data: " + strings.Repeat("x", 8192)), opts: []StreamableHTTPCOption{bound}, wantErr: ErrMessageTooLarge},
		{name: "event data lines over the bound", respond: eventStream(splitData(pingResult(8192))), opts: []StreamableHTTPCOption{bound}, wantErr: ErrMessageTooLarge},
		{name: "CR event under the bound", respond: eventStream("event: message\rdata: " + pingResult(100) + "\r\r"), opts: []StreamableHTTPCOption{bound}},
		{name: "CR event line over the bound", respond: eventStream("data: " + pingResult(8192) + "\r\r"), opts: []StreamableHTTPCOption{bound}, wantErr: ErrMessageTooLarge},
		{name: "CR event data lines over the bound", respond: eventStream(strings.ReplaceAll(splitData(pingResult(8192)), "\n", "\r")), opts: []StreamableHTTPCOption{bound}, wantErr: ErrMessageTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := sendPing(t, tt.respond, tt.opts...)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Nil(t, resp.Error)
		})
	}
}

// TestStreamableHTTP_ErrorBodyIsBounded checks that only maxErrorBodyBytes of
// an error response are read into the error.
func TestStreamableHTTP_ErrorBodyIsBounded(t *testing.T) {
	_, err := sendPing(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, strings.Repeat("x", 4*maxErrorBodyBytes))
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
	assert.LessOrEqual(t, len(err.Error()), maxErrorBodyBytes+100)
}

// TestSSE_EventIsBounded checks that an event over the bound ends the SSE
// stream and hands the connection-lost handler ErrMessageTooLarge.
func TestSSE_EventIsBounded(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr error
	}{
		{name: "event under the bound", body: "data: " + strings.Repeat("x", 100) + "\n\n", wantErr: io.EOF},
		{name: "event line over the bound", body: "data: " + strings.Repeat("x", 8192) + "\n\n", wantErr: ErrMessageTooLarge},
		{name: "event data lines over the bound", body: strings.Repeat("data: "+strings.Repeat("x", 512)+"\n", 16) + "\n", wantErr: ErrMessageTooLarge},
		{name: "CR event under the bound", body: "data: " + strings.Repeat("x", 100) + "\r\r", wantErr: io.EOF},
		{name: "CR event line over the bound", body: "data: " + strings.Repeat("x", 8192) + "\r\r", wantErr: ErrMessageTooLarge},
		{name: "CR event data lines over the bound", body: strings.Repeat("data: "+strings.Repeat("x", 512)+"\r", 16) + "\r", wantErr: ErrMessageTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sse := newBareSSE()
			sse.maxMessageBytes = 4096
			var lost error
			sse.SetConnectionLostHandler(func(err error) { lost = err })
			sse.readSSE(io.NopCloser(strings.NewReader(tt.body)), make(chan struct{}), &sync.Once{})
			assert.True(t, errors.Is(lost, tt.wantErr), "connection lost with %v", lost)
		})
	}
}

// TestNewSSE_BoundsMessagesByDefault checks the SSE client's default bound
// and that WithMaxMessageBytes changes it.
func TestNewSSE_BoundsMessagesByDefault(t *testing.T) {
	sse, err := NewSSE("http://127.0.0.1:1/sse")
	require.NoError(t, err)
	assert.Equal(t, DefaultMaxMessageBytes, sse.maxMessageBytes)
	sse, err = NewSSE("http://127.0.0.1:1/sse", WithMaxMessageBytes(0))
	require.NoError(t, err)
	assert.Zero(t, sse.maxMessageBytes)
}

// TestOAuthHandler_TokenResponseIsBounded checks that a token response over
// maxOAuthBodyBytes fails the refresh with ErrMessageTooLarge, even when its
// first maxOAuthBodyBytes hold a complete token.
func TestOAuthHandler_TokenResponseIsBounded(t *testing.T) {
	token := `{"access_token":"new-access","token_type":"bearer","expires_in":3600,"refresh_token":"next"}`
	tests := []struct {
		name    string
		body    string
		wantErr error
	}{
		{name: "under the bound", body: token},
		{name: "over the bound", body: `{"padding":"` + strings.Repeat("x", maxOAuthBodyBytes) + `"}`, wantErr: ErrMessageTooLarge},
		{name: "complete token then over the bound", body: token + strings.Repeat(" ", maxOAuthBodyBytes), wantErr: ErrMessageTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newRefreshServer(t, func(w http.ResponseWriter, _ string) {
				_, _ = io.WriteString(w, tt.body)
			})
			handler, _ := newRefreshTestHandler(t, server)
			token, err := handler.RefreshToken(t.Context(), "single-use")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "new-access", token.AccessToken)
		})
	}
}

// TestStreamableHTTP_ResumedEventIsBounded checks that an event over the
// bound on a resumed stream (SEP-1699) fails the request with
// ErrMessageTooLarge, as on the stream the request opened.
func TestStreamableHTTP_ResumedEventIsBounded(t *testing.T) {
	server := &pollingServer{postEvent: "id: 1\nretry: 10\ndata:\n\n", polls: 1}

	_, err := sendThroughPollingServer(t, server, mcp.ProtocolVersion20251125, WithHTTPMaxMessageBytes(32))
	require.ErrorIs(t, err, ErrMessageTooLarge)
}

// TestStreamableHTTP_ReturnsADecodedJSONResponse checks that a request returns
// once its JSON response is decoded, without waiting for the server to end
// the body, so a server that holds the body open cannot stall a request that
// has no deadline.
func TestStreamableHTTP_ReturnsADecodedJSONResponse(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, pingResult(100))
		w.(http.Flusher).Flush()
		<-release
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	client, err := NewStreamableHTTP(server.URL, WithHTTPMaxMessageBytes(4096))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	type result struct {
		resp *JSONRPCResponse
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := client.SendRequest(t.Context(), JSONRPCRequest{JSONRPC: mcp.JSONRPC_VERSION, ID: mcp.NewRequestId(int64(1)), Method: "ping"})
		done <- result{resp, err}
	}()
	select {
	case got := <-done:
		require.NoError(t, got.err)
		assert.Nil(t, got.resp.Error)
	case <-time.After(5 * time.Second):
		t.Fatal("the request waited for the server to end the response body")
	}
}
