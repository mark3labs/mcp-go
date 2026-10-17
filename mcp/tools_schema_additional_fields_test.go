package mcp

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListToolsAdditionalSchemaKeywords checks that typed tool listings retain input and output keywords.
func TestListToolsAdditionalSchemaKeywords(t *testing.T) {
	const input = `{"$schema":"https://json-schema.org/draft/2020-12/schema","title":"Order","type":"object","properties":{"sku":{"type":"string"},"quantity":{"type":"integer"}},"oneOf":[{"required":["sku"]},{"required":["quantity"]}]}`
	const output = `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","anyOf":[{"required":["id"]},{"required":["error"]}]}`
	var result ListToolsResult
	require.NoError(t, json.Unmarshal([]byte(`{"tools":[{"name":"place_order","inputSchema":`+input+`,"outputSchema":`+output+`}]}`), &result))
	require.Len(t, result.Tools, 1)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	var wire struct {
		Tools []struct {
			InputSchema  map[string]any `json:"inputSchema"`
			OutputSchema map[string]any `json:"outputSchema"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(encoded, &wire))
	require.Len(t, wire.Tools, 1)
	tests := []struct {
		name     string
		original string
		schema   map[string]any
	}{
		{"input", input, wire.Tools[0].InputSchema},
		{"output", output, wire.Tools[0].OutputSchema},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var expected map[string]any
			require.NoError(t, json.Unmarshal([]byte(tt.original), &expected))
			for key, value := range expected {
				assert.Equal(t, value, tt.schema[key], key)
			}
		})
	}
}

// TestSchemaAdditionalFieldsRoundTrip checks all schema types preserve arbitrary keyword values.
func TestSchemaAdditionalFieldsRoundTrip(t *testing.T) {
	const data = `{"type":"object","properties":{"x":{"type":"string"}},"required":["x"],"additionalProperties":false,"$defs":{"value":{"type":"string"}},"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:example","title":"Example","description":"Details","oneOf":[{"required":["x"]}],"anyOf":[true,false],"allOf":[{}],"if":{"required":["x"]},"then":{"title":"yes"},"else":{"title":"no"},"x-vendor":{"nested":[null,false,42]},"unknown-null":null,"unknown-bool":false,"unknown-number":1.25,"unknown-array":[1,"two"]}`
	tests := []struct {
		name   string
		schema func(*ToolArgumentsSchema) any
	}{
		{"arguments", func(s *ToolArgumentsSchema) any { return s }},
		{"input", func(s *ToolArgumentsSchema) any { return (*ToolInputSchema)(s) }},
		{"output", func(s *ToolArgumentsSchema) any { return (*ToolOutputSchema)(s) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var schema ToolArgumentsSchema
			target := tt.schema(&schema)
			require.NoError(t, json.Unmarshal([]byte(data), target))
			assert.Equal(t, "object", schema.Type)
			assert.Equal(t, []string{"x"}, schema.Required)
			assert.Equal(t, false, schema.AdditionalProperties)
			assert.Contains(t, schema.Properties, "x")
			assert.Contains(t, schema.Defs, "value")
			for _, key := range []string{"type", "properties", "required", "additionalProperties", "$defs", "definitions"} {
				assert.NotContains(t, schema.AdditionalFields, key)
			}
			encoded, err := json.Marshal(target)
			require.NoError(t, err)
			assert.JSONEq(t, data, string(encoded))
			require.NoError(t, json.Unmarshal(encoded, target))
			second, err := json.Marshal(target)
			require.NoError(t, err)
			assert.JSONEq(t, string(encoded), string(second))
		})
	}
}

// TestSchemaAdditionalFieldsCollisions checks structured fields always win without mutating extras.
func TestSchemaAdditionalFieldsCollisions(t *testing.T) {
	tests := []struct {
		name     string
		schema   ToolArgumentsSchema
		expected string
	}{
		{"structured", ToolArgumentsSchema{Type: "object", Properties: map[string]any{"x": true}, Required: []string{"x"}, Defs: map[string]any{"x": false}, AdditionalProperties: false}, `{"type":"object","properties":{"x":true},"required":["x"],"$defs":{"x":false},"additionalProperties":false,"title":"retained"}`},
		{"omitted optional fields", ToolArgumentsSchema{}, `{"type":"","properties":{},"required":[],"title":"retained"}`},
		{"array type", ToolArgumentsSchema{Types: []string{"object", "null"}}, `{"type":["object","null"],"properties":{},"required":[],"title":"retained"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extras := map[string]any{"type": "wrong", "properties": false, "required": false, "$defs": false, "additionalProperties": true, "definitions": map[string]any{"wrong": true}, "title": "retained"}
			original := maps.Clone(extras)
			tt.schema.AdditionalFields = extras
			encoded, err := json.Marshal(tt.schema)
			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(encoded))
			assert.Equal(t, original, extras)
		})
	}
}

