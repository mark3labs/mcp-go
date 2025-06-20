package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// LoggingMCPServer wraps an Interface implementation with structured logging using slog
type LoggingMCPServer struct {
	server *server.MCPServer
	logger *slog.Logger
}

// NewLoggingMCPServer creates a new logging wrapper around an Interface
func NewLoggingMCPServer(server *server.MCPServer, logger *slog.Logger) *LoggingMCPServer {
	return &LoggingMCPServer{
		server: server,
		logger: logger,
	}
}

func (l *LoggingMCPServer) HandleMessage(ctx context.Context, message json.RawMessage) mcp.JSONRPCMessage {
	// Parse basic message info for logging
	var baseMsg struct {
		ID     any           `json:"id,omitempty"`
		Method mcp.MCPMethod `json:"method,omitempty"`
	}
	json.Unmarshal(message, &baseMsg)

	start := time.Now()
	l.logger.InfoContext(ctx, "handling message",
		slog.String("method", string(baseMsg.Method)),
		slog.Any("id", baseMsg.ID),
		slog.Int("message_size", len(message)))

	response := l.server.HandleMessage(ctx, message)
	duration := time.Since(start)

	if response != nil {
		// Log response details
		responseBytes, _ := json.Marshal(response)
		l.logger.InfoContext(ctx, "message handled",
			slog.String("method", string(baseMsg.Method)),
			slog.Any("id", baseMsg.ID),
			slog.Duration("duration", duration),
			slog.Int("response_size", len(responseBytes)))
	} else {
		// Notification - no response
		l.logger.InfoContext(ctx, "notification handled",
			slog.String("method", string(baseMsg.Method)),
			slog.Duration("duration", duration))
	}

	return response
}

func (l *LoggingMCPServer) RegisterSession(ctx context.Context, session server.ClientSession) error {
	l.logger.InfoContext(ctx, "registering session",
		slog.String("session_id", session.SessionID()))

	err := l.server.RegisterSession(ctx, session)
	if err != nil {
		l.logger.ErrorContext(ctx, "failed to register session",
			slog.String("session_id", session.SessionID()),
			slog.String("error", err.Error()))
	} else {
		l.logger.InfoContext(ctx, "session registered successfully",
			slog.String("session_id", session.SessionID()))
	}
	return err
}

func (l *LoggingMCPServer) UnregisterSession(ctx context.Context, sessionID string) {
	l.logger.InfoContext(ctx, "unregistering session",
		slog.String("session_id", sessionID))
	l.server.UnregisterSession(ctx, sessionID)
	l.logger.InfoContext(ctx, "session unregistered",
		slog.String("session_id", sessionID))
}

func (l *LoggingMCPServer) WithContext(ctx context.Context, session server.ClientSession) context.Context {
	return l.server.WithContext(ctx, session)
}

func (l *LoggingMCPServer) SendNotificationToClient(ctx context.Context, method string, params map[string]any) error {
	l.logger.InfoContext(ctx, "sending notification to client",
		slog.String("method", method),
		slog.Any("params", params))

	err := l.server.SendNotificationToClient(ctx, method, params)
	if err != nil {
		l.logger.ErrorContext(ctx, "failed to send notification to client",
			slog.String("method", method),
			slog.String("error", err.Error()))
	}
	return err
}

func (l *LoggingMCPServer) SendNotificationToSpecificClient(sessionID string, method string, params map[string]any) error {
	l.logger.Info("sending notification to specific client",
		slog.String("session_id", sessionID),
		slog.String("method", method),
		slog.Any("params", params))

	err := l.server.SendNotificationToSpecificClient(sessionID, method, params)
	if err != nil {
		l.logger.Error("failed to send notification to specific client",
			slog.String("session_id", sessionID),
			slog.String("method", method),
			slog.String("error", err.Error()))
	}
	return err
}

func (l *LoggingMCPServer) SendNotificationToAllClients(method string, params map[string]any) {
	l.logger.Info("broadcasting notification to all clients",
		slog.String("method", method),
		slog.Any("params", params))
	l.server.SendNotificationToAllClients(method, params)
}

func (l *LoggingMCPServer) AddSessionTool(sessionID string, tool mcp.Tool, handler server.ToolHandlerFunc) error {
	l.logger.Info("adding session tool",
		slog.String("session_id", sessionID),
		slog.String("tool_name", tool.Name),
		slog.String("tool_description", tool.Description))

	err := l.server.AddSessionTool(sessionID, tool, handler)
	if err != nil {
		l.logger.Error("failed to add session tool",
			slog.String("session_id", sessionID),
			slog.String("tool_name", tool.Name),
			slog.String("error", err.Error()))
	}
	return err
}

func (l *LoggingMCPServer) AddSessionTools(sessionID string, tools ...server.ServerTool) error {
	toolNames := make([]string, len(tools))
	for i, tool := range tools {
		toolNames[i] = tool.Tool.Name
	}

	l.logger.Info("adding session tools",
		slog.String("session_id", sessionID),
		slog.Int("tool_count", len(tools)),
		slog.Any("tool_names", toolNames))

	err := l.server.AddSessionTools(sessionID, tools...)
	if err != nil {
		l.logger.Error("failed to add session tools",
			slog.String("session_id", sessionID),
			slog.String("error", err.Error()))
	}
	return err
}

