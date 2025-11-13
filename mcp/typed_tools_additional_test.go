package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTypedToolHandler(t *testing.T) {
	type Args struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	t.Run("successful execution", func(t *testing.T) {
		handler := func(ctx context.Context, request CallToolRequest, args Args) (*CallToolResult, error) {
			return NewToolResultText("Name: " + args.Name), nil
		}

		typedHandler := NewTypedToolHandler(handler)

		req := CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"name":  "test",
			"count": 5,
		}

		result, err := typedHandler(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Len(t, result.Content, 1)
		textContent, ok := result.Content[0].(TextContent)
		require.True(t, ok)
		assert.Equal(t, "Name: test", textContent.Text)
	})

	t.Run("bind arguments error", func(t *testing.T) {
		handler := func(ctx context.Context, request CallToolRequest, args Args) (*CallToolResult, error) {
			return NewToolResultText("Should not reach here"), nil
		}

		typedHandler := NewTypedToolHandler(handler)

		req := CallToolRequest{}
		req.Params.Arguments = "invalid arguments" // Not a map

		result, err := typedHandler(context.Background(), req)
		require.NoError(t, err) // Handler returns result, not error
		require.NotNil(t, result)

		// Should return error result
		require.Len(t, result.Content, 1)
		textContent, ok := result.Content[0].(TextContent)
		require.True(t, ok)
		assert.Contains(t, textContent.Text, "failed to bind arguments")
	})

	t.Run("handler returns error", func(t *testing.T) {
		handler := func(ctx context.Context, request CallToolRequest, args Args) (*CallToolResult, error) {
			return nil, errors.New("handler error")
		}

		typedHandler := NewTypedToolHandler(handler)

		req := CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"name":  "test",
			"count": 5,
		}

		result, err := typedHandler(context.Background(), req)
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("with complex arguments", func(t *testing.T) {
		type ComplexArgs struct {
			Items   []string          `json:"items"`
			Options map[string]string `json:"options"`
			Nested  struct {
				Value int `json:"value"`
			} `json:"nested"`
		}

		handler := func(ctx context.Context, request CallToolRequest, args ComplexArgs) (*CallToolResult, error) {
			return NewToolResultText("OK"), nil
		}

		typedHandler := NewTypedToolHandler(handler)

		req := CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"items":   []any{"a", "b", "c"},
			"options": map[string]any{"key": "value"},
			"nested":  map[string]any{"value": 42},
		}

		result, err := typedHandler(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, result)
	})
}

