package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

// bufferingHTTPResponseWriter is a minimal HTTPResponseWriter that buffers all
// output. Its CanStream method always returns false, simulating an integration
// (e.g. via fiber's adaptor.HTTPHandler) where the underlying transport cannot
// deliver chunked SSE responses.
type bufferingHTTPResponseWriter struct {
	mu     sync.Mutex
	header http.Header
	status int
	body   []byte
	wrote  bool
}

func newBufferingHTTPResponseWriter() *bufferingHTTPResponseWriter {
	return &bufferingHTTPResponseWriter{header: make(http.Header), status: http.StatusOK}
}

func (b *bufferingHTTPResponseWriter) Header() http.Header {
	return b.header
}

func (b *bufferingHTTPResponseWriter) WriteHeader(status int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.wrote {
		return
	}
	b.status = status
	b.wrote = true
}

func (b *bufferingHTTPResponseWriter) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.wrote {
		b.wrote = true
	}
	b.body = append(b.body, p...)
	return len(p), nil
}

func (b *bufferingHTTPResponseWriter) Flush() {}

func (b *bufferingHTTPResponseWriter) CanStream() bool { return false }

// flushableHTTPResponseWriter is an HTTPResponseWriter that records flushes so
// tests can inspect SSE behavior without spinning up a real net/http stack.
type flushableHTTPResponseWriter struct {
	bufferingHTTPResponseWriter
	flushes int
}

func newFlushableHTTPResponseWriter() *flushableHTTPResponseWriter {
	return &flushableHTTPResponseWriter{
		bufferingHTTPResponseWriter: bufferingHTTPResponseWriter{
			header: make(http.Header),
			status: http.StatusOK,
		},
	}
}

func (f *flushableHTTPResponseWriter) Flush() {
	f.mu.Lock()
	f.flushes++
	f.mu.Unlock()
}

func (f *flushableHTTPResponseWriter) CanStream() bool { return true }

func TestStreamableHTTPServer_Handle_InitializeWithoutNetHTTP(t *testing.T) {
	mcpServer := NewMCPServer("test-mcp-server", "1.0")
	srv := NewStreamableHTTPServer(mcpServer, WithStateful(true))

	body := []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "initialize",
		"params": {
			"protocolVersion": "2025-03-26",
			"capabilities": {},
			"clientInfo": {"name": "buffer-client", "version": "0.0.1"}
		}
	}`)

	w := newBufferingHTTPResponseWriter()
	r := &HTTPRequest{
		Method:  http.MethodPost,
		URL:     &url.URL{Path: "/mcp"},
		Header:  http.Header{"Content-Type": []string{"application/json"}},
		Body:    body,
		Context: t.Context(),
	}

	srv.Handle(w, r)

	assert.Equal(t, http.StatusOK, w.status)
	assert.Equal(t, "application/json", w.header.Get("Content-Type"))
	sessionID := w.header.Get(HeaderKeySessionID)
	assert.NotEmpty(t, sessionID, "stateful server should return a session id on initialize")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.body, &resp))
	assert.Equal(t, "2.0", resp["jsonrpc"])
	require.Contains(t, resp, "result")
}

func TestStreamableHTTPServer_Handle_GetRejectedWhenWriterCannotStream(t *testing.T) {
	mcpServer := NewMCPServer("test-mcp-server", "1.0")
	srv := NewStreamableHTTPServer(mcpServer)

	w := newBufferingHTTPResponseWriter()
	r := &HTTPRequest{
		Method:  http.MethodGet,
		URL:     &url.URL{Path: "/mcp"},
		Header:  http.Header{},
		Context: t.Context(),
	}

	srv.Handle(w, r)

	assert.Equal(t, http.StatusMethodNotAllowed, w.status)
	assert.Contains(t, string(w.body), "Streaming unsupported")
}

func TestStreamableHTTPServer_Handle_PostNotificationsBufferedWhenNotStreamable(t *testing.T) {
	// When the writer can't stream, mid-flight notifications must NOT be
	// emitted as SSE; the server should fall back to the buffered JSON path.
	mcpServer := NewMCPServer("test-mcp-server", "1.0")
	srv := NewStreamableHTTPServer(mcpServer, WithStateLess(true))

	body := []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "ping"
	}`)

	w := newBufferingHTTPResponseWriter()
	r := &HTTPRequest{
		Method:  http.MethodPost,
		URL:     &url.URL{Path: "/mcp"},
		Header:  http.Header{"Content-Type": []string{"application/json"}},
		Body:    body,
		Context: t.Context(),
	}

	srv.Handle(w, r)

	assert.Equal(t, http.StatusOK, w.status)
	assert.Equal(t, "application/json", w.header.Get("Content-Type"))
	assert.False(t, strings.HasPrefix(string(w.body), "event:"),
		"non-streaming writer must never receive SSE-formatted output")
}

