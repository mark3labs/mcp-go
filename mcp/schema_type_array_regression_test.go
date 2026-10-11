package mcp

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListToolsSchemaTypeArrayRegression(t *testing.T) {
	tests := []struct {
		name       string
		schemaType string
	}{
		{name: "existing string type", schemaType: `"object"`},
		{name: "nullable object type", schemaType: `["object","null"]`},
		{name: "single element array stays an array", schemaType: `["object"]`},
		{name: "scalar union", schemaType: `["string","number","boolean"]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := `{"tools":[{"name":"get_me","inputSchema":{"type":"object"},"outputSchema":{"type":` + tt.schemaType + `}}]}`
			var result ListToolsResult
			require.NoError(t, json.Unmarshal([]byte(data), &result))
			require.Len(t, result.Tools, 1)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			var output struct {
				Tools []struct {
					OutputSchema struct {
						Type json.RawMessage `json:"type"`
					} `json:"outputSchema"`
				} `json:"tools"`
			}
			require.NoError(t, json.Unmarshal(encoded, &output))
			require.Len(t, output.Tools, 1)
			assert.JSONEq(t, tt.schemaType, string(output.Tools[0].OutputSchema.Type))
		})
	}
}

func TestSchemaTypeArrayRepresentation(t *testing.T) {
	var schema ToolArgumentsSchema
	require.NoError(t, json.Unmarshal([]byte(`{"type":["object","null"]}`), &schema))
	assert.Empty(t, schema.Type)
	assert.Equal(t, []string{"object", "null"}, schema.Types)
	require.NoError(t, json.Unmarshal([]byte(`{"type":"string"}`), &schema))
	assert.Equal(t, "string", schema.Type)
	assert.Nil(t, schema.Types)
	require.NoError(t, json.Unmarshal([]byte(`{"type":["integer"]}`), &schema))
	assert.Empty(t, schema.Type)
	assert.Equal(t, []string{"integer"}, schema.Types)

	legacyType := "object"
	schema.Type = legacyType
	data, err := json.Marshal(schema)
	require.NoError(t, err)
	var encoded map[string]any
	require.NoError(t, json.Unmarshal(data, &encoded))
	assert.Equal(t, legacyType, encoded["type"])
}

func TestSchemaTypeArrayInputRoundTrip(t *testing.T) {
	var tool Tool
	require.NoError(t, json.Unmarshal([]byte(`{"name":"union","inputSchema":{"type":["object","null"],"properties":{"x":{"type":["string","null"]}},"required":["x"],"additionalProperties":false}}`), &tool))
	assert.Equal(t, []string{"object", "null"}, tool.InputSchema.Types)
	data, err := json.Marshal(tool)
	require.NoError(t, err)
	var decoded Tool
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, tool.InputSchema, decoded.InputSchema)
}

func TestSchemaTypeArrayGeneratedInputReplacement(t *testing.T) {
	type arguments struct {
		Name string `json:"name"`
	}
	for name, option := range map[string]ToolOption{
		"generated": WithInputSchema[arguments](),
		"cached":    WithCachedInputSchema[arguments](NewSchemaCache()),
	} {
		t.Run(name, func(t *testing.T) {
			tool := Tool{Name: "replacement", InputSchema: ToolInputSchema{Types: []string{"object", "null"}}}
			option(&tool)
			assert.Empty(t, tool.InputSchema.Type)
			assert.Nil(t, tool.InputSchema.Types)
			require.NotEmpty(t, tool.RawInputSchema)
			_, err := json.Marshal(tool)
			require.NoError(t, err)
		})
	}
}

func TestSchemaTypeArrayRejectsWrongJSONTypes(t *testing.T) {
	for _, raw := range []string{`1`, `true`, `{}`, `["object",1]`, `["object",{}]`} {
		t.Run(raw, func(t *testing.T) {
			var schema ToolArgumentsSchema
			assert.Error(t, json.Unmarshal([]byte(`{"type":`+raw+`}`), &schema))
		})
	}
}

func TestSchemaTypeArrayRawSchemaConflict(t *testing.T) {
	for _, output := range []bool{false, true} {
		t.Run(fmt.Sprint("output=", output), func(t *testing.T) {
			tool := NewTool("conflict")
			if output {
				tool.OutputSchema = ToolOutputSchema{Types: []string{"object", "null"}}
				tool.RawOutputSchema = json.RawMessage(`{"type":"object"}`)
			} else {
				tool.InputSchema = ToolInputSchema{Types: []string{"object", "null"}}
				tool.RawInputSchema = json.RawMessage(`{"type":"object"}`)
			}
			_, err := json.Marshal(tool)
			assert.ErrorIs(t, err, errToolSchemaConflict)
		})
	}
}
