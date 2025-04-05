package client

import (
	"fmt"
	"net/url"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
)

// SSEMCPClient implements the MCPClient interface using Server-Sent Events (SSE).
// It maintains a persistent HTTP connection to receive server-pushed events
// while sending requests over regular HTTP POST calls. The client handles
// automatic reconnection and message routing between requests and responses.
//
// Deprecated: Use Client instead.
type SSEMCPClient struct {
	Client
}

type ClientOption = transport.ClientOption

func WithHeaders(headers map[string]string) ClientOption {
	return transport.WithHeaders(headers)
}

func WithSSEReadTimeout(timeout time.Duration) ClientOption {
	return transport.WithSSEReadTimeout(timeout)
}

// NewSSEMCPClient creates a new SSE-based MCP client with the given base URL.
// Returns an error if the URL is invalid.
//
// Deprecated: Use NewClient instead.
func NewSSEMCPClient(baseURL string, options ...ClientOption) (*SSEMCPClient, error) {

	sseTransport, err := transport.NewSSE(baseURL, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSE transport: %w", err)
	}

	smc := &SSEMCPClient{
		Client: *NewClient(sseTransport),
	}

	return smc, nil
}

func (c *SSEMCPClient) GetBaseUrl() *url.URL {
	t := c.GetTransport()
	sse := t.(*transport.SSE)
	return sse.GetBaseURL()
}

// GetEndpoint returns the current endpoint URL for the SSE connection.
func (c *SSEMCPClient) GetEndpoint() *url.URL {
	t := c.GetTransport()
	sse := t.(*transport.SSE)
	return sse.GetEndpoint()
}
