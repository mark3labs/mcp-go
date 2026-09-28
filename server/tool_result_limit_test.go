package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

// addSizedTool registers a tool whose handler returns a text result of exactly
// textLen bytes, so tests can straddle the configured size limit.
func addSizedTool(t *testing.T, srv *MCPServer, name string, textLen int) {
	t.Helper()
	srv.AddTool(mcp.NewTool(name), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText(strings.Repeat("a", textLen)), nil
	})
}

func TestToolResultSizeLimit_UnderLimitPassesThrough(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithToolResultSizeLimit(4096))
	addSizedTool(t, srv, "small", 10)

	result := requireToolSuccess(t, callTool(t, srv, "small", map[string]any{}))
	require.Len(t, result.Content, 1)
	tc, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok, "expected TextContent, got %T", result.Content[0])
	require.Equal(t, strings.Repeat("a", 10), tc.Text)
}

func TestToolResultSizeLimit_OverLimitReturnsError(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithToolResultSizeLimit(64))
	addSizedTool(t, srv, "big", 4096)

	// The oversized payload is dropped and replaced by a small tool execution
	// error, not truncated.
	requireToolErrorContaining(t, callTool(t, srv, "big", map[string]any{}), "too large")
}

func TestToolResultSizeLimit_DisabledWhenNonPositive(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithToolResultSizeLimit(0))
	addSizedTool(t, srv, "big", 4096)

	result := requireToolSuccess(t, callTool(t, srv, "big", map[string]any{}))
	tc, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok, "expected TextContent, got %T", result.Content[0])
	require.Len(t, tc.Text, 4096, "guard must be inert when the limit is non-positive")
}

func TestToolResultSizeLimitMiddleware(t *testing.T) {
	mw := toolResultSizeLimitMiddleware(256)

	t.Run("propagates handler error", func(t *testing.T) {
		sentinel := errors.New("boom")
		next := mw(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return nil, sentinel
		})
		res, err := next(t.Context(), mcp.CallToolRequest{})
		require.ErrorIs(t, err, sentinel)
		require.Nil(t, res)
	})

	t.Run("passes through small result", func(t *testing.T) {
		next := mw(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("hi"), nil
		})
		res, err := next(t.Context(), mcp.CallToolRequest{})
		require.NoError(t, err)
		require.False(t, res.IsError)
	})

	t.Run("replaces oversized result with a small error", func(t *testing.T) {
		var req mcp.CallToolRequest
		req.Params.Name = "bigtool"
		next := mw(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText(strings.Repeat("x", 4096)), nil
		})
		res, err := next(t.Context(), req)
		require.NoError(t, err)
		require.True(t, res.IsError)

		enc, mErr := json.Marshal(res)
		require.NoError(t, mErr)
		require.Less(t, len(enc), 512, "replacement error result must itself be small")
		require.Contains(t, string(enc), "bigtool")
	})
}
