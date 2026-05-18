package client

import (
	"testing"

	"github.com/mark3labs/mcp-go/client/transport"
)

// TestNewStdioClient_AppliesClientOptions exercises the new constructor's
// promise: every ClientOption passed in the variadic slot is applied to the
// returned client. WithSession is convenient because it sets a single bool
// observable from the test.
func TestNewStdioClient_AppliesClientOptions(t *testing.T) {
	// Use a no-op command for the underlying subprocess; the test only
	// cares that NewStdioClient applies the options before returning.
	c, err := NewStdioClient("true", nil, nil, WithSession())
	if err != nil {
		t.Fatalf("NewStdioClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if !c.initialized {
		t.Fatalf("WithSession not applied; initialized=false")
	}
}

// TestNewSSEClient_AppliesClientOptions verifies that NewSSEClient routes
// the variadic ClientOption arguments through to the constructed client
// while keeping transportOpts separate.
func TestNewSSEClient_AppliesClientOptions(t *testing.T) {
	c, err := NewSSEClient(
		"http://example.invalid/sse",
		[]transport.ClientOption{transport.WithHeaders(map[string]string{"x-test": "1"})},
		WithSession(),
	)
	if err != nil {
		t.Fatalf("NewSSEClient: %v", err)
	}
	if !c.initialized {
		t.Fatalf("WithSession not applied; initialized=false")
	}
}

// TestNewStreamableHTTPClient_AppliesClientOptions verifies the same routing
// for the streamable-http convenience constructor.
func TestNewStreamableHTTPClient_AppliesClientOptions(t *testing.T) {
	c, err := NewStreamableHTTPClient(
		"http://example.invalid/mcp",
		nil,
		WithSession(),
	)
	if err != nil {
		t.Fatalf("NewStreamableHTTPClient: %v", err)
	}
	if !c.initialized {
		t.Fatalf("WithSession not applied; initialized=false")
	}
}

// TestNewStreamableHTTPClient_PreservesAutoSession asserts that the
// existing "transport reports an active session ID → set WithSession"
// behaviour from NewStreamableHttpClient is preserved on the new
// constructor. The fixture transport here has no session, so initialized
// reflects only the caller-supplied opts.
func TestNewStreamableHTTPClient_PreservesAutoSession(t *testing.T) {
	c, err := NewStreamableHTTPClient("http://example.invalid/mcp", nil)
	if err != nil {
		t.Fatalf("NewStreamableHTTPClient: %v", err)
	}
	if c.initialized {
		t.Fatalf("initialized=true without an active session; auto-session must not fire")
	}
}
