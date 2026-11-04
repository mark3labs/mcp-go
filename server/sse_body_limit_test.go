package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSSE_RequestBodyIsBounded checks that a message POST body is read only
// up to a bound: a larger one is answered 413 Request Entity Too Large with a
// JSON-RPC error, so a client cannot make the server hold an arbitrary amount
// of memory.
func TestSSE_RequestBodyIsBounded(t *testing.T) {
	ping := func(padding int) []byte {
		return []byte(`{"jsonrpc":"2.0","id":1,"method":"ping","params":{"_pad":"` + strings.Repeat("x", padding) + `"}}`)
	}
	tests := []struct {
		name    string
		opts    []SSEOption
		body    []byte
		chunked bool
		status  int
	}{
		{name: "under the bound", opts: []SSEOption{WithSSEMaxRequestBodyBytes(1024)}, body: ping(100), status: http.StatusAccepted},
		{name: "over the bound", opts: []SSEOption{WithSSEMaxRequestBodyBytes(1024)}, body: ping(2048), status: http.StatusRequestEntityTooLarge},
		{name: "chunked under the bound", opts: []SSEOption{WithSSEMaxRequestBodyBytes(1024)}, body: ping(100), chunked: true, status: http.StatusAccepted},
		{name: "chunked over the bound", opts: []SSEOption{WithSSEMaxRequestBodyBytes(1024)}, body: ping(2048), chunked: true, status: http.StatusRequestEntityTooLarge},
		{name: "over the default bound", body: ping(DefaultMaxRequestBodyBytes), status: http.StatusRequestEntityTooLarge},
		{name: "no bound when disabled", opts: []SSEOption{WithSSEMaxRequestBodyBytes(0)}, body: ping(DefaultMaxRequestBodyBytes), status: http.StatusAccepted},
		{name: "unreadable over the bound", opts: []SSEOption{WithSSEMaxRequestBodyBytes(1024)}, body: bytes.Repeat([]byte("x"), 2048), status: http.StatusRequestEntityTooLarge},
		{name: "unreadable under the bound", opts: []SSEOption{WithSSEMaxRequestBodyBytes(1024)}, body: []byte("x"), status: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(NewSSEServer(NewMCPServer("t", "1"), tt.opts...))
			t.Cleanup(ts.Close)
			sse, err := http.Get(ts.URL + "/sse")
			require.NoError(t, err)
			t.Cleanup(func() { sse.Body.Close() })
			endpoint := readSSEEndpoint(t, bufio.NewReader(sse.Body))

			var reader io.Reader = bytes.NewReader(tt.body)
			if tt.chunked {
				// A reader of a type http.NewRequest cannot size is sent chunked.
				reader = io.NopCloser(reader)
			}
			req, err := http.NewRequest(http.MethodPost, ts.URL+endpoint, reader)
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, tt.status, resp.StatusCode)
			if tt.status == http.StatusRequestEntityTooLarge {
				var body mcp.JSONRPCError
				require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
				assert.Equal(t, mcp.INVALID_REQUEST, body.Error.Code)
			}
		})
	}
}

// TestSSE_RequestBodyBoundDoesNotTrustContentLength checks that the bound is
// on the bytes read, not on Content-Length, which middleware that replaces the
// body leaves behind.
func TestSSE_RequestBodyBoundDoesNotTrustContentLength(t *testing.T) {
	server := NewSSEServer(NewMCPServer("t", "1"), WithSSEMaxRequestBodyBytes(1024))
	ts := httptest.NewServer(server)
	t.Cleanup(ts.Close)
	sse, err := http.Get(ts.URL + "/sse")
	require.NoError(t, err)
	t.Cleanup(func() { sse.Body.Close() })
	endpoint := readSSEEndpoint(t, bufio.NewReader(sse.Body))

	req := httptest.NewRequest(http.MethodPost, endpoint, io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), 2048))))
	req.ContentLength = 10
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rr.Code)
}

// TestSSE_RequestBodyOverDeclaredBoundIsRefusedUnread checks that a body whose
// Content-Length is already over the bound is answered 413 without a read.
func TestSSE_RequestBodyOverDeclaredBoundIsRefusedUnread(t *testing.T) {
	server := NewSSEServer(NewMCPServer("t", "1"), WithSSEMaxRequestBodyBytes(1024))
	ts := httptest.NewServer(server)
	t.Cleanup(ts.Close)
	sse, err := http.Get(ts.URL + "/sse")
	require.NoError(t, err)
	t.Cleanup(func() { sse.Body.Close() })
	endpoint := readSSEEndpoint(t, bufio.NewReader(sse.Body))

	body := &countingReader{Reader: bytes.NewReader(bytes.Repeat([]byte("x"), 2048))}
	req := httptest.NewRequest(http.MethodPost, endpoint, io.NopCloser(body))
	req.ContentLength = 2048
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rr.Code)
	assert.Zero(t, body.n)
}
