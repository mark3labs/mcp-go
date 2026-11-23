package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrimitiveItemRequired(t *testing.T) {
	for _, item := range []PropertyOption{WithStringItems(Required()), WithNumberItems(Required()), WithIntegerItems(Required()), WithBooleanItems(Required())} {
		tool := NewTool("example", WithArray("items", Required(), item))
		property := tool.InputSchema.Properties["items"].(map[string]any)
		require.NotContains(t, property["items"], "required")
		require.Equal(t, []string{"items"}, tool.InputSchema.Required)
	}
}