func (l *LoggingMCPServer) DeleteSessionTools(sessionID string, names ...string) error {
	l.logger.Info("deleting session tools",
		slog.String("session_id", sessionID),
		slog.Any("tool_names", names))

	err := l.server.DeleteSessionTools(sessionID, names...)
	if err != nil {
		l.logger.Error("failed to delete session tools",
			slog.String("session_id", sessionID),
			slog.Any("tool_names", names),
			slog.String("error", err.Error()))
	}
	return err
}

func (l *LoggingMCPServer) AddTool(tool mcp.Tool, handler server.ToolHandlerFunc) {
	l.logger.Info("adding global tool",
		slog.String("tool_name", tool.Name),
		slog.String("tool_description", tool.Description))
	l.server.AddTool(tool, handler)
}

func (l *LoggingMCPServer) AddTools(tools ...server.ServerTool) {
	toolNames := make([]string, len(tools))
	for i, tool := range tools {
		toolNames[i] = tool.Tool.Name
	}

	l.logger.Info("adding global tools",
		slog.Int("tool_count", len(tools)),
		slog.Any("tool_names", toolNames))
	l.server.AddTools(tools...)
}

func (l *LoggingMCPServer) DeleteTools(names ...string) {
	l.logger.Info("deleting global tools",
		slog.Any("tool_names", names))
	l.server.DeleteTools(names...)
}

func (l *LoggingMCPServer) AddPrompt(prompt mcp.Prompt, handler server.PromptHandlerFunc) {
	l.logger.Info("adding prompt",
		slog.String("prompt_name", prompt.Name),
		slog.String("prompt_description", prompt.Description))
	l.server.AddPrompt(prompt, handler)
}

func (l *LoggingMCPServer) AddPrompts(prompts ...server.ServerPrompt) {
	promptNames := make([]string, len(prompts))
	for i, prompt := range prompts {
		promptNames[i] = prompt.Prompt.Name
	}

	l.logger.Info("adding prompts",
		slog.Int("prompt_count", len(prompts)),
		slog.Any("prompt_names", promptNames))
	l.server.AddPrompts(prompts...)
}

func (l *LoggingMCPServer) DeletePrompts(names ...string) {
	l.logger.Info("deleting prompts",
		slog.Any("prompt_names", names))
	l.server.DeletePrompts(names...)
}

func (l *LoggingMCPServer) AddResource(resource mcp.Resource, handler server.ResourceHandlerFunc) {
	l.logger.Info("adding resource",
		slog.String("resource_uri", resource.URI),
		slog.String("resource_name", resource.Name),
		slog.String("resource_description", resource.Description))
	l.server.AddResource(resource, handler)
}

func (l *LoggingMCPServer) AddResources(resources ...server.ServerResource) {
	resourceNames := make([]string, len(resources))
	resourceURIs := make([]string, len(resources))
	for i, resource := range resources {
		resourceNames[i] = resource.Resource.Name
		resourceURIs[i] = resource.Resource.URI
	}

	l.logger.Info("adding resources",
		slog.Int("resource_count", len(resources)),
		slog.Any("resource_names", resourceNames),
		slog.Any("resource_uris", resourceURIs))
	l.server.AddResources(resources...)
}

func (l *LoggingMCPServer) RemoveResource(uri string) {
	l.logger.Info("removing resource",
		slog.String("resource_uri", uri))
	l.server.RemoveResource(uri)
}

func (l *LoggingMCPServer) AddResourceTemplate(template mcp.ResourceTemplate, handler server.ResourceTemplateHandlerFunc) {
	l.logger.Info("adding resource template",
		slog.String("template_name", template.Name),
		slog.String("template_uri_pattern", template.URITemplate.Raw()),
		slog.String("template_description", template.Description))
	l.server.AddResourceTemplate(template, handler)
}

func main() {
	// Configure structured logging with slog
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
	}))

	// Create the base MCP server with tools and resources
	mcpServer := server.NewMCPServer("example-server", "1.0.0",
		server.WithResourceCapabilities(true, true),
		server.WithToolCapabilities(true),
		server.WithPromptCapabilities(true),
	)

	// Add some example tools
	mcpServer.AddTool(
		mcp.NewTool("time", mcp.WithDescription("Get current time")),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			logger.InfoContext(ctx, "time tool called")
			return mcp.NewToolResultText("Current time: " + time.Now().Format(time.RFC3339)), nil
		},
	)

	// Add example resource
	mcpServer.AddResource(
		mcp.NewResource("example://info", "Server Info", mcp.WithResourceDescription("Information about this server")),
		func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			logger.InfoContext(ctx, "info resource accessed")
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      "example://info",
					MIMEType: "text/plain",
					Text:     "This is an example MCP server with logging",
				},
			}, nil
		},
	)

	// Wrap the server with logging
	customLoggingServer := NewLoggingMCPServer(mcpServer, logger)

	// Create the StreamableHTTP server with the logging wrapper
	httpServer := server.NewStreamableHTTPServer(customLoggingServer,
		server.WithEndpointPath("/mcp"),
		server.WithHeartbeatInterval(30*time.Second),
	)

	logger.Info("starting MCP server",
		slog.String("address", ":8080"),
		slog.String("endpoint", "/mcp"))

	// Start server in a goroutine
	go func() {
		if err := httpServer.Start(":8080"); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed to start", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("server shutdown failed", slog.String("error", err.Error()))
	} else {
		logger.Info("server shutdown complete")
	}
}
