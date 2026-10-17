package mcp

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// objectKeys returns the top-level keys of a JSON object in the order they
// appear in raw.
func objectKeys(t testing.TB, raw []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	require.NoError(t, err)
	require.Equal(t, json.Delim('{'), tok, "expected a JSON object: %s", raw)

	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		require.NoError(t, err)
		key, ok := tok.(string)
		require.True(t, ok, "expected an object key, got %v", tok)
		keys = append(keys, key)

		var skip json.RawMessage
		require.NoError(t, dec.Decode(&skip))
	}
	return keys
}

// objectField returns the raw value of key in the JSON object raw.
func objectField(t testing.TB, raw []byte, key string) json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &fields))
	value, ok := fields[key]
	require.True(t, ok, "missing key %q in %s", key, raw)
	return value
}

const unsortedPropertiesSchema = `{
	"type": "object",
	"properties": {
		"zeta": {"type": "string"},
		"alpha": {"type": "number"},
		"mid": {"type": "boolean"}
	},
	"required": ["zeta"]
}`

var unsortedPropertiesOrder = []string{"zeta", "alpha", "mid"}

// TestToolArgumentsSchema_PropertyOrderRoundTrip checks that decoding and
// marshaling keeps the property order for each schema type and inside Tool
// and ListToolsResult.
func TestToolArgumentsSchema_PropertyOrderRoundTrip(t *testing.T) {
	t.Run("ToolArgumentsSchema", func(t *testing.T) {
		var schema ToolArgumentsSchema
		require.NoError(t, json.Unmarshal([]byte(unsortedPropertiesSchema), &schema))

		out, err := json.Marshal(schema)
		require.NoError(t, err)
		assert.Equal(t, unsortedPropertiesOrder, objectKeys(t, objectField(t, out, "properties")))
	})

	t.Run("ToolInputSchema", func(t *testing.T) {
		var schema ToolInputSchema
		require.NoError(t, json.Unmarshal([]byte(unsortedPropertiesSchema), &schema))

		out, err := json.Marshal(schema)
		require.NoError(t, err)
		assert.Equal(t, unsortedPropertiesOrder, objectKeys(t, objectField(t, out, "properties")))
	})

	t.Run("ToolOutputSchema", func(t *testing.T) {
		var schema ToolOutputSchema
		require.NoError(t, json.Unmarshal([]byte(unsortedPropertiesSchema), &schema))

		out, err := json.Marshal(schema)
		require.NoError(t, err)
		assert.Equal(t, unsortedPropertiesOrder, objectKeys(t, objectField(t, out, "properties")))
	})

	t.Run("Tool", func(t *testing.T) {
		toolJSON := `{
			"name": "search",
			"inputSchema": ` + unsortedPropertiesSchema + `,
			"outputSchema": ` + unsortedPropertiesSchema + `
		}`
		var tool Tool
		require.NoError(t, json.Unmarshal([]byte(toolJSON), &tool))

		out, err := json.Marshal(tool)
		require.NoError(t, err)
		for _, field := range []string{"inputSchema", "outputSchema"} {
			schema := objectField(t, out, field)
			assert.Equal(t, unsortedPropertiesOrder, objectKeys(t, objectField(t, schema, "properties")), field)
		}
	})

	t.Run("ListToolsResult", func(t *testing.T) {
		resultJSON := `{
			"tools": [
				{"name": "first", "inputSchema": ` + unsortedPropertiesSchema + `},
				{"name": "second", "inputSchema": {
					"type": "object",
					"properties": {"b": {}, "c": {}, "a": {}}
				}}
			]
		}`
		var result ListToolsResult
		require.NoError(t, json.Unmarshal([]byte(resultJSON), &result))

		out, err := json.Marshal(result)
		require.NoError(t, err)

		var decoded struct {
			Tools []json.RawMessage `json:"tools"`
		}
		require.NoError(t, json.Unmarshal(out, &decoded))
		require.Len(t, decoded.Tools, 2)

		want := [][]string{unsortedPropertiesOrder, {"b", "c", "a"}}
		for i, tool := range decoded.Tools {
			schema := objectField(t, tool, "inputSchema")
			assert.Equal(t, want[i], objectKeys(t, objectField(t, schema, "properties")))
		}
	})

	t.Run("output is stable across repeated marshals", func(t *testing.T) {
		var schema ToolArgumentsSchema
		require.NoError(t, json.Unmarshal([]byte(unsortedPropertiesSchema), &schema))

		first, err := json.Marshal(schema)
		require.NoError(t, err)
		for range 50 {
			out, err := json.Marshal(schema)
			require.NoError(t, err)
			require.Equal(t, string(first), string(out))
		}
	})
}

