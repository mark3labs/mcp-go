package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolSchemaRoundTrip_DeclaredDialectValidation(t *testing.T) {
	tests := []struct {
		name    string
		schema  string
		valid   string
		invalid string
	}{
		{
			name: "draft-07 definitions retain numbers beyond float64 range",
			schema: `{
				"$schema": "http://json-schema.org/draft-07/schema#",
				"type": "object", "required": ["value"],
				"definitions": {"huge": {"const": 1e400}},
				"properties": {"value": {"$ref": "#/definitions/huge"}}
			}`,
			valid: `{"value": 1e400}`, invalid: `{"value": 1e399}`,
		},
		{
			name: "draft-07 IDs inside definitions remain discoverable",
			schema: `{
				"$schema": "http://json-schema.org/draft-07/schema#",
				"type": "object", "required": ["sku"],
				"definitions": {"sku": {"$id": "#sku", "type": "string", "minLength": 2}},
				"properties": {"sku": {"$ref": "#sku"}},
				"allOf": [{"properties": {"sku": {"$ref": "#/definitions/sku"}}}]
			}`,
			valid: `{"sku": "ab"}`, invalid: `{"sku": "a"}`,
		},
		{
			name: "2020-12 anchors inside defs and nested applicators",
			schema: `{
				"$schema": "https://json-schema.org/draft/2020-12/schema",
				"type": "object", "required": ["sku"],
				"$defs": {"sku": {"$anchor": "sku", "type": "string", "minLength": 2}},
				"properties": {"sku": {"$ref": "#sku"}},
				"allOf": [{"properties": {"sku": {"$ref": "#/$defs/sku"}}}],
				"unevaluatedProperties": false
			}`,
			valid: `{"sku": "ab"}`, invalid: `{"sku": "ab", "extra": true}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := compileRoundTripSchema(t, []byte(tt.schema))
			require.NoError(t, original.Validate(decodeRoundTripJSON(t, tt.valid)))
			require.Error(t, original.Validate(decodeRoundTripJSON(t, tt.invalid)))
			for name, encoded := range roundTripToolSchemas(t, tt.schema) {
				t.Run(name, func(t *testing.T) {
					before := decodeRoundTripJSON(t, tt.schema).(map[string]any)
					after := decodeRoundTripJSON(t, string(encoded)).(map[string]any)
					assert.Equal(t, before["$schema"], after["$schema"])
					for _, keyword := range []string{"definitions", "$defs", "properties", "allOf"} {
						assert.Equal(t, before[keyword], after[keyword], keyword)
					}
					compiled := compileRoundTripSchema(t, encoded)
					assert.NoError(t, compiled.Validate(decodeRoundTripJSON(t, tt.valid)))
					assert.Error(t, compiled.Validate(decodeRoundTripJSON(t, tt.invalid)))
				})
			}
		})
	}
}

func TestToolSchemaRoundTrip_BothDefinitionContainers(t *testing.T) {
	tests := []struct {
		name    string
		dialect string
	}{
		{name: "draft-07", dialect: `"$schema": "http://json-schema.org/draft-07/schema#",`},
		{name: "2020-12", dialect: `"$schema": "https://json-schema.org/draft/2020-12/schema",`},
		{name: "no declared dialect"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := `{` + tt.dialect + `
				"type": "object", "required": ["old", "new"],
				"definitions": {"value": {"type": "integer"}},
				"$defs": {"value": {"type": "string"}},
				"properties": {"old": {"$ref": "#/definitions/value"}, "new": {"$ref": "#/$defs/value"}}
			}`
			original := compileRoundTripSchema(t, []byte(data))
			valid := decodeRoundTripJSON(t, `{"old": 1, "new": "a"}`)
			invalid := decodeRoundTripJSON(t, `{"old": "a", "new": 1}`)
			require.NoError(t, original.Validate(valid))
			require.Error(t, original.Validate(invalid))
			for name, encoded := range roundTripToolSchemas(t, data) {
				t.Run(name, func(t *testing.T) {
					compiled := compileRoundTripSchema(t, encoded)
					assert.NoError(t, compiled.Validate(valid))
					assert.Error(t, compiled.Validate(invalid))
				})
			}
		})
	}
}

func TestToolSchemaRoundTrip_UnknownDialectPreservesSyntax(t *testing.T) {
	data := `{
		"$schema": "https://example.com/custom-dialect", "type": "object",
		"definitions": {"value": {"type": "integer"}},
		"properties": {"value": {"$ref": "#/definitions/value"}},
		"allOf": [{"properties": {"value": {"$ref": "#/definitions/value"}}}],
		"x-data": {"$ref": "#/definitions/value"}
	}`
	for name, encoded := range roundTripToolSchemas(t, data) {
		t.Run(name, func(t *testing.T) {
			before := decodeRoundTripJSON(t, data).(map[string]any)
			after := decodeRoundTripJSON(t, string(encoded)).(map[string]any)
			delete(after, "required")
			assert.Equal(t, before, after)
		})
	}
}

func TestToolSchemaRoundTrip_KeywordNumberPrecision(t *testing.T) {
	tests := []struct {
		name     string
		keywords string
	}{
		{name: "const", keywords: `"const": {"value": 9007199254740993}`},
		{name: "nested oneOf", keywords: `"oneOf": [{"properties": {"value": {"const": 9007199254740993}}}]`},
		{name: "nested conditional", keywords: `"if": {"required": ["value"]}, "then": {"properties": {"value": {"enum": [9007199254740993]}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := `{"$schema": "https://json-schema.org/draft/2020-12/schema", "type": "object", "required": ["value"], ` + tt.keywords + `}`
			valid := decodeRoundTripJSON(t, `{"value": 9007199254740993}`)
			invalid := decodeRoundTripJSON(t, `{"value": 9007199254740992}`)
			original := compileRoundTripSchema(t, []byte(data))
			require.NoError(t, original.Validate(valid))
			require.Error(t, original.Validate(invalid))
			for name, encoded := range roundTripToolSchemas(t, data) {
				t.Run(name, func(t *testing.T) {
					compiled := compileRoundTripSchema(t, encoded)
					assert.NoError(t, compiled.Validate(valid))
					assert.Error(t, compiled.Validate(invalid))
				})
			}
		})
	}
}

func TestToolSchemaRoundTrip_AdditionalKeywordNumbers(t *testing.T) {
	data := `{"type": "object", "properties": {}, "required": [], "x-vendor": {"values": [9007199254740993, 1e400, 0.123456789012345678901]}}`
	for name, encoded := range roundTripToolSchemas(t, data) {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, decodeRoundTripJSON(t, data), decodeRoundTripJSON(t, string(encoded)))
		})
	}
}

