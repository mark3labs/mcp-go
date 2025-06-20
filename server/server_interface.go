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
	// Message handling
	HandleMessage(ctx context.Context, message json.RawMessage) mcp.JSONRPCMessage

	// Session management
	RegisterSession(ctx context.Context, session ClientSession) error
	UnregisterSession(ctx context.Context, sessionID string)
	WithContext(ctx context.Context, session ClientSession) context.Context

	// Notifications
	SendNotificationToClient(ctx context.Context, method string, params map[string]any) error
	SendNotificationToSpecificClient(sessionID string, method string, params map[string]any) error
	SendNotificationToAllClients(method string, params map[string]any)

	// Session-specific tools
	AddSessionTool(sessionID string, tool mcp.Tool, handler ToolHandlerFunc) error
	AddSessionTools(sessionID string, tools ...ServerTool) error
	DeleteSessionTools(sessionID string, names ...string) error

	// Global tools management
	AddTool(tool mcp.Tool, handler ToolHandlerFunc)
	AddTools(tools ...ServerTool)
	DeleteTools(names ...string)

	// Prompts management
	AddPrompt(prompt mcp.Prompt, handler PromptHandlerFunc)
	AddPrompts(prompts ...ServerPrompt)
	DeletePrompts(names ...string)

	// Resources management
	AddResource(resource mcp.Resource, handler ResourceHandlerFunc)
	AddResources(resources ...ServerResource)
	RemoveResource(uri string)
	AddResourceTemplate(template mcp.ResourceTemplate, handler ResourceTemplateHandlerFunc)
}
