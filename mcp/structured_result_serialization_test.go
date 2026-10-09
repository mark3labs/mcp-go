package mcp

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStructuredOnlySerializationFailureIsToolError(t *testing.T) {
	cycle := map[string]any{}
	cycle["self"] = cycle
	tests := []struct {
		name  string
		value any
	}{
		{"nonfinite calculated value", map[string]any{"score": math.NaN()}},
		{"cyclic graph", cycle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewToolResultStructuredOnly(tt.value)
			assert.True(t, result.IsError)
			assert.Nil(t, result.StructuredContent)
			_, err := json.Marshal(result)
			require.NoError(t, err)
			require.Len(t, result.Content, 1)
			assert.Contains(t, result.Content[0].(TextContent).Text, "serializing structured content")
		})
	}
}
