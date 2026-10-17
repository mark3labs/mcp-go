package server

import "net/http/httptest"

// NewTestServer creates a test SSE server for internal package tests.
// This helper is defined in a _test.go file so production builds do not link net/http/httptest.
// External packages should use github.com/mark3labs/mcp-go/server/servertest.NewTestServer.
func NewTestServer(srv *MCPServer, opts ...SSEOption) *httptest.Server {
	s := NewSSEServer(srv, opts...)
	ts := httptest.NewServer(s)
	WithBaseURL(ts.URL)(s)
	return ts
}

// NewTestStreamableHTTPServer creates a test Streamable HTTP server for internal package tests.
// External packages should use servertest.NewTestStreamableHTTPServer.
func NewTestStreamableHTTPServer(srv *MCPServer, opts ...StreamableHTTPOption) *httptest.Server {
	s := NewStreamableHTTPServer(srv, opts...)
	return httptest.NewServer(s)
}
