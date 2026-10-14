package servertest

import (
	"net/http/httptest"

	"github.com/mark3labs/mcp-go/server"
)

// NewTestServer creates a test SSE server. Replaces server.NewTestServer.
func NewTestServer(srv *server.MCPServer, opts ...server.SSEOption) *httptest.Server {
	s := server.NewSSEServer(srv, opts...)
	ts := httptest.NewServer(s)
	server.WithBaseURL(ts.URL)(s)
	return ts
}
