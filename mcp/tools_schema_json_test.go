package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func wireSchemas(t *testing.T, tool Tool) (input, output json.RawMessage) {
	t.Helper()
	b, err := json.Marshal(tool)
	require.NoError(t, err)
	var v struct {
		InputSchema  json.RawMessage `json:"inputSchema"`
		OutputSchema json.RawMessage `json:"outputSchema"`
	}
	require.NoError(t, json.Unmarshal(b, &v))
	return v.InputSchema, v.OutputSchema
}

func decodedTool(t *testing.T, s string) Tool {
	t.Helper()
	var tool Tool
	require.NoError(t, json.Unmarshal([]byte(s), &tool))
	return tool
}

func TestTool_SchemaJSONMatchesWireForm(t *testing.T) {
	rawIn := json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"x-custom":true}`)
	rawOut := json.RawMessage(`{"type":"object","properties":{"n":{"type":"number"}}}`)
	tests := []struct {
		name string
		tool Tool
	}{
		{name: "structured input", tool: NewTool("search", WithString("q", Required()))},
		{name: "raw input", tool: NewToolWithRawSchema("search", "", rawIn)},
		{name: "structured output", tool: NewTool("count", WithOutputSchema[struct {
			N int `json:"n"`
		}]())},
		{name: "raw output", tool: NewTool("count", WithRawOutputSchema(rawOut))},
		{name: "decoded from a server", tool: decodedTool(t, `{"name":"search","inputSchema":{"type":"object","properties":{"q":{"type":"string","minLength":1}},"$defs":{"d":{"type":"string"}}},"outputSchema":{"type":"object","properties":{"hits":{"type":"array"}}}}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantIn, wantOut := wireSchemas(t, tt.tool)

			in, err := tt.tool.InputSchemaJSON()
			require.NoError(t, err)
			assert.JSONEq(t, string(wantIn), string(in))

			out, err := tt.tool.OutputSchemaJSON()
			require.NoError(t, err)
			if wantOut == nil {
				assert.Nil(t, out)
				return
			}
			assert.JSONEq(t, string(wantOut), string(out))
		})
	}
}

func TestTool_SchemaJSONWithoutOutputSchema(t *testing.T) {
	out, err := NewTool("ping").OutputSchemaJSON()
	require.NoError(t, err)
	assert.Nil(t, out)
}

func TestTool_SchemaJSONConflicts(t *testing.T) {
	raw := json.RawMessage(`{"type":"object"}`)
	structured := []struct {
		name   string
		fields ToolArgumentsSchema
	}{
		{name: "Type", fields: ToolArgumentsSchema{Type: "object"}},
		{name: "Types", fields: ToolArgumentsSchema{Types: []string{"object", "null"}}},
		{name: "Properties", fields: ToolArgumentsSchema{Properties: map[string]any{"q": map[string]any{"type": "string"}}}},
		{name: "Required", fields: ToolArgumentsSchema{Required: []string{"q"}}},
		{name: "Defs", fields: ToolArgumentsSchema{Defs: map[string]any{"d": map[string]any{"type": "string"}}}},
		{name: "AdditionalProperties", fields: ToolArgumentsSchema{AdditionalProperties: false}},
	}
	for _, s := range structured {
		t.Run("input with "+s.name, func(t *testing.T) {
			tool := Tool{Name: "a", InputSchema: ToolInputSchema(s.fields), RawInputSchema: raw}
			_, err := tool.InputSchemaJSON()
			require.ErrorIs(t, err, errToolSchemaConflict)
			_, err = json.Marshal(tool)
			require.ErrorIs(t, err, errToolSchemaConflict)
		})
		t.Run("output with "+s.name, func(t *testing.T) {
			tool := Tool{Name: "a", OutputSchema: ToolOutputSchema(s.fields), RawOutputSchema: raw}
			_, err := tool.OutputSchemaJSON()
			require.ErrorIs(t, err, errToolSchemaConflict)
			_, err = json.Marshal(tool)
			require.ErrorIs(t, err, errToolSchemaConflict)
		})
	}
}

func TestTool_SchemaJSONReturnsACopy(t *testing.T) {
	tool := NewToolWithRawSchema("search", "", json.RawMessage(`{"type":"object"}`))
	in, err := tool.InputSchemaJSON()
	require.NoError(t, err)
	in[0] = 'x'
	assert.JSONEq(t, `{"type":"object"}`, string(tool.RawInputSchema))
}
