package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentPointers(t *testing.T) {
	block := NewTextContent("hello")
	var content Content = &block
	parsed, ok := AsTextContent(content)
	require.True(t, ok)
	require.Same(t, &block, parsed)
	require.Equal(t, "hello", GetTextFromContent(content))
	var absent *TextContent
	_, ok = AsTextContent(absent)
	require.False(t, ok)
}
