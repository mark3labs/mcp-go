package server

import (
	"context"
	"fmt"
	"github.com/mark3labs/mcp-go/mcp"
)

type requestIDKey struct{}

// RequestContext represents an exchange with MCP client. The exchange provides
// methods to interact with the client and query its capabilities.
type RequestContext struct {
	mcpServer *MCPServer

	progressToken *mcp.ProgressToken
}

func NewRequestContext(mcpServer *MCPServer, requestParamMeta *mcp.Meta) RequestContext {
	requestContext := RequestContext{
		mcpServer: mcpServer,
	}

	// server should send progress notification if request metadata includes a progressToken
	if requestParamMeta != nil && requestParamMeta.ProgressToken != nil {
		requestContext.progressToken = &requestParamMeta.ProgressToken
	}

	return requestContext
}

// IsLoggingNotificationSupported returns true if server supports logging notification
func (exchange *RequestContext) IsLoggingNotificationSupported() bool {
	return exchange.mcpServer != nil && exchange.mcpServer.capabilities.logging != nil && *exchange.mcpServer.capabilities.logging
}

// SendLoggingNotification send logging notification to client.
// If server does not support logging notification, this method will do nothing.
func (exchange *RequestContext) SendLoggingNotification(ctx context.Context, level mcp.LoggingLevel, message map[string]any) error {
	if !exchange.IsLoggingNotificationSupported() {
		return nil
	}

	clientLogLevel := ClientSessionFromContext(ctx).GetLogLevel()
	allowed, err := clientLogLevel.Allows(level)
	if err != nil {
		return err
	}
	if !allowed {
		return nil
	}

	params := map[string]any{
		"level": level,
		"data":  message,
	}
	if ClientSessionFromContext(ctx).GetLoggerName() != nil {
		params["logger"] = *ClientSessionFromContext(ctx).GetLoggerName()
	}

	return exchange.mcpServer.SendNotificationToClient(
		ctx,
		string(mcp.MethodNotificationMessage),
		params,
	)
}

// SendProgressNotification send progress notification only if the client has requested progress
func (exchange *RequestContext) SendProgressNotification(ctx context.Context, progress, total *float64, message *string) error {
	if exchange.progressToken == nil {
		return nil
	}

	params := map[string]any{
		"progress":      progress,
		"progressToken": *exchange.progressToken,
	}
	if total != nil {
		params["total"] = *total
	}
	if message != nil {
		params["message"] = *message
	}

	return exchange.mcpServer.SendNotificationToClient(
		ctx,
		string(mcp.MethodNotificationProgress),
		params,
	)
}

// SendCancellationNotification send cancellation notification to client
func (exchange *RequestContext) SendCancellationNotification(ctx context.Context, reason *string) error {
	requestIDRawValue := ctx.Value(requestIDKey{})
	if requestIDRawValue == nil {
		return fmt.Errorf("invalid requestID")
	}

	requestID, ok := requestIDRawValue.(mcp.RequestId)
	if !ok {
		return fmt.Errorf("invalid requestID type")
	}

	params := map[string]any{
		"requestId": requestID.Value(),
	}
	if reason != nil {
		params["reason"] = reason
	}

	return exchange.mcpServer.SendNotificationToClient(
		ctx,
		string(mcp.MethodNotificationCancellation),
		params,
	)
}

// TODO should implement other methods like 'roots/list', this still could happen when server handle client request
