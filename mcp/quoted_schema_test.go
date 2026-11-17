package mcp

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
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
			assert.Equal(t, []any{"Count"}, schema["required"])
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
		OtherQuoted        //nolint:govet // Verify that encoding/json omits fields with ambiguous JSON tags.
		Mode        string `json:"mode" jsonschema:"enum=fast,enum=safe"`
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

type quotedEnumString string

type quotedEnumNumber json.Number

type quotedEnumMarshaler string

func (v quotedEnumMarshaler) MarshalJSON() ([]byte, error) { return json.Marshal(string(v)) }

func TestSchemaFor_QuotedStringEnums(t *testing.T) {
	type Fields struct {
		JSONNumber    json.Number         `json:"jsonNumber,string" jsonschema:"Allowed number,enum=1,enum=2"`
		NamedNumber   quotedEnumNumber    `json:"namedNumber,string" jsonschema:"Allowed named,enum=1,enum=2"`
		Mode          string              `json:"mode,string" jsonschema:"Allowed modes,enum=fast,enum=a\\b,enum=a\"b,enum=a\nb,enum=<>&,enum=雪"`
		Named         quotedEnumString    `json:"named,string" jsonschema:"Allowed named,enum=fast,enum=safe"`
		Pointer       *quotedEnumString   `json:"pointer,string" jsonschema:"Allowed pointer,enum=fast,enum=safe"`
		IntPointer    *int                `json:"intPointer,string" jsonschema:"Allowed integers,enum=1,enum=2"`
		BoolPointer   *bool               `json:"boolPointer,string" jsonschema:"Allowed booleans,enum=true,enum=false"`
		NumberPointer *json.Number        `json:"numberPointer,string" jsonschema:"Allowed numbers,enum=1,enum=2"`
		Number        int                 `json:"number,string" jsonschema:"Allowed number,enum=1,enum=2"`
		Bool          bool                `json:"bool,string" jsonschema:"Allowed boolean,enum=true,enum=false"`
		Plain         string              `json:"plain" jsonschema:"Allowed plain,enum=fast,enum=safe"`
		Custom        quotedEnumMarshaler `json:"custom,string" jsonschema:"Allowed custom,enum=fast,enum=safe"`
	}
	type fallback struct {
		Fields
		Trigger string `json:"trigger" jsonschema:"enum=fast,enum=safe"`
	}
	_, err := jsonschema.For[Fields](&jsonschema.ForOptions{IgnoreInvalidTypes: true})
	require.NoError(t, err, "normal fixture must not require fallback")
	_, err = jsonschema.For[fallback](&jsonschema.ForOptions{IgnoreInvalidTypes: true})
	require.ErrorContains(t, err, "tag must not begin with 'WORD='")

	for _, test := range []struct {
		name string
		raw  func() (json.RawMessage, error)
	}{
		{name: "normal", raw: SchemaForRaw[Fields]},
		{name: "fallback", raw: SchemaForRaw[fallback]},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := test.raw()
			require.NoError(t, err)
			var schema map[string]any
			require.NoError(t, json.Unmarshal(raw, &schema))
			properties := schema["properties"].(map[string]any)
			mode := properties["mode"].(map[string]any)
			want := []any{"\"fast\"", "\"a\\\\b\"", "\"a\\\"b\"", "\"a\\nb\"", "\"\\u003c\\u003e\\u0026\"", "\"雪\""}
			assert.Equal(t, want, mode["enum"])
			assert.Equal(t, []any{"1", "2"}, properties["jsonNumber"].(map[string]any)["enum"])
			assert.Equal(t, []any{`"1"`, `"2"`}, properties["namedNumber"].(map[string]any)["enum"])
			for name, value := range map[string]any{
				"jsonNumber": struct {
					Value json.Number `json:"value,string"`
				}{Value: "1"},
				"namedNumber": struct {
					Value quotedEnumNumber `json:"value,string"`
				}{Value: "1"},
			} {
				encoded, err := json.Marshal(value)
				require.NoError(t, err)
				var document map[string]any
				require.NoError(t, json.Unmarshal(encoded, &document))
				assert.Contains(t, properties[name].(map[string]any)["enum"], document["value"])
			}
			assert.Equal(t, []any{`"fast"`, `"safe"`}, properties["named"].(map[string]any)["enum"])
			assert.Equal(t, []any{`"fast"`, `"safe"`, nil}, properties["pointer"].(map[string]any)["enum"])
			assert.Equal(t, []any{"1", "2", nil}, properties["intPointer"].(map[string]any)["enum"])
			assert.Equal(t, []any{"true", "false", nil}, properties["boolPointer"].(map[string]any)["enum"])
			assert.Equal(t, []any{"1", "2", nil}, properties["numberPointer"].(map[string]any)["enum"])
			assert.Equal(t, []any{"null", "string"}, properties["pointer"].(map[string]any)["type"])
			assert.Equal(t, []any{"1", "2"}, properties["number"].(map[string]any)["enum"])
			assert.Equal(t, []any{"true", "false"}, properties["bool"].(map[string]any)["enum"])
			for _, name := range []string{"plain", "custom"} {
				assert.Equal(t, []any{"fast", "safe"}, properties[name].(map[string]any)["enum"])
			}
			for _, value := range []string{"fast", "a\\b", "a\"b", "a\nb", "<>&", "雪"} {
				encoded, err := json.Marshal(struct {
					Mode string `json:"mode,string"`
				}{Mode: value})
				require.NoError(t, err)
				var document map[string]any
				require.NoError(t, json.Unmarshal(encoded, &document))
				assert.Contains(t, mode["enum"], document["mode"])
			}
		})
	}
}

