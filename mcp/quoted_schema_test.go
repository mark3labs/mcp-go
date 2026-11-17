package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type quotedSchemaInt int

type quotedSchemaMarshaler int

func (quotedSchemaMarshaler) MarshalJSON() ([]byte, error) { return []byte("17"), nil }

type quotedSchemaFields struct {
	Bool      bool                  `json:"bool,string"`
	Int       int                   `json:"int,string"`
	Int8      int8                  `json:"int8,string"`
	Int16     int16                 `json:"int16,string"`
	Int32     int32                 `json:"int32,string"`
	Int64     int64                 `json:"int64,string"`
	Uint      uint                  `json:"uint,string"`
	Uint8     uint8                 `json:"uint8,string"`
	Uint16    uint16                `json:"uint16,string"`
	Uint32    uint32                `json:"uint32,string"`
	Uint64    uint64                `json:"uint64,string"`
	Uintptr   uintptr               `json:"uintptr,string"`
	Float32   float32               `json:"float32,string"`
	Float64   float64               `json:"float64,string"`
	String    string                `json:"string,string"`
	Named     quotedSchemaInt       `json:"named,string"`
	Pointer   *int                  `json:"pointer,string,omitempty"`
	Plain     int                   `json:"plain"`
	Slice     []int                 `json:"slice,string"`     //nolint:staticcheck // Verify that encoding/json ignores string for unsupported types.
	DoublePtr **int                 `json:"doublePtr,string"` //nolint:staticcheck // Verify that encoding/json ignores string for unsupported types.
	Custom    quotedSchemaMarshaler `json:"custom,string"`
}

func TestSchemaFor_QuotedFieldTypes(t *testing.T) {
	type Fields quotedSchemaFields
	type exportedFallback struct {
		Fields
		Mode string `json:"mode" jsonschema:"enum=fast,enum=safe"`
	}
	tests := []struct {
		name string
		raw  func() (json.RawMessage, error)
	}{
		{name: "normal", raw: SchemaForRaw[quotedSchemaFields]},
		{name: "enum fallback", raw: SchemaForRaw[exportedFallback]},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, err := test.raw()
			require.NoError(t, err)
			var schema map[string]any
			require.NoError(t, json.Unmarshal(raw, &schema))
			properties := schema["properties"].(map[string]any)
			for _, name := range []string{"bool", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64", "string", "named"} {
				assert.Equal(t, map[string]any{"type": "string"}, properties[name], name)
			}
			assert.Equal(t, map[string]any{"type": []any{"null", "string"}}, properties["pointer"])
			assert.NotContains(t, schema["required"], "pointer")
			assert.Equal(t, "integer", properties["plain"].(map[string]any)["type"])
			assert.Equal(t, []any{"null", "array"}, properties["slice"].(map[string]any)["type"])
			assert.Equal(t, []any{"null", "integer"}, properties["doublePtr"].(map[string]any)["type"])
			assert.Equal(t, "integer", properties["custom"].(map[string]any)["type"])
		})
	}
}

func TestToolSchema_QuotedFields(t *testing.T) {
	type fields struct {
		Count *int `json:"count,string" jsonschema_description:"Number of results"`
	}
	type request struct {
		Nested fields   `json:"nested"`
		List   []fields `json:"list"`
		Mode   string   `json:"mode" jsonschema:"enum=fast,enum=safe"`
	}
	tool := NewTool("quoted", WithInputSchema[request](), WithOutputSchema[request]())
	output, err := json.Marshal(tool.OutputSchema)
	require.NoError(t, err)
	for _, raw := range []json.RawMessage{tool.RawInputSchema, output} {
		var schema map[string]any
		require.NoError(t, json.Unmarshal(raw, &schema))
		properties := schema["properties"].(map[string]any)
		nested := properties["nested"].(map[string]any)
		list := properties["list"].(map[string]any)
		for _, field := range []map[string]any{nested, list["items"].(map[string]any)} {
			count := field["properties"].(map[string]any)["count"]
			assert.Equal(t, map[string]any{"type": []any{"null", "string"}, "description": "Number of results"}, count)
		}
	}
	count := 42
	for _, test := range []struct {
		value *int
		want  string
	}{
		{value: &count, want: `{"count":"42"}`},
		{value: nil, want: `{"count":null}`},
	} {
		raw, err := json.Marshal(fields{Count: test.value})
		require.NoError(t, err)
		assert.JSONEq(t, test.want, string(raw))
	}
}

func TestSchemaFor_QuotedFieldShadowing(t *testing.T) {
	type Embedded struct {
		Count int `json:"count,string"`
	}
	type request struct {
		Embedded
		Count int `json:"count"`
	}
	raw, err := SchemaForRaw[request]()
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(raw, &schema))
	count := schema["properties"].(map[string]any)["count"].(map[string]any)
	assert.Equal(t, "integer", count["type"])
	raw, err = json.Marshal(request{Count: 7})
	require.NoError(t, err)
	assert.JSONEq(t, `{"count":7}`, string(raw))
}