// TestToolArgumentsSchema_PropertyOrderAfterMutation checks marshal output
// after properties are added to or deleted from a decoded schema.
func TestToolArgumentsSchema_PropertyOrderAfterMutation(t *testing.T) {
	var schema ToolArgumentsSchema
	require.NoError(t, json.Unmarshal([]byte(unsortedPropertiesSchema), &schema))

	// Names removed from the map are skipped. Names added to the map without
	// updating the order are appended after the known names, sorted.
	delete(schema.Properties, "alpha")
	schema.Properties["beta"] = map[string]any{"type": "string"}
	schema.Properties["aardvark"] = map[string]any{"type": "string"}

	out, err := json.Marshal(schema)
	require.NoError(t, err)
	properties := objectField(t, out, "properties")
	assert.Equal(t, []string{"zeta", "mid", "aardvark", "beta"}, objectKeys(t, properties))

	var values map[string]any
	require.NoError(t, json.Unmarshal(properties, &values))
	assert.Equal(t, map[string]any{
		"zeta":     map[string]any{"type": "string"},
		"mid":      map[string]any{"type": "boolean"},
		"aardvark": map[string]any{"type": "string"},
		"beta":     map[string]any{"type": "string"},
	}, values)

	// A name that is deleted and added back keeps its recorded position.
	schema.Properties["alpha"] = map[string]any{"type": "integer"}
	out, err = json.Marshal(schema)
	require.NoError(t, err)
	assert.Equal(t, []string{"zeta", "alpha", "mid", "aardvark", "beta"},
		objectKeys(t, objectField(t, out, "properties")))

	schema.Properties = nil
	out, err = json.Marshal(schema)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(objectField(t, out, "properties")))
}

// TestToolArgumentsSchema_PropertyOrderDraft07Definitions checks that a
// draft-07 schema keeps its property order while its refs move to $defs.
func TestToolArgumentsSchema_PropertyOrderDraft07Definitions(t *testing.T) {
	jsonData := `{
		"type": "object",
		"properties": {
			"target": {"$ref": "#/definitions/target"},
			"operation": {"$ref": "#/definitions/operation_type"}
		},
		"required": ["operation"],
		"definitions": {
			"operation_type": {"type": "string", "enum": ["create", "delete"]},
			"target": {"type": "string"}
		}
	}`

	var schema ToolArgumentsSchema
	require.NoError(t, json.Unmarshal([]byte(jsonData), &schema))
	require.Contains(t, schema.Defs, "operation_type")
	require.Contains(t, schema.Defs, "target")

	out, err := json.Marshal(schema)
	require.NoError(t, err)
	assert.NotContains(t, string(out), "#/definitions/")

	properties := objectField(t, out, "properties")
	assert.Equal(t, []string{"target", "operation"}, objectKeys(t, properties))
	assert.JSONEq(t, `{
		"target": {"$ref": "#/$defs/target"},
		"operation": {"$ref": "#/$defs/operation_type"}
	}`, string(properties))
	assert.JSONEq(t, `{
		"operation_type": {"type": "string", "enum": ["create", "delete"]},
		"target": {"type": "string"}
	}`, string(objectField(t, out, "$defs")))
}