func TestSchemaFor_QuotedRecursiveEmbedding(t *testing.T) {
	type Recursive struct {
		*Recursive
		Count int `json:"count,string"`
	}
	type Node struct {
		Count int `json:"count,string"`
	}
	type ShadowNode struct {
		Other int `json:"other"`
	}
	type RecursiveShadow struct {
		*RecursiveShadow
		Hidden ShadowNode `json:"node,omitempty"` //nolint:modernize // Preserve omitempty to exercise native optional-field metadata before shadow selection.
	}
	type Request struct {
		Node Node `json:"node"`
		RecursiveShadow
	}
	for _, test := range []struct {
		name   string
		native func() (*jsonschema.Schema, error)
		raw    func() (json.RawMessage, error)
		value  any
		path   []string
		want   string
	}{
		{name: "normal", native: func() (*jsonschema.Schema, error) { return jsonschema.For[Recursive](nil) }, raw: SchemaForRaw[Recursive], value: Recursive{Count: 5}, want: `{"count":"5"}`},
		{name: "shadowed", native: func() (*jsonschema.Schema, error) { return jsonschema.For[Request](nil) }, raw: SchemaForRaw[Request], value: Request{Node: Node{Count: 5}}, path: []string{"node"}, want: `{"node":{"count":"5"}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.native()
			require.NoError(t, err, "native inference supports this recursive embedding")
			raw, err := test.raw()
			require.NoError(t, err)
			var schema map[string]any
			require.NoError(t, json.Unmarshal(raw, &schema))
			for _, name := range test.path {
				schema = schema["properties"].(map[string]any)[name].(map[string]any)
			}
			assert.Equal(t, "string", schema["properties"].(map[string]any)["count"].(map[string]any)["type"])
			encoded, err := json.Marshal(test.value)
			require.NoError(t, err)
			assert.JSONEq(t, test.want, string(encoded))
		})
	}
}

type quotedNativeNode struct {
	Count int `json:"count"`
}
type quotedNativeOther struct {
	Other int `json:"other"`
}
type quotedNativeShadow struct {
	Hidden quotedNativeOther `json:"node,omitempty"` //nolint:modernize // Preserve omitempty to exercise native optional-field metadata before shadow selection.
}
type quotedNativeMarshaler struct {
	Node quotedNativeNode `json:"node"`
	quotedNativeShadow
}

func (v quotedNativeMarshaler) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Node quotedNativeOther `json:"node"`
	}{Node: v.Hidden})
}

func TestSchemaFor_QuotedNativeFieldSelection(t *testing.T) {
	type Node struct {
		Count int `json:"count,string"`
	}
	type Other struct {
		Other int `json:"other"`
	}
	type Shadow struct {
		Hidden Other `json:"node,omitempty"` //nolint:modernize // Preserve omitempty to exercise native optional-field metadata before shadow selection.
	}
	type private struct {
		Label string `json:"label,string" jsonschema:"Private label"`
	}
	type Fields struct {
		Node Node `json:"node"`
		Shadow
		private
		When  time.Time       `json:"when"`
		Raw   json.RawMessage `json:"raw"`
		Small int8            `json:"small" jsonschema_description:"Small count"`
	}
	nativeFields, nativeErr := jsonschema.For[Fields](nil)
	require.NoError(t, nativeErr)
	require.Equal(t, "Private label", nativeFields.Properties["label"].Description)
	for _, test := range []struct {
		name string
		raw  func() (json.RawMessage, error)
		path []string
		kind any
	}{
		{name: "slice", raw: SchemaForRaw[[]Fields], path: []string{"items"}, kind: []any{"null", "array"}},
		{name: "array", raw: SchemaForRaw[[2]Fields], path: []string{"items"}, kind: "array"},
		{name: "map", raw: SchemaForRaw[map[string]Fields], path: []string{"additionalProperties"}, kind: "object"},
		{name: "double pointer", raw: SchemaForRaw[**Fields], kind: []any{"null", "object"}},
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
			props := schema["properties"].(map[string]any)
			assert.Equal(t, "Private label", props["label"].(map[string]any)["description"])
			node := props["node"].(map[string]any)
			nodeProps := node["properties"].(map[string]any)
			require.Contains(t, nodeProps, "count")
			assert.NotContains(t, nodeProps, "other")
			assert.Equal(t, "string", nodeProps["count"].(map[string]any)["type"])
			assert.ElementsMatch(t, []any{"node", "when", "raw", "small", "label"}, schema["required"])
			small := props["small"].(map[string]any)
			assert.Equal(t, float64(-128), small["minimum"])
			assert.Equal(t, float64(127), small["maximum"])
			assert.Equal(t, "Small count", small["description"])
			nativeTime, err := jsonschema.For[time.Time](nil)
			require.NoError(t, err)
			wantTime, err := json.Marshal(nativeTime)
			require.NoError(t, err)
			gotTime, err := json.Marshal(props["when"])
			require.NoError(t, err)
			assert.JSONEq(t, string(wantTime), string(gotTime))
			nativeRaw, err := jsonschema.For[json.RawMessage](nil)
			require.NoError(t, err)
			wantRaw, err := json.Marshal(nativeRaw)
			require.NoError(t, err)
			gotRaw, err := json.Marshal(props["raw"])
			require.NoError(t, err)
			assert.JSONEq(t, string(wantRaw), string(gotRaw))
		})
	}
	type Embedded struct {
		Value int `json:"embedded,string"`
	}
	type distinctNames struct {
		Value string `json:"outer"`
		Embedded
	}
	raw, err := SchemaForRaw[distinctNames]()
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(raw, &schema))
	props := schema["properties"].(map[string]any)
	require.Contains(t, props, "outer")
	require.Contains(t, props, "embedded")
	assert.Equal(t, "string", props["embedded"].(map[string]any)["type"])
	assert.ElementsMatch(t, []any{"outer", "embedded"}, schema["required"])
	encoded, err := json.Marshal(distinctNames{Value: "x", Embedded: Embedded{Value: 5}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"outer":"x","embedded":"5"}`, string(encoded))

	custom, err := SchemaForRaw[quotedNativeMarshaler]()
	require.NoError(t, err)
	native, err := jsonschema.For[quotedNativeMarshaler](nil)
	require.NoError(t, err)
	wantCustom, err := json.Marshal(native)
	require.NoError(t, err)
	assert.JSONEq(t, string(wantCustom), string(custom))
	encoded, err = json.Marshal(quotedNativeMarshaler{quotedNativeShadow: quotedNativeShadow{Hidden: quotedNativeOther{Other: 5}}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"node":{"other":5}}`, string(encoded))
}

func TestSchemaFor_QuotedSelectedAnnotations(t *testing.T) {
	type Shadow struct {
		Old string `json:"mode,string" jsonschema:"Old mode,enum=old"`
	}
	type fields struct {
		Mode string `json:"mode,string"`
		Shadow
		Plain string `json:"plain" jsonschema:"Plain mode,enum=plain"`
	}
	raw, err := SchemaForRaw[fields]()
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(raw, &schema))
	properties := schema["properties"].(map[string]any)
	mode := properties["mode"].(map[string]any)
	assert.Equal(t, "string", mode["type"])
	assert.NotContains(t, mode, "enum")
	assert.NotContains(t, mode, "description")
	plain := properties["plain"].(map[string]any)
	assert.Equal(t, []any{"plain"}, plain["enum"])
	assert.Equal(t, "Plain mode", plain["description"])
}

type quotedPointerJSONStruct struct {
	Count int `json:"count,string"`
}

func (value *quotedPointerJSONStruct) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Count int `json:"count"`
	}{Count: value.Count})
}

type quotedTextStruct struct {
	Count int `json:"count,string"`
}

func (quotedTextStruct) MarshalText() ([]byte, error) { return []byte("opaque"), nil }

func TestSchemaFor_QuotedCustomStructEncoding(t *testing.T) {
	type nestedJSON struct {
		Value *quotedPointerJSONStruct `json:"value"`
	}
	type nestedText struct {
		Value quotedTextStruct `json:"value"`
	}
	for _, test := range []struct {
		name   string
		raw    func() (json.RawMessage, error)
		native func(*jsonschema.ForOptions) (*jsonschema.Schema, error)
	}{
		{name: "pointer JSON root", raw: SchemaForRaw[*quotedPointerJSONStruct], native: jsonschema.For[*quotedPointerJSONStruct]},
		{name: "pointer JSON nested", raw: SchemaForRaw[nestedJSON], native: jsonschema.For[nestedJSON]},
		{name: "pointer JSON slice", raw: SchemaForRaw[[]*quotedPointerJSONStruct], native: jsonschema.For[[]*quotedPointerJSONStruct]},
		{name: "text root", raw: SchemaForRaw[quotedTextStruct], native: jsonschema.For[quotedTextStruct]},
		{name: "text nested", raw: SchemaForRaw[nestedText], native: jsonschema.For[nestedText]},
		{name: "text map", raw: SchemaForRaw[map[string]quotedTextStruct], native: jsonschema.For[map[string]quotedTextStruct]},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := test.raw()
			require.NoError(t, err)
			native, err := test.native(&jsonschema.ForOptions{IgnoreInvalidTypes: true})
			require.NoError(t, err)
			want, err := json.Marshal(native)
			require.NoError(t, err)
			assert.JSONEq(t, string(want), string(raw))
		})
	}
}
