// Package mcptest implements helper functions for testing MCP servers.
package mcptest

import (
	"bytes"
	"context"
	"io"
	"log"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Server encapsulates an MCP server and manages resources like pipes and context.
type Server struct {
	name  string
	tools []server.ServerTool

	ctx    context.Context
	cancel func()

	serverReader io.Reader
	serverWriter io.Writer
	clientReader io.Reader
	clientWriter io.WriteCloser

	logBuffer bytes.Buffer

	transport transport.Interface
}

// NewServer starts a new MCP server with the provided tools and returns the server instance.
func NewServer(t *testing.T, tools ...server.ServerTool) (*Server, error) {
	server := NewUnstartedServer(t)
	server.AddTools(tools...)

	if err := server.Start(); err != nil {
		return nil, err
	}

	return server, nil
}

// NewUnstartedServer creates a new MCP server instance with the given name, but does not start the server.
// Useful for tests where you need to add tools before starting the server.
func NewUnstartedServer(t *testing.T) *Server {
	server := &Server{
		name: t.Name(),
	}

	// Use t.Context() once we switch to go >= 1.24
	ctx := context.TODO()

	// Set up context with cancellation, used to stop the server
	server.ctx, server.cancel = context.WithCancel(ctx)

	// Set up pipes for client-server communication
	server.serverReader, server.clientWriter = io.Pipe()
	server.clientReader, server.serverWriter = io.Pipe()

	// Return the configured server
	return server
}

// AddTools adds multiple tools to an unstarted server.
func (s *Server) AddTools(tools ...server.ServerTool) {
	s.tools = append(s.tools, tools...)
}

// AddTool adds a tool to an unstarted server.
func (s *Server) AddTool(tool mcp.Tool, handler server.ToolHandlerFunc) {
	s.tools = append(s.tools, server.ServerTool{
		Tool:    tool,
		Handler: handler,
	})
}

// Start starts the server in a goroutine. Make sure to defer Close() after Start().
// When using NewServer(), the returned server is already started.
func (s *Server) Start() error {
	// Start the MCP server in a goroutine
	go func() {
		mcpServer := server.NewMCPServer(s.name, "1.0.0")

		mcpServer.AddTools(s.tools...)

		logger := log.New(&s.logBuffer, "", 0)

		stdioServer := server.NewStdioServer(mcpServer)
		stdioServer.SetErrorLogger(logger)

		if err := stdioServer.Listen(s.ctx, s.serverReader, s.serverWriter); err != nil {
			logger.Println("StdioServer.Listen failed:", err)
		}
	}()

	s.transport = transport.NewIO(s.clientReader, s.clientWriter, io.NopCloser(&s.logBuffer))

	return s.transport.Start(s.ctx)
}

// Close stops the server and cleans up resources like temporary directories.
func (s *Server) Close() {
	if s.transport != nil {
		s.transport.Close()
		s.transport = nil
	}

	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

// Client returns an MCP client connected to the server.
func (s *Server) Client() client.MCPClient {
	return client.NewClient(s.transport)
}
