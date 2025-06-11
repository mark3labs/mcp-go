package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// Client implements the MCP client.
type Client struct {
	transport transport.Interface

	initialized        bool
	notifications      []func(mcp.JSONRPCNotification)
	notifyMu           sync.RWMutex
	requestID          atomic.Int64
	clientCapabilities mcp.ClientCapabilities
	serverCapabilities mcp.ServerCapabilities
	samplingHandler     SamplingHandler
}

type ClientOption func(*Client)

// WithClientCapabilities sets the client capabilities for the client.
func WithClientCapabilities(capabilities mcp.ClientCapabilities) ClientOption {
	return func(c *Client) {
		c.clientCapabilities = capabilities
	}
}

// NewClient creates a new MCP client with the given transport.
// Usage:
//
//	stdio := transport.NewStdio("./mcp_server", nil, "xxx")
//	client, err := NewClient(stdio)
//	if err != nil {
//	    log.Fatalf("Failed to create client: %v", err)
//	}
func NewClient(transport transport.Interface, options ...ClientOption) *Client {
	client := &Client{
		transport: transport,
	}

	for _, opt := range options {
		opt(client)
	}

	return client
}

// Start initiates the connection to the server.
// Must be called before using the client.
func (c *Client) Start(ctx context.Context) error {
	if c.transport == nil {
		return fmt.Errorf("transport is nil")
	}
	err := c.transport.Start(ctx)
	if err != nil {
		return err
	}

	c.transport.SetNotificationHandler(func(notification mcp.JSONRPCNotification) {
		c.notifyMu.RLock()
		defer c.notifyMu.RUnlock()
		for _, handler := range c.notifications {
			handler(notification)
		}
	})

	// Set up bidirectional request handler for sampling if handler is available
	if c.samplingHandler != nil {
		c.transport.SetRequestHandler(c.handleIncomingRequest)
	}

	return nil
}