// TestToolArgumentsSchema_PropertyOrderEdgeCases covers duplicate keys and
// null, empty and non-object properties.
func TestToolArgumentsSchema_PropertyOrderEdgeCases(t *testing.T) {
	t.Run("duplicate keys keep the first position", func(t *testing.T) {
		var schema ToolArgumentsSchema
		require.NoError(t, json.Unmarshal([]byte(`{
			"type": "object",
			"properties": {"b": {"n": 1}, "a": {}, "b": {"n": 2}}
		}`), &schema))

		out, err := json.Marshal(schema)
		require.NoError(t, err)
		properties := objectField(t, out, "properties")
		assert.Equal(t, []string{"b", "a"}, objectKeys(t, properties))
		// encoding/json keeps the last value for a duplicate key.
		assert.JSONEq(t, `{"b": {"n": 2}, "a": {}}`, string(properties))
	})

	t.Run("null properties", func(t *testing.T) {
		var schema ToolArgumentsSchema
		require.NoError(t, json.Unmarshal([]byte(`{"type": "object", "properties": null}`), &schema))
		assert.Nil(t, schema.Properties)

		out, err := json.Marshal(schema)
		require.NoError(t, err)
		assert.JSONEq(t, `{"type": "object", "properties": {}, "required": []}`, string(out))
	})

	t.Run("empty properties", func(t *testing.T) {
		var schema ToolArgumentsSchema
		require.NoError(t, json.Unmarshal([]byte(`{"type": "object", "properties": {}}`), &schema))

		out, err := json.Marshal(schema)
		require.NoError(t, err)
		assert.JSONEq(t, `{"type": "object", "properties": {}, "required": []}`, string(out))
	})

	t.Run("non-object properties is an error", func(t *testing.T) {
		for _, input := range []string{
			`{"type": "object", "properties": []}`,
			`{"type": "object", "properties": "x"}`,
			`{"type": "object", "properties": 1}`,
		} {
			var schema ToolArgumentsSchema
			assert.Error(t, json.Unmarshal([]byte(input), &schema), input)
		}
	})
}

// nilOrderGoldenCases holds marshaled output captured from mcp-go v1.0.0,
// before ToolArgumentsSchema recorded property order. Schemas built in Go
// without a property order must keep producing these exact bytes.
var nilOrderGoldenCases = []struct {
	name   string
	schema ToolArgumentsSchema
	want   string
}{
	{
		name:   "zero value",
		schema: ToolArgumentsSchema{},
		want:   `{"properties":{},"required":[],"type":""}`,
	},
	{
		name:   "type only",
		schema: ToolArgumentsSchema{Type: "object"},
		want:   `{"properties":{},"required":[],"type":"object"}`,
	},
	{
		name: "empty properties map",
		schema: ToolArgumentsSchema{
			Type:       "object",
			Properties: map[string]any{},
		},
		want: `{"properties":{},"required":[],"type":"object"}`,
	},
	{
		name: "several properties and required",
		schema: ToolArgumentsSchema{
			Type: "object",
			Properties: map[string]any{
				"zeta":  map[string]any{"type": "string", "description": "last"},
				"alpha": map[string]any{"type": "number", "minimum": 0, "maximum": 10.5},
				"mid":   map[string]any{"type": "boolean", "default": true},
			},
			Required: []string{"zeta", "alpha"},
		},
		want: `{"properties":{"alpha":{"maximum":10.5,"minimum":0,"type":"number"},"mid":{"default":true,"type":"boolean"},"zeta":{"description":"last","type":"string"}},"required":["zeta","alpha"],"type":"object"}`,
	},
	{
		name: "nested objects and arrays",
		schema: ToolArgumentsSchema{
			Type: "object",
			Properties: map[string]any{
				"filter": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"z": map[string]any{"type": "string"},
						"a": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
					},
				},
				"tags": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string", "enum": []string{"b", "a"}},
				},
			},
		},
		want: `{"properties":{"filter":{"properties":{"a":{"items":{"type":"integer"},"type":"array"},"z":{"type":"string"}},"type":"object"},"tags":{"items":{"enum":["b","a"],"type":"string"},"type":"array"}},"required":[],"type":"object"}`,
	},
	{
		name: "HTML characters and unicode in keys and values",
		schema: ToolArgumentsSchema{
			Type: "object",
			Properties: map[string]any{
				"<b>&amp;":   map[string]any{"description": "a < b && c > d"},
				"café":       map[string]any{"description": " line sep"},
				"quote\"key": "plain string value",
			},
		},
		want: `{"properties":{"\u003cb\u003e\u0026amp;":{"description":"a \u003c b \u0026\u0026 c \u003e d"},"café":{"description":"\u2028line\u2029sep"},"quote\"key":"plain string value"},"required":[],"type":"object"}`,
	},
	{
		name: "non-map property values",
		schema: ToolArgumentsSchema{
			Type: "object",
			Properties: map[string]any{
				"null":   nil,
				"bool":   true,
				"number": 1.25,
				"raw":    json.RawMessage(`{"type": "string"}`),
				"slice":  []any{"x", 2},
			},
		},
		want: `{"properties":{"bool":true,"null":null,"number":1.25,"raw":{"type":"string"},"slice":["x",2]},"required":[],"type":"object"}`,
	},
	{
		name: "defs and additionalProperties",
		schema: ToolArgumentsSchema{
			Type: "object",
			Defs: map[string]any{
				"thing": map[string]any{"type": "string"},
			},
			Properties: map[string]any{
				"b": map[string]any{"$ref": "#/$defs/thing"},
				"a": map[string]any{"$ref": "#/$defs/thing"},
			},
			AdditionalProperties: false,
		},
		want: `{"$defs":{"thing":{"type":"string"}},"additionalProperties":false,"properties":{"a":{"$ref":"#/$defs/thing"},"b":{"$ref":"#/$defs/thing"}},"required":[],"type":"object"}`,
	},
	{
		name: "additionalProperties schema",
		schema: ToolArgumentsSchema{
			Type:                 "object",
			Properties:           map[string]any{"k": map[string]any{"type": "string"}},
			AdditionalProperties: map[string]any{"type": "integer"},
		},
		want: `{"additionalProperties":{"type":"integer"},"properties":{"k":{"type":"string"}},"required":[],"type":"object"}`,
	},
}

