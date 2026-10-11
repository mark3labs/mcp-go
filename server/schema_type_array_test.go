package server

import (
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaTypeArrayValidation(t *testing.T) {
	tool := mcp.Tool{
		Name:         "array_types",
		InputSchema:  mcp.ToolInputSchema{Types: []string{"object", "null"}},
		OutputSchema: mcp.ToolOutputSchema{Types: []string{"object", "null"}},
	}
	tests := []struct {
		name      string
		getSchema func(mcp.Tool) ([]byte, bool)
	}{
		{name: "input", getSchema: schemaJSONFor},
		{name: "output", getSchema: outputSchemaJSONFor},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, ok := tt.getSchema(tool)
			require.True(t, ok, "array-only schema must not be treated as absent")
			compiled, err := compileSchema(tool.Name, data)
			require.NoError(t, err)
			assert.NoError(t, compiled.Validate(map[string]any{}))
			assert.NoError(t, compiled.Validate(nil))
			assert.Error(t, compiled.Validate("not an object"))
			var wire map[string]any
			require.NoError(t, json.Unmarshal(data, &wire))
			assert.Equal(t, []any{"object", "null"}, wire["type"])
		})
	}
}
