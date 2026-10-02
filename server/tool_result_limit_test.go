package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestToolResultSizeLimit_TaskTool(t *testing.T) {
	largeText := mcp.NewTextContent(strings.Repeat("a", 4096))
	small := &mcp.CreateTaskResult{
		Content:           []mcp.Content{mcp.NewTextContent("hi")},
		StructuredContent: map[string]any{"value": "hi"},
		Result:            mcp.Result{Meta: mcp.NewMetaFromMap(map[string]any{"custom": "hi"})},
		IsError:           true,
	}
	encoded, err := json.Marshal(&mcp.CallToolResult{
		Result: small.Result, Content: small.Content,
		StructuredContent: small.StructuredContent, IsError: small.IsError,
	})
	require.NoError(t, err)
	sentinel := errors.New("handler failed")
	tests := []struct {
		name    string
		limit   int
		result  *mcp.CreateTaskResult
		err     error
		dropped bool
	}{
		{name: "oversized text", limit: 256, result: &mcp.CreateTaskResult{Content: []mcp.Content{largeText}}, dropped: true},
		{name: "oversized structured content", limit: 256, result: &mcp.CreateTaskResult{StructuredContent: strings.Repeat("a", 4096)}, dropped: true},
		{name: "oversized metadata", limit: 256, result: &mcp.CreateTaskResult{Result: mcp.Result{Meta: mcp.NewMetaFromMap(map[string]any{"custom": strings.Repeat("a", 4096)})}}, dropped: true},
		{name: "exact limit preserves payload", limit: len(encoded), result: small},
		{name: "one byte over limit", limit: len(encoded) - 1, result: small, dropped: true},
		{name: "zero disables limit", result: &mcp.CreateTaskResult{Content: []mcp.Content{largeText}}},
		{name: "negative disables limit", limit: -1, result: &mcp.CreateTaskResult{Content: []mcp.Content{largeText}}},
		{name: "unencodable result", limit: 256, result: &mcp.CreateTaskResult{StructuredContent: make(chan int)}},
		{name: "nil result", limit: 256},
		{name: "handler error", limit: 256, result: &mcp.CreateTaskResult{Content: []mcp.Content{largeText}}, err: sentinel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewMCPServer("test", "1.0.0", WithToolResultSizeLimit(tt.limit))
			tool := mcp.NewTool("task_tool", mcp.WithTaskSupport(mcp.TaskSupportRequired))
			srv.AddTaskTool(tool, func(context.Context, mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
				return tt.result, tt.err
			})
			entry, err := srv.createTask(t.Context(), "test-task", tool.Name, nil, nil)
			require.NoError(t, err)
			srv.executeTaskTool(t.Context(), entry, srv.taskTools[tool.Name], mcp.CallToolRequest{
				Params: mcp.CallToolParams{Name: tool.Name},
			})
			require.True(t, entry.completed)
			if tt.err != nil {
				require.ErrorIs(t, entry.resultErr, sentinel)
				require.Equal(t, mcp.TaskStatusFailed, entry.task.Status)
				return
			}
			require.NoError(t, entry.resultErr)
			require.Equal(t, mcp.TaskStatusCompleted, entry.task.Status)
			if !tt.dropped {
				require.Same(t, tt.result, entry.result)
				return
			}
			stored, ok := entry.result.(*mcp.CallToolResult)
			require.True(t, ok, "task store must contain the replacement tool error, got %T", entry.result)
			require.True(t, stored.IsError)
			require.Nil(t, stored.StructuredContent)
			require.Nil(t, stored.Meta)
			result, reqErr := srv.handleTaskResult(t.Context(), 1, mcp.TaskResultRequest{
				Params: mcp.TaskResultParams{TaskId: entry.task.TaskId},
			})
			require.Nil(t, reqErr)
			require.True(t, result.IsError)
			require.Equal(t, stored.Content, result.Content)
			text, ok := result.Content[0].(mcp.TextContent)
			require.True(t, ok)
			require.Contains(t, text.Text, "too large")
			require.Contains(t, text.Text, tool.Name)
		})
	}
}

