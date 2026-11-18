package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseToolResultContentPreservesEmptyArray(t *testing.T) {
	tests := []struct {
		name string
		wire string
	}{
		{"empty", `{"type":"tool_result","toolUseId":"call_1","content":[]}`},
		{"structured", `{"type":"tool_result","toolUseId":"call_1","content":[],"structuredContent":{"count":0}}`},
		{"nonempty", `{"type":"tool_result","toolUseId":"call_1","content":[{"type":"text","text":"done"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var input map[string]any
			require.NoError(t, json.Unmarshal([]byte(tt.wire), &input))
			parsed, err := ParseContent(input)
			require.NoError(t, err)
			encoded, err := json.Marshal(parsed)
			require.NoError(t, err)
			assert.JSONEq(t, tt.wire, string(encoded))
		})
	}
}
