package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const schemaWithOtherKeywords = `{
	"$schema": "https://json-schema.org/draft/2020-12/schema",
	"$id": "https://example.com/schemas/order",
	"title": "Order",
	"description": "An order to place",
	"type": "object",
	"properties": {
		"sku": {"type": "string"},
		"quantity": {"type": "integer", "minimum": 1}
	},
	"required": ["sku"],
	"additionalProperties": false,
	"oneOf": [{"required": ["sku"]}, {"required": ["quantity"]}],
	"$defs": {"money": {"type": "number"}},
	"x-vendor": {"cacheable": true}
}`

// Decoding a schema used to keep only the keywords ToolArgumentsSchema has
// fields for, so a tool listed from a server lost its $schema, title, oneOf
// and so on when it was passed on. The 2026-07-28 spec has clients and
// servers validate a schema according to its declared dialect, which an
// encoded copy without $schema no longer declares.
func TestToolArgumentsSchema_KeepsOtherKeywords(t *testing.T) {
	var schema ToolArgumentsSchema
	require.NoError(t, json.Unmarshal([]byte(schemaWithOtherKeywords), &schema))

	assert.Equal(t, map[string]any{
		"$schema":     "https://json-schema.org/draft/2020-12/schema",
		"$id":         "https://example.com/schemas/order",
		"title":       "Order",
		"description": "An order to place",
		"oneOf": []any{
			map[string]any{"required": []any{"sku"}},
			map[string]any{"required": []any{"quantity"}},
		},
		"x-vendor": map[string]any{"cacheable": true},
	}, schema.AdditionalFields)

	encoded, err := json.Marshal(schema)
	require.NoError(t, err)
	assert.JSONEq(t, schemaWithOtherKeywords, string(encoded))
}

// A tool decoded from tools/list keeps them in both of its schemas.
func TestTool_RoundTripKeepsSchemaKeywords(t *testing.T) {
	data := `{"name": "place_order", "inputSchema": ` + schemaWithOtherKeywords + `, "outputSchema": ` + schemaWithOtherKeywords + `}`
	var tool Tool
	require.NoError(t, json.Unmarshal([]byte(data), &tool))

	encoded, err := json.Marshal(tool)
	require.NoError(t, err)
	var decoded struct {
		InputSchema  json.RawMessage `json:"inputSchema"`
		OutputSchema json.RawMessage `json:"outputSchema"`
	}
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.JSONEq(t, schemaWithOtherKeywords, string(decoded.InputSchema))
	assert.JSONEq(t, schemaWithOtherKeywords, string(decoded.OutputSchema))
}

// The fields win over entries of AdditionalFields with the same name, and
// Defs is written only as "$defs".
func TestToolArgumentsSchema_FieldsWinOverAdditionalFields(t *testing.T) {
	schema := ToolArgumentsSchema{
		Type:       "object",
		Properties: map[string]any{"a": map[string]any{"type": "string"}},
		Defs:       map[string]any{"d": map[string]any{"type": "number"}},
		AdditionalFields: map[string]any{
			"type":        "string",
			"properties":  map[string]any{"b": map[string]any{}},
			"required":    []string{"b"},
			"$defs":       map[string]any{},
			"definitions": map[string]any{"old": map[string]any{}},
			"title":       "kept",
		},
	}

	encoded, err := json.Marshal(schema)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"type": "object",
		"properties": {"a": {"type": "string"}},
		"required": [],
		"$defs": {"d": {"type": "number"}},
		"title": "kept"
	}`, string(encoded))

	// Without Defs, "definitions" set in code is the only home of its refs.
	encoded, err = json.Marshal(ToolArgumentsSchema{
		Type:             "object",
		AdditionalFields: map[string]any{"definitions": map[string]any{"old": map[string]any{}}},
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"type": "object", "properties": {}, "required": [], "definitions": {"old": {}}}`, string(encoded))
}

// A schema with only the keywords that have fields leaves AdditionalFields
// nil, so decoding into a reused value doesn't keep stale keywords. JSON null
// leaves the schema as it is, as encoding/json does.
func TestToolArgumentsSchema_AdditionalFieldsResetOnDecode(t *testing.T) {
	schema := ToolArgumentsSchema{AdditionalFields: map[string]any{"title": "stale"}}
	require.NoError(t, json.Unmarshal([]byte(`null`), &schema))
	assert.Equal(t, map[string]any{"title": "stale"}, schema.AdditionalFields)

	require.NoError(t, json.Unmarshal([]byte(`{"type": "object", "properties": {}}`), &schema))
	assert.Nil(t, schema.AdditionalFields)
}

// Decoding turns draft-07 "definitions" into "$defs" and rewrites the local
// refs that point into it. Refs in the kept keywords, a top-level oneOf here,
// have to follow, or they would dangle once "definitions" is gone.
func TestToolArgumentsSchema_Draft07RefsInOtherKeywords(t *testing.T) {
	var schema ToolArgumentsSchema
	require.NoError(t, json.Unmarshal([]byte(`{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type": "object",
		"definitions": {"sku": {"type": "string"}},
		"properties": {"sku": {"$ref": "#/definitions/sku"}},
		"oneOf": [{"properties": {"sku": {"$ref": "#/definitions/sku"}}}]
	}`), &schema))

	encoded, err := json.Marshal(schema)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type": "object",
		"$defs": {"sku": {"type": "string"}},
		"properties": {"sku": {"$ref": "#/$defs/sku"}},
		"required": [],
		"oneOf": [{"properties": {"sku": {"$ref": "#/$defs/sku"}}}]
	}`, string(encoded))
}

// Draft-04 requires "required" to have at least one element, so a schema
// that declares it doesn't get the empty "required" others do.
func TestToolArgumentsSchema_Draft04WithoutRequired(t *testing.T) {
	var schema ToolArgumentsSchema
	require.NoError(t, json.Unmarshal([]byte(`{
		"$schema": "http://json-schema.org/draft-04/schema#",
		"type": "object",
		"properties": {"sku": {"type": "string"}}
	}`), &schema))

	encoded, err := json.Marshal(schema)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"$schema": "http://json-schema.org/draft-04/schema#",
		"type": "object",
		"properties": {"sku": {"type": "string"}}
	}`, string(encoded))
}
