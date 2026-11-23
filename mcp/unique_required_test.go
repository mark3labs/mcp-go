package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUniqueRequired(t *testing.T) {
	tool := NewTool("example", WithString("name", Required()), WithInteger("count", Required()), WithString("name", Required(), Description("Display name")))
	require.Equal(t, []string{"name", "count"}, tool.InputSchema.Required)
	require.Equal(t, "Display name", tool.InputSchema.Properties["name"].(map[string]any)["description"])
}
