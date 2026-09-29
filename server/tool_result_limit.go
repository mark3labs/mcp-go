package server

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
)

// WithToolResultSizeLimit installs a server-wide guard that rejects tool
// results whose serialized size exceeds maxBytes. It protects AI clients from
// accidentally receiving a huge CallToolResult that would consume a large part
// of the model's context window or make the next model call unnecessarily
// expensive.
//
// The limit is opt-in: a maxBytes of zero or less disables the guard entirely
// and leaves existing behavior unchanged, so no middleware is installed.
//
// When a result exceeds the limit it is replaced by a small tool execution
// error (CallToolResult with IsError: true) that explains the overflow rather
// than being silently truncated. Silent truncation is avoided because a partial
// result can still look valid to the model.
//
// The size is measured as the number of bytes in the JSON encoding of the
// CallToolResult returned by the handler. Because the guard runs inside the
// tool handler chain, it measures the result before the JSON-RPC envelope and
// any protocol-version metadata (resultType, _meta.serverInfo on 2026-07-28 and
// later) are attached, so the response delivered to the client can be slightly
// larger than maxBytes. Treat the limit as a bound on the tool payload, not as
// an exact cap on the wire response.
func WithToolResultSizeLimit(maxBytes int) ServerOption {
	if maxBytes <= 0 {
		return func(*MCPServer) {}
	}
	return WithToolHandlerMiddleware(toolResultSizeLimitMiddleware(maxBytes))
}

// toolResultSizeLimitMiddleware returns a ToolHandlerMiddleware that enforces
// the given byte limit on the result produced by the wrapped handler.
func toolResultSizeLimitMiddleware(maxBytes int) ToolHandlerMiddleware {
	return func(next ToolHandlerFunc) ToolHandlerFunc {
		return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			result, err := next(ctx, request)
			if err != nil || result == nil {
				return result, err
			}

			encoded, mErr := json.Marshal(result)
			if mErr != nil {
				// The result cannot be measured; let it through unchanged
				// rather than blocking the call. Encoding will be attempted
				// again when the response is written to the client.
				return result, nil
			}

			if len(encoded) <= maxBytes {
				return result, nil
			}

			return mcp.NewToolResultErrorf(
				"tool result for %q is too large: %d bytes exceeds the configured limit of %d bytes; the result was dropped instead of truncated to avoid overloading the client context window",
				request.Params.Name,
				len(encoded),
				maxBytes,
			), nil
		}
	}
}
