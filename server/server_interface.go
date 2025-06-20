package server

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
)

// Ensure MCPServer implements the Interface
var _ Interface = (*MCPServer)(nil)

// Interface defines the essential interface that all MCP server transports depend on.
// This allows for custom implementations of the core server logic while maintaining
// compatibility with all existing transports (SSE, Stdio, StreamableHTTP).
type Interface interface {
	// HandleMessage processes an incoming JSON-RPC message and returns an appropriate response.
	// This is the core method that processes MCP protocol messages.
	HandleMessage(ctx context.Context, message json.RawMessage) mcp.JSONRPCMessage

	// RegisterSession registers a new client session with the server.
	// Returns an error if the session already exists or registration fails.
	RegisterSession(ctx context.Context, session ClientSession) error

	// UnregisterSession removes a session from the server by session ID.
	UnregisterSession(ctx context.Context, sessionID string)

	// WithContext creates a new context with the given session attached.
	// This allows the session to be retrieved later using ClientSessionFromContext.
	WithContext(ctx context.Context, session ClientSession) context.Context

	// SendNotificationToClient sends a notification to the client associated with the given context.
	// Returns an error if no session is found or the notification cannot be sent.
	SendNotificationToClient(ctx context.Context, method string, params map[string]any) error

	// SendNotificationToSpecificClient sends a notification to a specific client by session ID.
	// Returns an error if the session is not found or the notification cannot be sent.
	SendNotificationToSpecificClient(sessionID string, method string, params map[string]any) error

	// SendNotificationToAllClients broadcasts a notification to all currently active sessions.
	SendNotificationToAllClients(method string, params map[string]any)

	// AddSessionTool adds a tool for a specific session.
	// Returns an error if the session doesn't exist or doesn't support tools.
	AddSessionTool(sessionID string, tool mcp.Tool, handler ToolHandlerFunc) error

	// AddSessionTools adds multiple tools for a specific session.
	// Returns an error if the session doesn't exist or doesn't support tools.
	AddSessionTools(sessionID string, tools ...ServerTool) error

	// DeleteSessionTools removes tools from a specific session.
	// Returns an error if the session doesn't exist or doesn't support tools.
	DeleteSessionTools(sessionID string, names ...string) error
}
