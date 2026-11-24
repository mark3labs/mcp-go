package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSamplingResultShape(t *testing.T) {
	for _, invalid := range []any{"answer", map[string]any{"type": "text", "text": "answer"}} {
		_, err := ParseSamplingContent(map[string]any{"type": "tool_result", "toolUseId": "a", "content": invalid})
		require.Error(t, err)
	}
}