// TestToolArgumentsSchema_NilPropertyOrderMatchesGolden checks that schemas
// without a property order marshal to the v1.0.0 bytes.
func TestToolArgumentsSchema_NilPropertyOrderMatchesGolden(t *testing.T) {
	for _, tc := range nilOrderGoldenCases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := json.Marshal(tc.schema)
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(out))

			input, err := json.Marshal(ToolInputSchema(tc.schema))
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(input))

			output, err := json.Marshal(ToolOutputSchema(tc.schema))
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(output))
		})
	}
}

// TestToolArgumentsSchema_SortedPropertyOrderMatchesGolden checks that a
// sorted, empty or unrelated order marshals to the same bytes as a nil order.
func TestToolArgumentsSchema_SortedPropertyOrderMatchesGolden(t *testing.T) {
	for _, tc := range nilOrderGoldenCases {
		t.Run(tc.name, func(t *testing.T) {
			sorted := make([]string, 0, len(tc.schema.Properties))
			for name := range tc.schema.Properties {
				sorted = append(sorted, name)
			}
			sort.Strings(sorted)

			for _, order := range [][]string{sorted, {}, {"not-a-property"}} {
				schema := tc.schema
				schema.PropertyOrder = order
				out, err := json.Marshal(schema)
				require.NoError(t, err)
				assert.Equal(t, tc.want, string(out), "order %q", order)
			}
		})
	}
}

// TestToolArgumentsSchema_PropertyOrderField checks which inputs record an
// order, marshaling with an order set in Go, and the error for a value that
// cannot be encoded.
func TestToolArgumentsSchema_PropertyOrderField(t *testing.T) {
	t.Run("decode records the order", func(t *testing.T) {
		var schema ToolArgumentsSchema
		require.NoError(t, json.Unmarshal([]byte(unsortedPropertiesSchema), &schema))
		assert.Equal(t, unsortedPropertiesOrder, schema.PropertyOrder)
	})

	t.Run("no keys records no order", func(t *testing.T) {
		for _, input := range []string{
			`{"type": "object"}`,
			`{"type": "object", "properties": null}`,
			`{"type": "object", "properties": {}}`,
		} {
			var schema ToolArgumentsSchema
			require.NoError(t, json.Unmarshal([]byte(input), &schema))
			assert.Nil(t, schema.PropertyOrder, input)
		}
	})

	t.Run("explicit order on a Go built schema", func(t *testing.T) {
		schema := ToolArgumentsSchema{
			Type: "object",
			Properties: map[string]any{
				"a": map[string]any{"type": "string"},
				"b": map[string]any{"type": "string"},
				"c": map[string]any{"type": "string"},
			},
			PropertyOrder: []string{"c", "missing", "a", "c"},
		}
		out, err := json.Marshal(schema)
		require.NoError(t, err)
		assert.Equal(t,
			`{"properties":{"c":{"type":"string"},"a":{"type":"string"},"b":{"type":"string"}},"required":[],"type":"object"}`,
			string(out))
	})

	t.Run("unsupported property value returns the encoding error", func(t *testing.T) {
		schema := ToolArgumentsSchema{
			Type:          "object",
			Properties:    map[string]any{"ch": make(chan int)},
			PropertyOrder: []string{"ch"},
		}
		_, orderedErr := json.Marshal(schema)
		require.Error(t, orderedErr)

		schema.PropertyOrder = nil
		_, mapErr := json.Marshal(schema)
		require.Error(t, mapErr)
		assert.Equal(t, mapErr.Error(), orderedErr.Error())
	})
}