// TestSchemaAdditionalFieldsReceiverReuse checks replacement and clearing of additional keywords.
func TestSchemaAdditionalFieldsReceiverReuse(t *testing.T) {
	var schema ToolArgumentsSchema
	require.NoError(t, json.Unmarshal([]byte(`{"title":"first","x-old":true}`), &schema))
	require.NoError(t, json.Unmarshal([]byte(`{"description":"second"}`), &schema))
	assert.Equal(t, map[string]any{"description": "second"}, schema.AdditionalFields)
	require.NoError(t, json.Unmarshal([]byte(`{"type":"object"}`), &schema))
	assert.Nil(t, schema.AdditionalFields)
	assert.Equal(t, ToolArgumentsSchema{Type: "object"}, schema)
	for _, extras := range []map[string]any{nil, {}} {
		encoded, err := json.Marshal(ToolArgumentsSchema{AdditionalFields: extras})
		require.NoError(t, err)
		assert.JSONEq(t, `{"type":"","properties":{},"required":[]}`, string(encoded))
	}
}

// TestSchemaAdditionalFieldsErrors checks marshal and modeled-field decode errors propagate.
func TestSchemaAdditionalFieldsErrors(t *testing.T) {
	_, err := json.Marshal(ToolArgumentsSchema{AdditionalFields: map[string]any{"unsupported": func() {}}})
	require.Error(t, err)
	tests := []struct{ name, data string }{
		{"malformed", `{"title":`}, {"type", `{"type":1}`}, {"type array", `{"type":[true]}`},
		{"properties", `{"properties":false}`}, {"required", `{"required":1}`}, {"defs", `{"$defs":false}`}, {"definitions", `{"definitions":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var schema ToolArgumentsSchema
			assert.Error(t, json.Unmarshal([]byte(tt.data), &schema))
		})
	}
}

// TestSchemaAdditionalFieldsDraft07Refs checks reference normalization only when definitions is adopted.
func TestSchemaAdditionalFieldsDraft07Refs(t *testing.T) {
	tests := []struct{ name, defs, expectedRef string }{
		{"adopt definitions", `"definitions":{"value":true}`, "#/$defs/value"},
		{"defs precedence", `"$defs":{"value":false},"definitions":{"ignored":true}`, "#/definitions/value"},
		{"no definitions", `"$defs":{"value":true}`, "#/definitions/value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const extras = `"$schema":"http://json-schema.org/draft-07/schema#","$ref":"#/definitions/value","oneOf":[{"$ref":"#/definitions/value"}],"anyOf":[{"$ref":"#/definitions/value"}],"allOf":[{"$ref":"#/definitions/value"}],"if":{"$ref":"#/definitions/value"},"then":{"$ref":"#/definitions/value"},"else":{"$ref":"#/definitions/value"},"x-external":{"$ref":"https://example.com/definitions/value"}`
			var schema ToolArgumentsSchema
			require.NoError(t, json.Unmarshal([]byte(`{"type":"object",`+tt.defs+`,`+extras+`}`), &schema))
			assert.NotContains(t, schema.AdditionalFields, "definitions")
			assert.NotContains(t, schema.Defs, "ignored")
			encoded, err := json.Marshal(schema.AdditionalFields)
			require.NoError(t, err)
			expected := strings.ReplaceAll(`{`+extras+`}`, "#/definitions/value", tt.expectedRef)
			assert.JSONEq(t, expected, string(encoded))
			full, err := json.Marshal(schema)
			require.NoError(t, err)
			var wire map[string]any
			require.NoError(t, json.Unmarshal(full, &wire))
			assert.NotContains(t, wire, "definitions")
			assert.Equal(t, tt.expectedRef, wire["$ref"])
		})
	}
}
