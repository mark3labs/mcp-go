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
	Slice     []int                 `json:"slice,string"`
	DoublePtr **int                 `json:"doublePtr,string"`
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