func TestStreamableHTTPServer_Handle_InvalidContentType(t *testing.T) {
	mcpServer := NewMCPServer("test-mcp-server", "1.0")
	srv := NewStreamableHTTPServer(mcpServer)

	w := newBufferingHTTPResponseWriter()
	r := &HTTPRequest{
		Method:  http.MethodPost,
		URL:     &url.URL{Path: "/mcp"},
		Header:  http.Header{"Content-Type": []string{"text/plain"}},
		Body:    []byte(`{}`),
		Context: t.Context(),
	}

	srv.Handle(w, r)

	assert.Equal(t, http.StatusBadRequest, w.status)
	assert.Contains(t, string(w.body), "Invalid content type")
}

func TestStreamableHTTPServer_Handle_UnknownMethod(t *testing.T) {
	mcpServer := NewMCPServer("test-mcp-server", "1.0")
	srv := NewStreamableHTTPServer(mcpServer)

	w := newBufferingHTTPResponseWriter()
	r := &HTTPRequest{
		Method:  http.MethodPatch,
		URL:     &url.URL{Path: "/mcp"},
		Header:  http.Header{},
		Context: t.Context(),
	}

	srv.Handle(w, r)

	assert.Equal(t, http.StatusNotFound, w.status)
}

func TestStreamableHTTPServer_Handle_FlushableWriterMatchesServeHTTP(t *testing.T) {
	// A streamable HTTPResponseWriter going through Handle should produce
	// the same JSON body as net/http's default writer going through ServeHTTP
	// for a vanilla initialize request.
	mcpServer := NewMCPServer("test-mcp-server", "1.0")
	srv := NewStreamableHTTPServer(mcpServer)

	body := []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "initialize",
		"params": {
			"protocolVersion": "2025-03-26",
			"capabilities": {},
			"clientInfo": {"name": "stream-client", "version": "0.0.1"}
		}
	}`)

	w := newFlushableHTTPResponseWriter()
	r := &HTTPRequest{
		Method:  http.MethodPost,
		URL:     &url.URL{Path: "/mcp"},
		Header:  http.Header{"Content-Type": []string{"application/json"}},
		Body:    body,
		Context: t.Context(),
	}

	srv.Handle(w, r)

	assert.Equal(t, http.StatusOK, w.status)
	assert.Equal(t, "application/json", w.header.Get("Content-Type"))
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.body, &resp))
	assert.Equal(t, "2.0", resp["jsonrpc"])
	require.Contains(t, resp, "result")
}

func TestStreamableHTTPServer_Handle_ContextCarriedThrough(t *testing.T) {
	// Values placed on HTTPRequest.Context must reach tool handlers, since
	// pre-decorating the context is the canonical way for non-net/http
	// integrations to replace WithHTTPContextFunc.
	type ctxKey struct{}

	mcpServer := NewMCPServer("test-mcp-server", "1.0")

	var seen any
	mcpServer.AddTool(
		mcp.NewTool("echo", mcp.WithDescription("ctx echo")),
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			seen = ctx.Value(ctxKey{})
			return mcp.NewToolResultText("ok"), nil
		},
	)

	srv := NewStreamableHTTPServer(mcpServer, WithStateLess(true))

	// Initialize first.
	initBody := []byte(`{
		"jsonrpc":"2.0","id":1,"method":"initialize",
		"params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"x","version":"0"}}
	}`)
	w := newBufferingHTTPResponseWriter()
	srv.Handle(w, &HTTPRequest{
		Method:  http.MethodPost,
		URL:     &url.URL{Path: "/mcp"},
		Header:  http.Header{"Content-Type": []string{"application/json"}},
		Body:    initBody,
		Context: t.Context(),
	})
	require.Equal(t, http.StatusOK, w.status)

	// Then call the tool with a decorated context.
	callBody := []byte(`{
		"jsonrpc":"2.0","id":2,"method":"tools/call",
		"params":{"name":"echo","arguments":{}}
	}`)
	w2 := newBufferingHTTPResponseWriter()
	ctx := context.WithValue(t.Context(), ctxKey{}, "from-fasthttp")
	srv.Handle(w2, &HTTPRequest{
		Method:  http.MethodPost,
		URL:     &url.URL{Path: "/mcp"},
		Header:  http.Header{"Content-Type": []string{"application/json"}},
		Body:    callBody,
		Context: ctx,
	})
	require.Equal(t, http.StatusOK, w2.status)

	assert.Equal(t, "from-fasthttp", seen)
}