func TestSchemaFor_QuotedRootContainers(t *testing.T) {
	type request struct {
		Count int `json:"count,string"`
	}
	tests := []struct {
		name string
		raw  func() (json.RawMessage, error)
		path []string
	}{
		{name: "slice", raw: SchemaForRaw[[]request], path: []string{"items"}},
		{name: "array", raw: SchemaForRaw[[2]request], path: []string{"items"}},
		{name: "map", raw: SchemaForRaw[map[string]request], path: []string{"additionalProperties"}},
		{name: "double pointer", raw: SchemaForRaw[**request]},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, err := test.raw()
			require.NoError(t, err)
			var schema map[string]any
			require.NoError(t, json.Unmarshal(raw, &schema))
			for _, key := range test.path {
				schema = schema[key].(map[string]any)
			}
			count := schema["properties"].(map[string]any)["count"].(map[string]any)
			assert.Equal(t, "string", count["type"])
		})
	}
}

func TestSchemaFor_QuotedFieldDominance(t *testing.T) {
	type Quoted struct {
		Inner int `json:"Count,string"`
	}
	type Plain struct {
		Count int
	}
	type shallowPlain struct {
		Count int
		Quoted
	}
	type shallowQuoted struct {
		Count int `json:"Count,string"`
		Plain
	}
	type taggedFirst struct {
		Quoted
		Plain
	}
	type taggedLast struct {
		Plain
		Quoted
	}
	tests := []struct {
		name  string
		raw   func() (json.RawMessage, error)
		value any
		kind  string
		json  string
	}{
		{name: "shallow unquoted before embedded", raw: SchemaForRaw[shallowPlain], value: shallowPlain{Count: 7}, kind: "integer", json: `{"Count":7}`},
		{name: "shallow quoted before embedded", raw: SchemaForRaw[shallowQuoted], value: shallowQuoted{Count: 7}, kind: "string", json: `{"Count":"7"}`},
		{name: "tagged at same depth first", raw: SchemaForRaw[taggedFirst], value: taggedFirst{Quoted: Quoted{Inner: 7}}, kind: "string", json: `{"Count":"7"}`},
		{name: "tagged at same depth last", raw: SchemaForRaw[taggedLast], value: taggedLast{Quoted: Quoted{Inner: 7}}, kind: "string", json: `{"Count":"7"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, err := test.raw()
			require.NoError(t, err)
			var schema map[string]any
			require.NoError(t, json.Unmarshal(raw, &schema))
			count := schema["properties"].(map[string]any)["Count"].(map[string]any)
			assert.Equal(t, test.kind, count["type"])
			payload, err := json.Marshal(test.value)
			require.NoError(t, err)
			assert.JSONEq(t, test.json, string(payload))
		})
	}
}

func TestSchemaFor_QuotedFieldEnums(t *testing.T) {
	type normal struct {
		Count int `json:"count,string" jsonschema:"Allowed count,enum=1,enum=2"`
	}
	type fallback struct {
		Count int `json:"count,string" jsonschema:"enum=1,enum=2"`
	}
	tests := []struct {
		name string
		raw  func() (json.RawMessage, error)
	}{
		{name: "normal", raw: SchemaForRaw[normal]},
		{name: "enum fallback", raw: SchemaForRaw[fallback]},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, err := test.raw()
			require.NoError(t, err)
			var schema map[string]any
			require.NoError(t, json.Unmarshal(raw, &schema))
			count := schema["properties"].(map[string]any)["count"].(map[string]any)
			assert.Equal(t, "string", count["type"])
			assert.Equal(t, []any{"1", "2"}, count["enum"])
		})
	}
}

func TestSchemaFor_QuotedFallbackContainers(t *testing.T) {
	type fields struct {
		Count int    `json:"count,string"`
		Mode  string `json:"mode" jsonschema:"enum=fast,enum=safe"`
	}
	type nested struct {
		Pointer *fields           `json:"pointer"`
		Slice   []fields          `json:"slice"`
		Map     map[string]fields `json:"map"`
	}
	for _, test := range []struct {
		name string
		raw  func() (json.RawMessage, error)
		path []string
		kind any
	}{
		{name: "slice", raw: SchemaForRaw[[]fields], path: []string{"items"}, kind: []any{"null", "array"}},
		{name: "array", raw: SchemaForRaw[[2]fields], path: []string{"items"}, kind: "array"},
		{name: "map", raw: SchemaForRaw[map[string]fields], path: []string{"additionalProperties"}, kind: []any{"null", "object"}},
		{name: "double pointer", raw: SchemaForRaw[**fields], kind: []any{"null", "object"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := test.raw()
			require.NoError(t, err)
			var schema map[string]any
			require.NoError(t, json.Unmarshal(raw, &schema))
			assert.Equal(t, test.kind, schema["type"])
			if test.name == "array" {
				assert.Equal(t, float64(2), schema["minItems"])
				assert.Equal(t, float64(2), schema["maxItems"])
			}
			for _, key := range test.path {
				schema = schema[key].(map[string]any)
			}
			count := schema["properties"].(map[string]any)["count"].(map[string]any)
			assert.Equal(t, "string", count["type"])
		})
	}
	raw, err := SchemaForRaw[nested]()
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(raw, &schema))
	properties := schema["properties"].(map[string]any)
	for name, kind := range map[string]string{"pointer": "object", "slice": "array", "map": "object"} {
		property := properties[name].(map[string]any)
		assert.Equal(t, []any{"null", kind}, property["type"], name)
	}
}

func TestSchemaFor_QuotedFallbackFieldSelection(t *testing.T) {
	type Quoted struct {
		Count int `json:"Count,string"`
	}
	type Plain struct{ Count int }
	type OtherQuoted struct {
		Other int `json:"Count,string"`
	}
	type shallowPlain struct {
		Count int
		Quoted
		Mode string `json:"mode" jsonschema:"enum=fast,enum=safe"`
	}
	type shallowQuoted struct {
		Count int `json:"Count,string"`
		Plain
		Mode string `json:"mode" jsonschema:"enum=fast,enum=safe"`
	}
	type taggedFirst struct {
		Quoted
		Plain
		Mode string `json:"mode" jsonschema:"enum=fast,enum=safe"`
	}
	type taggedLast struct {
		Plain
		Quoted
		Mode string `json:"mode" jsonschema:"enum=fast,enum=safe"`
	}
	type ambiguous struct {
		Quoted
		OtherQuoted
		Mode string `json:"mode" jsonschema:"enum=fast,enum=safe"`
	}
	type private struct {
		Count  int `json:"Count,string"`
		hidden int
	}
	type namedPrivate struct {
		private `json:"nested"`
		Mode    string `json:"mode" jsonschema:"enum=fast,enum=safe"`
	}
	for _, test := range []struct {
		name     string
		raw      func() (json.RawMessage, error)
		value    any
		wantJSON string
		kind     string
		nested   bool
	}{
		{name: "shallow plain", raw: SchemaForRaw[shallowPlain], value: shallowPlain{Count: 7, Mode: "fast"}, wantJSON: `{"Count":7,"mode":"fast"}`, kind: "integer"},
		{name: "shallow quoted", raw: SchemaForRaw[shallowQuoted], value: shallowQuoted{Count: 7, Mode: "fast"}, wantJSON: `{"Count":"7","mode":"fast"}`, kind: "string"},
		{name: "tagged first", raw: SchemaForRaw[taggedFirst], value: taggedFirst{Quoted: Quoted{Count: 7}, Mode: "fast"}, wantJSON: `{"Count":"7","mode":"fast"}`, kind: "string"},
		{name: "tagged last", raw: SchemaForRaw[taggedLast], value: taggedLast{Quoted: Quoted{Count: 7}, Mode: "fast"}, wantJSON: `{"Count":"7","mode":"fast"}`, kind: "string"},
		{name: "ambiguous", raw: SchemaForRaw[ambiguous], value: ambiguous{Mode: "fast"}, wantJSON: `{"mode":"fast"}`},
		{name: "named private", raw: SchemaForRaw[namedPrivate], value: namedPrivate{private: private{Count: 7, hidden: 9}, Mode: "fast"}, wantJSON: `{"nested":{"Count":"7"},"mode":"fast"}`, kind: "string", nested: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := test.raw()
			require.NoError(t, err)
			var schema map[string]any
			require.NoError(t, json.Unmarshal(raw, &schema))
			if test.nested {
				schema = schema["properties"].(map[string]any)["nested"].(map[string]any)
			}
			properties := schema["properties"].(map[string]any)
			assert.NotContains(t, properties, "hidden")
			if test.kind == "" {
				assert.NotContains(t, properties, "Count")
				assert.ElementsMatch(t, []any{"mode"}, schema["required"])
			} else {
				require.Contains(t, properties, "Count")
				assert.Equal(t, test.kind, properties["Count"].(map[string]any)["type"])
				wantRequired := []any{"Count", "mode"}
				if test.nested {
					wantRequired = []any{"Count"}
				}
				assert.ElementsMatch(t, wantRequired, schema["required"])
			}
			encoded, err := json.Marshal(test.value)
			require.NoError(t, err)
			assert.JSONEq(t, test.wantJSON, string(encoded))
		})
	}
}
