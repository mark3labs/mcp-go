package client

import (
	"fmt"

	"github.com/mark3labs/mcp-go/client/transport"
)

// NewStreamableHttpClient is a convenience method that creates a new streamable-http-based MCP client
// with the given base URL. Returns an error if the URL is invalid.
func NewStreamableHttpClient(baseURL string, options ...transport.StreamableHTTPCOption) (*Client, error) {
	trans, err := transport.NewStreamableHTTP(baseURL, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSE transport: %w", err)
	}
	clientOptions := make([]ClientOption, 0)
	sessionID := trans.GetSessionId()
	if sessionID != "" {
		clientOptions = append(clientOptions, WithSession())
	}
	return NewClient(trans, clientOptions...), nil
}

// NewStreamableHTTPClient creates a new streamable-http-based MCP client
// with the given base URL, applying the provided transport-level options
// when constructing the transport and the provided client-level options
// to the returned client.
//
// Pass transport options (e.g. transport.WithContinuousListening) as a
// slice in transportOpts, and client options (e.g. WithTracer,
// WithPropagator) as the variadic opts. When the transport reports an
// active session ID at construction time, WithSession is appended
// automatically so the returned client skips re-initialisation.
func NewStreamableHTTPClient(
	baseURL string,
	transportOpts []transport.StreamableHTTPCOption,
	opts ...ClientOption,
) (*Client, error) {
	trans, err := transport.NewStreamableHTTP(baseURL, transportOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create streamable-http transport: %w", err)
	}
	clientOpts := opts
	if trans.GetSessionId() != "" {
		clientOpts = append(clientOpts, WithSession())
	}
	return NewClient(trans, clientOpts...), nil
}
