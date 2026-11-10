package server

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestMCPServer_PaginationWithReorderedToolFilter(t *testing.T) {
	s := NewMCPServer("test", "1.0.0",
		WithToolCapabilities(true),
		WithPaginationLimit(1),
		WithToolFilter(func(_ context.Context, tools []mcp.Tool) []mcp.Tool {
			sort.Slice(tools, func(i, j int) bool { return tools[i].Name > tools[j].Name })
			return tools
		}),
	)
	for _, name := range []string{"a", "b", "c"} {
		s.AddTool(mcp.NewTool(name), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return nil, nil
		})
	}

	var got []string
	var cursor mcp.Cursor
	for page := 1; ; page++ {
		response := s.HandleMessage(t.Context(), fmt.Appendf(nil,
			`{"jsonrpc":"2.0","id":%d,"method":"tools/list","params":{"cursor":%q}}`, page, cursor))
		rpc, ok := response.(mcp.JSONRPCResponse)
		require.True(t, ok, "unexpected response: %+v", response)
		result, ok := rpc.Result.(mcp.ListToolsResult)
		require.True(t, ok, "unexpected result: %+v", rpc.Result)
		for _, tool := range result.Tools {
			got = append(got, tool.Name)
		}
		cursor = result.NextCursor
		if cursor == "" {
			break
		}
	}
	require.Equal(t, []string{"a", "b", "c"}, got)
}

func TestMCPServer_PaginationWithReorderedPromptFilter(t *testing.T) {
	s := NewMCPServer("test", "1.0.0",
		WithPromptCapabilities(true),
		WithPaginationLimit(1),
		WithPromptFilter(func(_ context.Context, prompts []mcp.Prompt) []mcp.Prompt {
			sort.Slice(prompts, func(i, j int) bool { return prompts[i].Name > prompts[j].Name })
			return prompts
		}),
	)
	for _, name := range []string{"a", "b", "c"} {
		s.AddPrompt(mcp.NewPrompt(name), func(context.Context, mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return nil, nil
		})
	}

	var got []string
	var cursor mcp.Cursor
	for page := 1; ; page++ {
		response := s.HandleMessage(t.Context(), fmt.Appendf(nil,
			`{"jsonrpc":"2.0","id":%d,"method":"prompts/list","params":{"cursor":%q}}`, page, cursor))
		rpc, ok := response.(mcp.JSONRPCResponse)
		require.True(t, ok, "unexpected response: %+v", response)
		result, ok := rpc.Result.(mcp.ListPromptsResult)
		require.True(t, ok, "unexpected result: %+v", rpc.Result)
		for _, prompt := range result.Prompts {
			got = append(got, prompt.Name)
		}
		cursor = result.NextCursor
		if cursor == "" {
			break
		}
	}
	require.Equal(t, []string{"a", "b", "c"}, got)
}
