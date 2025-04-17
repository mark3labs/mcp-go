package client

import (
	"fmt"
	"github.com/mark3labs/mcp-go/client/transport"
)

// NewStreamableHttpClient creates a new streamable-http-based MCP client with the given base URL.
// Returns an error if the URL is invalid.
func NewStreamableHttpClient(baseURL string, options ...transport.StreamableHTTPCOption) (*Client, error) {
	trans, err := transport.NewStreamableHTTP(baseURL, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSE transport: %w", err)
	}
	return NewClient(trans), nil
}

// GetSessionID returns the current session ID for the Streamable HTTP connection.
//
// Note: This method only works with Streamable HTTP transport, or it will panic.
func GetSessionID(c *Client) string {
	t := c.GetTransport()
	sse := t.(*transport.StreamableHTTP)
	return sse.GetSessionId()
}
