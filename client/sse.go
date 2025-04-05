package client

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/mark3labs/mcp-go/client/transport"
)

func WithHeaders(headers map[string]string) transport.ClientOption {
	return transport.WithHeaders(headers)
}

// WithHTTPTransport sets a custom HTTP transport for the SSEMCPClient.
func WithHTTPTransport(t http.RoundTripper) transport.ClientOption {
	return transport.WithHTTPTransport(t)
}

// NewSSEMCPClient creates a new SSE-based MCP client with the given base URL.
// Returns an error if the URL is invalid.
func NewSSEMCPClient(baseURL string, options ...transport.ClientOption) (*Client, error) {

	sseTransport, err := transport.NewSSE(baseURL, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSE transport: %w", err)
	}

	return NewClient(sseTransport), nil
}

// GetEndpoint returns the current endpoint URL for the SSE connection.
//
// Note: This method only works with SSE transport, or it will panic.
func GetEndpoint(c *Client) *url.URL {
	t := c.GetTransport()
	sse := t.(*transport.SSE)
	return sse.GetEndpoint()
}
