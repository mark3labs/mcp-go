package server

import (
	"context"
	"fmt"
	"github.com/mark3labs/mcp-go/mcp"
)

// TypedToolHandlerFunc is a function that handles a tool call with typed arguments
type TypedToolHandlerFunc[T any] func(ctx context.Context, requestSession RequestSession, request mcp.CallToolRequest, args T) (*mcp.CallToolResult, error)

// NewTypedToolHandler creates a ToolHandlerFunc that automatically binds arguments to a typed struct
func NewTypedToolHandler[T any](handler TypedToolHandlerFunc[T]) func(ctx context.Context, requestSession RequestSession, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, requestSession RequestSession, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args T
		if err := request.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to bind arguments: %v", err)), nil
		}
		return handler(ctx, requestSession, request, args)
	}
}
