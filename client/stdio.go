package client

import (
	"context"
	"fmt"
	"io"

	"github.com/mark3labs/mcp-go/client/transport"
)

// NewStdioMCPClient creates a new stdio-based MCP client that communicates with a subprocess.
// It launches the specified command with given arguments and sets up stdin/stdout pipes for communication.
// Returns an error if the subprocess cannot be started or the pipes cannot be created.
//
// NOTICE: NewStdioMCPClient will start the connection automatically.
// This is for backward compatibility.
func NewStdioMCPClient(
	command string,
	env []string,
	args ...string,
) (*Client, error) {
	return NewStdioMCPClientWithOptions(command, env, args)
}

// NewStdioMCPClientWithOptions creates a new stdio-based MCP client that communicates with a subprocess.
// It launches the specified command with given arguments and sets up stdin/stdout pipes for communication.
// Optional configuration functions can be provided to customize the transport before it starts,
// such as setting a custom command function.
//
// NOTICE: NewStdioMCPClientWithOptions automatically starts the underlying transport.
// This is for backward compatibility.
func NewStdioMCPClientWithOptions(
	command string,
	env []string,
	args []string,
	opts ...transport.StdioOption,
) (*Client, error) {
	stdioTransport := transport.NewStdioWithOptions(command, env, args, opts...)

	if err := stdioTransport.Start(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to start stdio transport: %w", err)
	}

	return NewClient(stdioTransport), nil
}

// NewStdioClient creates a new stdio-based MCP client that communicates with
// a subprocess. It launches the specified command with the given environment
// variables and arguments, starts the underlying transport, and applies the
// supplied client-level options (e.g. WithTracer, WithPropagator) to the
// returned client.
//
// Callers that need transport-level options (e.g. a custom command function)
// should fall back to NewStdioMCPClientWithOptions or construct the transport
// directly with transport.NewStdioWithOptions and call NewClient.
func NewStdioClient(
	command string,
	env []string,
	args []string,
	opts ...ClientOption,
) (*Client, error) {
	stdioTransport := transport.NewStdioWithOptions(command, env, args)

	if err := stdioTransport.Start(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to start stdio transport: %w", err)
	}

	return NewClient(stdioTransport, opts...), nil
}

// GetStderr returns a reader for the stderr output of the subprocess.
// This can be used to capture error messages or logs from the subprocess.
//
// It works with any transport that exposes a Stderr() io.Reader method,
// including *transport.Stdio and *transport.CommandTransport.
func GetStderr(c *Client) (io.Reader, bool) {
	t := c.GetTransport()

	type stderrer interface {
		Stderr() io.Reader
	}
	if s, ok := t.(stderrer); ok {
		return s.Stderr(), true
	}
	return nil, false
}
