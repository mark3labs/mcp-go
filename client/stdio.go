package client

import (
	"context"
	"io"

	"github.com/mark3labs/mcp-go/client/transport"
)

// StdioMCPClient implements the MCPClient interface using stdio communication.
// It launches a subprocess and communicates with it via standard input/output streams
// using JSON-RPC messages. The client handles message routing between requests and
// responses, and supports asynchronous notifications.
//
// Deprecated: Use Client instead.
type StdioMCPClient struct {
	Client
}

// NewStdioMCPClient creates a new stdio-based MCP client that communicates with a subprocess.
// It launches the specified command with given arguments and sets up stdin/stdout pipes for communication.
// Returns an error if the subprocess cannot be started or the pipes cannot be created.
//
// NewStdioMCPClient will start the connection automatically. Don't call the Start method manually.
//
// Deprecated: Use NewClient instead.
func NewStdioMCPClient(
	command string,
	env []string,
	args ...string,
) (*StdioMCPClient, error) {

	stdioTransport := transport.NewStdio(command, env, args...)
	stdioTransport.Start(context.Background())

	return &StdioMCPClient{
		Client: *NewClient(stdioTransport),
	}, nil
}

// Stderr returns a reader for the stderr output of the subprocess.
// This can be used to capture error messages or logs from the subprocess.
func (c *StdioMCPClient) Stderr() io.Reader {
	t := c.GetTransport()
	stdio := t.(*transport.Stdio)
	return stdio.Stderr()
}