func TestToolSchemaRoundTrip_NoDialectNormalizesDefinitions(t *testing.T) {
	data := `{
		"type": "object", "required": ["value"],
		"definitions": {"value": {"type": "integer"}},
		"properties": {"value": {"$ref": "#/definitions/value"}},
		"allOf": [{"properties": {"value": {"$ref": "#/definitions/value"}}}]
	}`
	for name, encoded := range roundTripToolSchemas(t, data) {
		t.Run(name, func(t *testing.T) {
			assert.NotContains(t, string(encoded), "#/definitions/")
			compiled := compileRoundTripSchema(t, encoded)
			assert.NoError(t, compiled.Validate(decodeRoundTripJSON(t, `{"value": 1}`)))
			assert.Error(t, compiled.Validate(decodeRoundTripJSON(t, `{"value": "a"}`)))
		})
	}
}

func roundTripToolSchemas(t *testing.T, data string) map[string]json.RawMessage {
	t.Helper()
	var tool Tool
	require.NoError(t, json.Unmarshal([]byte(`{"name": "round_trip", "inputSchema": `+data+`, "outputSchema": `+data+`}`), &tool))
	encoded, err := json.Marshal(tool)
	require.NoError(t, err)
	var schemas struct {
		Input  json.RawMessage `json:"inputSchema"`
		Output json.RawMessage `json:"outputSchema"`
	}
	require.NoError(t, json.Unmarshal(encoded, &schemas))
	return map[string]json.RawMessage{"input": schemas.Input, "output": schemas.Output}
}

func decodeRoundTripJSON(t *testing.T, data string) any {
	t.Helper()
	decoded, err := jsonschema.UnmarshalJSON(strings.NewReader(data))
	require.NoError(t, err)
	return decoded
}

func compileRoundTripSchema(t *testing.T, data []byte) *jsonschema.Schema {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	require.NoError(t, err)
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	const uri = "https://example.com/round-trip-schema"
	require.NoError(t, compiler.AddResource(uri, doc))
	compiled, err := compiler.Compile(uri)
	require.NoError(t, err)
	return compiled
}