// TestToolArgumentsSchema_PropertyOrderDeepEqual documents that a decoded
// schema is not reflect.DeepEqual to a literal with a nil PropertyOrder until
// the orders match.
func TestToolArgumentsSchema_PropertyOrderDeepEqual(t *testing.T) {
	literal := ToolArgumentsSchema{
		Type: "object",
		Properties: map[string]any{
			"b": map[string]any{"type": "string"},
			"a": map[string]any{"type": "string"},
		},
	}

	var decoded ToolArgumentsSchema
	require.NoError(t, json.Unmarshal([]byte(`{
		"type": "object",
		"properties": {"b": {"type": "string"}, "a": {"type": "string"}}
	}`), &decoded))

	assert.False(t, reflect.DeepEqual(literal, decoded))

	withOrder := literal
	withOrder.PropertyOrder = []string{"b", "a"}
	assert.True(t, reflect.DeepEqual(withOrder, decoded))

	decoded.PropertyOrder = nil
	assert.True(t, reflect.DeepEqual(literal, decoded))
}

// FuzzToolArgumentsSchemaPropertyOrder builds properties objects from fuzzed
// names and checks key order, values and a stable second round trip.
func FuzzToolArgumentsSchemaPropertyOrder(f *testing.F) {
	f.Add([]byte("zeta\x00alpha\x00mid"))
	f.Add([]byte("b\x00a\x00b"))
	f.Add([]byte("a"))
	f.Add([]byte(""))
	f.Add([]byte("\x00\x00"))
	f.Add([]byte("<b>\x00&amp;\x00caf\xc3\xa9\x00\xff\xfe"))
	f.Add([]byte("Key\x00key\x00KEY"))

	f.Fuzz(func(t *testing.T, data []byte) {
		names := bytes.Split(data, []byte{0})

		// Build a properties object with the names in the given order.
		// Duplicate names are allowed and each value records its index.
		var buf bytes.Buffer
		buf.WriteString(`{"type":"object","properties":{`)
		for i, name := range names {
			if i > 0 {
				buf.WriteByte(',')
			}
			key, err := json.Marshal(string(name))
			require.NoError(t, err)
			buf.Write(key)
			buf.WriteString(`:{"index":`)
			value, err := json.Marshal(i)
			require.NoError(t, err)
			buf.Write(value)
			buf.WriteByte('}')
		}
		buf.WriteString(`}}`)
		input := buf.Bytes()

		// The expected order is the decoded names with duplicates at their
		// first position. Invalid UTF-8 decodes to U+FFFD, so distinct input
		// bytes can collapse into one name.
		var want []string
		seen := make(map[string]bool)
		for _, key := range objectKeys(t, objectField(t, input, "properties")) {
			if !seen[key] {
				seen[key] = true
				want = append(want, key)
			}
		}

		var schema ToolArgumentsSchema
		require.NoError(t, json.Unmarshal(input, &schema))

		out, err := json.Marshal(schema)
		require.NoError(t, err)
		properties := objectField(t, out, "properties")
		got := objectKeys(t, properties)
		if len(want) == 0 {
			assert.Empty(t, got)
		} else {
			assert.Equal(t, want, got)
		}

		var wantValues, gotValues map[string]any
		require.NoError(t, json.Unmarshal(objectField(t, input, "properties"), &wantValues))
		require.NoError(t, json.Unmarshal(properties, &gotValues))
		assert.Equal(t, wantValues, gotValues)

		// A second round trip keeps the same bytes.
		var again ToolArgumentsSchema
		require.NoError(t, json.Unmarshal(out, &again))
		out2, err := json.Marshal(again)
		require.NoError(t, err)
		assert.Equal(t, string(out), string(out2))
	})
}
