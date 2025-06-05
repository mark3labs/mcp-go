package server

import (
	"context"
	"fmt"
	"github.com/mark3labs/mcp-go/mcp"
)

type requestIDKey struct{}

// RequestSession represents an exchange with MCP client, and provides
// methods to interact with the client and query its capabilities.
type RequestSession struct {
	progressToken *mcp.ProgressToken
}

func NewRequestSession(requestParamMeta *mcp.Meta) RequestSession {
	requestSession := RequestSession{}

	// server should send progress notification if request metadata includes a progressToken
	if requestParamMeta != nil && requestParamMeta.ProgressToken != nil {
		requestSession.progressToken = &requestParamMeta.ProgressToken
	}

	return requestSession
}

// IsLoggingNotificationSupported returns true if server supports logging notification
func (exchange *RequestSession) IsLoggingNotificationSupported(ctx context.Context) bool {
	mcpServer := ServerFromContext(ctx)
	return mcpServer != nil && mcpServer.capabilities.logging != nil && *mcpServer.capabilities.logging
}

// SendLoggingNotification send logging notification to client.
// If server does not support logging notification, this method will do nothing.
func (exchange *RequestSession) SendLoggingNotification(ctx context.Context, level mcp.LoggingLevel, message map[string]any) error {
	if !exchange.IsLoggingNotificationSupported(ctx) {
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

	mcpServer := ServerFromContext(ctx)
	return mcpServer.SendNotificationToClient(
		ctx,
		string(mcp.MethodNotificationMessage),
		params,
	)
}

// SendProgressNotification send progress notification only if the client has requested progress
func (exchange *RequestSession) SendProgressNotification(ctx context.Context, progress float64, total *float64, message *string) error {
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

	mcpServer := ServerFromContext(ctx)
	return mcpServer.SendNotificationToClient(
		ctx,
		string(mcp.MethodNotificationProgress),
		params,
	)
}

// SendCancellationNotification send cancellation notification to client
func (exchange *RequestSession) SendCancellationNotification(ctx context.Context, reason *string) error {
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

	mcpServer := ServerFromContext(ctx)
	return mcpServer.SendNotificationToClient(
		ctx,
		string(mcp.MethodNotificationCancellation),
		params,
	)
}

// TODO should implement other methods like 'roots/list', this still could happen when server handle client request
