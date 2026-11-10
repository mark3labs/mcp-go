package server

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A POST body is read only up to a bound: a larger one is answered 413
// Request Entity Too Large with a JSON-RPC error, so a client cannot make
// the server hold an arbitrary amount of memory.
func TestStreamableHTTP_RequestBodyIsBounded(t *testing.T) {
	initialize := func(padding int) []byte {
		return []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","clientInfo":{"name":"c","version":"1"},"capabilities":{},"_pad":"` + strings.Repeat("x", padding) + `"}}`)
	}
	for name, tt := range map[string]struct {
		opts    []StreamableHTTPOption
		body    []byte
		chunked bool
		status  int
	}{
		"under the bound":           {opts: []StreamableHTTPOption{WithMaxRequestBodyBytes(1024)}, body: initialize(100), status: http.StatusOK},
		"over the bound":            {opts: []StreamableHTTPOption{WithMaxRequestBodyBytes(1024)}, body: initialize(2048), status: http.StatusRequestEntityTooLarge},
		"chunked under the bound":   {opts: []StreamableHTTPOption{WithMaxRequestBodyBytes(1024)}, body: initialize(100), chunked: true, status: http.StatusOK},
		"chunked over the bound":    {opts: []StreamableHTTPOption{WithMaxRequestBodyBytes(1024)}, body: initialize(2048), chunked: true, status: http.StatusRequestEntityTooLarge},
		"over the default bound":    {body: initialize(DefaultMaxRequestBodyBytes), status: http.StatusRequestEntityTooLarge},
		"no bound when disabled":    {opts: []StreamableHTTPOption{WithMaxRequestBodyBytes(0)}, body: initialize(DefaultMaxRequestBodyBytes), status: http.StatusOK},
		"unreadable over the bound": {opts: []StreamableHTTPOption{WithMaxRequestBodyBytes(1024)}, body: bytes.Repeat([]byte("x"), 2048), status: http.StatusRequestEntityTooLarge},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(NewStreamableHTTPServer(NewMCPServer("t", "1"), tt.opts...))
			t.Cleanup(server.Close)
			var reader io.Reader = bytes.NewReader(tt.body)
			if tt.chunked {
				// A reader of a type http.NewRequest cannot size is sent chunked.
				reader = io.NopCloser(reader)
			}
			req, err := http.NewRequest(http.MethodPost, server.URL+"/mcp", reader)
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

// The bound is on the bytes read, not on Content-Length, which middleware
// that replaces the body leaves behind.
func TestStreamableHTTP_RequestBodyBoundDoesNotTrustContentLength(t *testing.T) {
	server := NewStreamableHTTPServer(NewMCPServer("t", "1"), WithMaxRequestBodyBytes(1024))
	req := httptest.NewRequest(http.MethodPost, "/mcp", io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), 2048))))
	req.ContentLength = 10
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rr.Code)
}

// countingReader counts the bytes it hands out.
type countingReader struct {
	io.Reader
	n int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.n += int64(n)
	return n, err
}

// readBounded never takes more than the bound and one byte from the reader,
// whatever the bound and the buffer's growth.
func TestReadBoundedNeverReadsPastTheBound(t *testing.T) {
	for _, limit := range []int64{1, 100, 511, 512, 513, 1024, 1 << 20} {
		reader := &countingReader{Reader: bytes.NewReader(bytes.Repeat([]byte("z"), 2<<20))}
		_, err := readBounded(reader, limit)
		require.ErrorIs(t, err, errBodyTooLarge, "limit %d", limit)
		assert.LessOrEqual(t, reader.n, limit+1, "limit %d", limit)
	}
}

// readBounded reads a body of the bound's length, refuses one byte more,
// and reads from a reader that yields a byte at a time.
func TestReadBounded(t *testing.T) {
	for name, tt := range map[string]struct {
		size    int
		byByte  bool
		dataErr bool
		limit   int64 // 1024 when zero
		wantErr error
	}{
		"empty":                   {size: 0},
		"within the bound":        {size: 1000},
		"at the bound":            {size: 1024},
		"one over the bound":      {size: 1025, wantErr: errBodyTooLarge},
		"far over the bound":      {size: 1 << 20, wantErr: errBodyTooLarge},
		"byte at a time":          {size: 1024, byByte: true},
		"byte at a time and over": {size: 1025, byByte: true, wantErr: errBodyTooLarge},
		"data and EOF together":   {size: 1024, dataErr: true},
		"the largest bound":       {size: 1000, limit: math.MaxInt64},
	} {
		t.Run(name, func(t *testing.T) {
			data := bytes.Repeat([]byte("y"), tt.size)
			var reader io.Reader = bytes.NewReader(data)
			if tt.byByte {
				reader = iotest.OneByteReader(reader)
			}
			if tt.dataErr {
				reader = iotest.DataErrReader(reader)
			}
			limit := tt.limit
			if limit == 0 {
				limit = 1024
			}
			got, err := readBounded(reader, limit)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, data, got)
		})
	}
}