func TestNewStructuredToolHandler(t *testing.T) {
	type Args struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}

	type Result struct {
		Results []string `json:"results"`
		Count   int      `json:"count"`
	}

	t.Run("successful execution", func(t *testing.T) {
		handler := func(ctx context.Context, request CallToolRequest, args Args) (Result, error) {
			return Result{
				Results: []string{"result1", "result2"},
				Count:   2,
			}, nil
		}

		structuredHandler := NewStructuredToolHandler(handler)

		req := CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"query": "test",
			"limit": 10,
		}

		result, err := structuredHandler(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Should have text content (JSON fallback)
		require.Len(t, result.Content, 1)
		textContent, ok := result.Content[0].(TextContent)
		require.True(t, ok)

		// Text should be JSON representation
		var jsonResult map[string]any
		err = json.Unmarshal([]byte(textContent.Text), &jsonResult)
		require.NoError(t, err)

		// Should have structured content
		require.NotNil(t, result.StructuredContent)
		structuredMap, ok := result.StructuredContent.(Result)
		require.True(t, ok)
		assert.Equal(t, 2, structuredMap.Count)
		assert.Len(t, structuredMap.Results, 2)
	})

	t.Run("bind arguments error", func(t *testing.T) {
		handler := func(ctx context.Context, request CallToolRequest, args Args) (Result, error) {
			return Result{}, errors.New("should not reach here")
		}

		structuredHandler := NewStructuredToolHandler(handler)

		req := CallToolRequest{}
		req.Params.Arguments = "invalid" // Not a map

		result, err := structuredHandler(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Should return error result
		require.Len(t, result.Content, 1)
		textContent, ok := result.Content[0].(TextContent)
		require.True(t, ok)
		assert.Contains(t, textContent.Text, "failed to bind arguments")
	})

	t.Run("handler execution error", func(t *testing.T) {
		handler := func(ctx context.Context, request CallToolRequest, args Args) (Result, error) {
			return Result{}, errors.New("execution failed")
		}

		structuredHandler := NewStructuredToolHandler(handler)

		req := CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"query": "test",
			"limit": 10,
		}

		result, err := structuredHandler(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Should return error result
		require.Len(t, result.Content, 1)
		textContent, ok := result.Content[0].(TextContent)
		require.True(t, ok)
		assert.Contains(t, textContent.Text, "tool execution failed")
		assert.Contains(t, textContent.Text, "execution failed")
	})

	t.Run("with primitive result", func(t *testing.T) {
		handler := func(ctx context.Context, request CallToolRequest, args Args) (string, error) {
			return "simple result", nil
		}

		structuredHandler := NewStructuredToolHandler(handler)

		req := CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"query": "test",
			"limit": 10,
		}

		result, err := structuredHandler(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.NotNil(t, result.StructuredContent)
		strResult, ok := result.StructuredContent.(string)
		require.True(t, ok)
		assert.Equal(t, "simple result", strResult)
	})

	t.Run("with map result", func(t *testing.T) {
		handler := func(ctx context.Context, request CallToolRequest, args Args) (map[string]any, error) {
			return map[string]any{
				"status": "success",
				"data":   []int{1, 2, 3},
			}, nil
		}

		structuredHandler := NewStructuredToolHandler(handler)

		req := CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"query": "test",
			"limit": 10,
		}

		result, err := structuredHandler(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.NotNil(t, result.StructuredContent)
		mapResult, ok := result.StructuredContent.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "success", mapResult["status"])
	})

	t.Run("with empty struct args", func(t *testing.T) {
		type EmptyArgs struct{}

		handler := func(ctx context.Context, request CallToolRequest, args EmptyArgs) (string, error) {
			return "no args needed", nil
		}

		structuredHandler := NewStructuredToolHandler(handler)

		req := CallToolRequest{}
		req.Params.Arguments = map[string]any{}

		result, err := structuredHandler(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, result)
	})
}

func TestTypedToolHandler_ContextPropagation(t *testing.T) {
	type Args struct {
		Value string `json:"value"`
	}

	type contextKey string

	t.Run("context is passed to handler", func(t *testing.T) {
		ctxKey := contextKey("test-key")
		ctxValue := "test-value"

		handler := func(ctx context.Context, request CallToolRequest, args Args) (*CallToolResult, error) {
			// Verify context value is available
			val := ctx.Value(ctxKey)
			if val == nil {
				return NewToolResultError("context value missing"), nil
			}
			return NewToolResultText("context value: " + val.(string)), nil
		}

		typedHandler := NewTypedToolHandler(handler)

		req := CallToolRequest{}
		req.Params.Arguments = map[string]any{"value": "test"}

		ctx := context.WithValue(context.Background(), ctxKey, ctxValue)
		result, err := typedHandler(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Len(t, result.Content, 1)
		textContent, ok := result.Content[0].(TextContent)
		require.True(t, ok)
		assert.Equal(t, "context value: test-value", textContent.Text)
	})

	t.Run("cancelled context", func(t *testing.T) {
		handler := func(ctx context.Context, request CallToolRequest, args Args) (*CallToolResult, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
				return NewToolResultText("completed"), nil
			}
		}

		typedHandler := NewTypedToolHandler(handler)

		req := CallToolRequest{}
		req.Params.Arguments = map[string]any{"value": "test"}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		result, err := typedHandler(ctx, req)
		assert.Error(t, err)
		assert.Nil(t, result)
	})
}
