package mcp

import (
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaForRaw_InvalidJSONTagName(t *testing.T) {
	type request struct {
		Value string `json:"bad\\name" jsonschema_description:"The value"`
	}
	raw, err := SchemaForRaw[request]()
	require.NoError(t, err)
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(raw, &schema))
	require.Contains(t, schema.Properties, "Value")
	require.NotContains(t, schema.Properties, `bad\name`)
	resolved, err := schema.Resolve(nil)
	require.NoError(t, err)
	payload, err := json.Marshal(request{Value: "hello"})
	require.NoError(t, err)
	require.JSONEq(t, `{"Value":"hello"}`, string(payload))
	var value any
	require.NoError(t, json.Unmarshal(payload, &value))
	require.NoError(t, resolved.Validate(value))
}

func TestSchemaForRaw_JSONTagNames(t *testing.T) {
	type Fields struct {
		Backslash string `json:"bad\\name" jsonschema_description:"Preserved description"`
		Quote     int    `json:"bad\"name,string"`
		Control   string `json:"bad\tname,omitempty"`
		Symbol    string `json:"snow☃man"`
		Valid     string `json:"温度 !#$%&()*+-./:;<=>?@[]^_{|}~"`
		Dash      string `json:"-,"`
		Hidden    string `json:"-"`
	}
	type fallback struct {
		Fields
		Mode string `json:"mode" jsonschema:"enum=fast,enum=safe"`
	}
	fields := Fields{Backslash: "hello", Quote: 7, Symbol: "snow", Valid: "valid", Dash: "dash", Hidden: "hidden"}
	tests := []struct {
		name      string
		raw       func() (json.RawMessage, error)
		value     any
		wantCount int
	}{
		{name: "native", raw: SchemaForRaw[Fields], value: fields, wantCount: 6},
		{name: "enum fallback", raw: SchemaForRaw[fallback], value: fallback{Fields: fields, Mode: "fast"}, wantCount: 7},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, err := test.raw()
			require.NoError(t, err)
			var schema jsonschema.Schema
			require.NoError(t, json.Unmarshal(raw, &schema))
			for _, name := range []string{"Backslash", "Quote", "Control", "Symbol", "温度 !#$%&()*+-./:;<=>?@[]^_{|}~", "-"} {
				require.Contains(t, schema.Properties, name)
			}
			assert.Len(t, schema.Properties, test.wantCount)
			assert.Equal(t, "Preserved description", schema.Properties["Backslash"].Description)
			assert.Equal(t, "string", schema.Properties["Quote"].Type)
			assert.NotContains(t, schema.Required, "Control")
			payload, err := json.Marshal(test.value)
			require.NoError(t, err)
			var value map[string]any
			require.NoError(t, json.Unmarshal(payload, &value))
			assert.Equal(t, "7", value["Quote"])
			assert.NotContains(t, value, "Hidden")
			resolved, err := schema.Resolve(nil)
			require.NoError(t, err)
			require.NoError(t, resolved.Validate(value))
		})
	}
}

func TestSchemaForRaw_InvalidJSONTagEmbedding(t *testing.T) {
	type Embedded struct {
		Value string `json:"value"`
	}
	type request struct {
		Embedded `json:"bad\\name"`
	}
	raw, err := SchemaForRaw[request]()
	require.NoError(t, err)
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(raw, &schema))
	require.Contains(t, schema.Properties, "value")
	require.Len(t, schema.Properties, 1)
}

func TestSchemaForRaw_InvalidJSONTagDominance(t *testing.T) {
	type Invalid struct {
		Value string `json:"bad\\name"`
	}
	type Tagged struct {
		Other int `json:"Value"`
	}
	type request struct {
		Invalid
		Tagged
	}
	raw, err := SchemaForRaw[request]()
	require.NoError(t, err)
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(raw, &schema))
	require.Len(t, schema.Properties, 1)
	require.Contains(t, schema.Properties, "Value")
	require.Equal(t, "integer", schema.Properties["Value"].Type)
	payload, err := json.Marshal(request{Invalid: Invalid{Value: "ignored"}, Tagged: Tagged{Other: 7}})
	require.NoError(t, err)
	require.JSONEq(t, `{"Value":7}`, string(payload))
}