func TestToolResultSizeLimit_RepeatedOptions(t *testing.T) {
	payload := mcp.NewToolResultText(strings.Repeat("a", 500))
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	tests := []struct {
		name    string
		limits  []int
		dropped bool
	}{
		{name: "loosen", limits: []int{64, 4096}},
		{name: "tighten", limits: []int{4096, 64}, dropped: true},
		{name: "repeat positive", limits: []int{64, 64}, dropped: true},
		{name: "disable with zero", limits: []int{64, 0}},
		{name: "disable with negative", limits: []int{64, -1}},
		{name: "re-enable loose after zero", limits: []int{64, 0, 4096}},
		{name: "re-enable loose after negative", limits: []int{64, -1, 4096}},
		{name: "re-enable strict after zero", limits: []int{4096, 0, 64}, dropped: true},
		{name: "re-enable strict after negative", limits: []int{4096, -1, 64}, dropped: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, taskOnly := range []bool{false, true} {
				name := "regular"
				if taskOnly {
					name = "task-only"
				}
				t.Run(name, func(t *testing.T) {
					options := make([]ServerOption, 0, len(tt.limits))
					for _, limit := range tt.limits {
						options = append(options, WithToolResultSizeLimit(limit))
					}
					srv := NewMCPServer("test", "1.0.0", options...)
					var result *mcp.CallToolResult
					if taskOnly {
						tool := mcp.NewTool("sized", mcp.WithTaskSupport(mcp.TaskSupportRequired))
						taskResult := &mcp.CreateTaskResult{Content: payload.Content}
						srv.AddTaskTool(tool, func(context.Context, mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
							return taskResult, nil
						})
						entry, err := srv.createTask(t.Context(), "test-task", tool.Name, nil, nil)
						require.NoError(t, err)
						srv.executeTaskTool(t.Context(), entry, srv.taskTools[tool.Name], mcp.CallToolRequest{
							Params: mcp.CallToolParams{Name: tool.Name},
						})
						require.True(t, entry.completed)
						require.NoError(t, entry.resultErr)
						require.Equal(t, mcp.TaskStatusCompleted, entry.task.Status)
						if !tt.dropped {
							require.Same(t, taskResult, entry.result)
						}
						taskResponse, reqErr := srv.handleTaskResult(t.Context(), 1, mcp.TaskResultRequest{
							Params: mcp.TaskResultParams{TaskId: entry.task.TaskId},
						})
						require.Nil(t, reqErr)
						result = &mcp.CallToolResult{Content: taskResponse.Content, IsError: taskResponse.IsError}
					} else {
						addSizedTool(t, srv, "sized", 500)
						response := callTool(t, srv, "sized", map[string]any{})
						if tt.dropped {
							requireToolErrorContaining(t, response, "too large")
							result = response.(mcp.JSONRPCResponse).Result.(*mcp.CallToolResult)
						} else {
							result = requireToolSuccess(t, response)
						}
					}
					require.Equal(t, tt.dropped, result.IsError)
					if tt.dropped {
						text, ok := result.Content[0].(mcp.TextContent)
						require.True(t, ok)
						require.Contains(t, text.Text, fmt.Sprintf("%d bytes exceeds the configured limit of %d bytes", len(encoded), tt.limits[len(tt.limits)-1]))
					} else {
						require.Equal(t, payload.Content, result.Content)
					}
				})
			}
		})
	}
}

func TestToolResultSizeLimit_RepeatedOptionsPreserveMiddlewareOrder(t *testing.T) {
	var events []string
	observe := func(name string) ToolHandlerMiddleware {
		return func(next ToolHandlerFunc) ToolHandlerFunc {
			return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				events = append(events, name+" before")
				result, err := next(ctx, request)
				events = append(events, fmt.Sprintf("%s after: IsError=%t", name, result.IsError))
				return result, err
			}
		}
	}
	srv := NewMCPServer("test", "1.0.0",
		WithToolHandlerMiddleware(observe("outer")),
		WithToolResultSizeLimit(64),
		WithToolHandlerMiddleware(observe("inner")),
		WithToolResultSizeLimit(4096),
	)
	addSizedTool(t, srv, "big", 5000)
	requireToolErrorContaining(t, callTool(t, srv, "big", map[string]any{}), "configured limit of 4096 bytes")
	require.Equal(t, []string{"outer before", "inner before", "inner after: IsError=false", "outer after: IsError=true"}, events)
}
