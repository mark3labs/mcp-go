package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaFor_JSONSchemaEnumTagSpacing(t *testing.T) {
	type withLeadingSpace struct {
		Name string `json:"name" jsonschema_description:"User name to query" jsonschema:" enum=Alice,enum=Bob"`
	}
	type withoutLeadingSpace struct {
		Name string `json:"name" jsonschema_description:"User name to query" jsonschema:"enum=Alice,enum=Bob"`
	}

	tests := []struct {
		name   string
		schema func() json.RawMessage
	}{
		{
			name: "leading space",
			schema: func() json.RawMessage {
				return NewTool("get_user_info", WithInputSchema[withLeadingSpace]()).RawInputSchema
			},
		},
		{
			name: "no leading space",
			schema: func() json.RawMessage {
				return NewTool("get_user_info", WithInputSchema[withoutLeadingSpace]()).RawInputSchema
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := test.schema()
			require.NotNil(t, raw)

			var schema map[string]any
			require.NoError(t, json.Unmarshal(raw, &schema))

			properties := schema["properties"].(map[string]any)
			nameProp := properties["name"].(map[string]any)
			assert.Equal(t, "User name to query", nameProp["description"])
			assert.ElementsMatch(t, []any{"Alice", "Bob"}, nameProp["enum"])
		})
	}
}

func TestSchemaFor_UnsupportedJSONSchemaOptionReturnsError(t *testing.T) {
	type request struct {
		Name string `json:"name" jsonschema:"description=User name"`
	}

	_, err := SchemaForRaw[request]()
	require.ErrorContains(t, err, "tag must not begin with 'WORD='")
}

func TestSchemaFor_NamedAnonymousStructTags(t *testing.T) {
	type Embedded struct {
		Mode string `json:"mode" jsonschema_description:"Run mode" jsonschema:"enum=fast,enum=safe"`
	}
	type request struct {
		Embedded `json:"embedded"`
	}

	raw, err := SchemaForRaw[request]()
	require.NoError(t, err)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(raw, &schema))

	properties := schema["properties"].(map[string]any)
	assert.NotContains(t, properties, "mode")
	embedded := properties["embedded"].(map[string]any)
	embeddedProperties := embedded["properties"].(map[string]any)
	mode := embeddedProperties["mode"].(map[string]any)
	assert.Equal(t, "Run mode", mode["description"])
	assert.ElementsMatch(t, []any{"fast", "safe"}, mode["enum"])
}

func TestSchemaFor_RecursiveTaggedStructReturnsError(t *testing.T) {
	type node struct {
		Kind string `json:"kind" jsonschema:"enum=branch,enum=leaf"`
		Next *node  `json:"next,omitempty"`
	}

	_, err := SchemaForRaw[node]()
	require.ErrorIs(t, err, errRecursiveSchemaFallback)

	type EmbeddedNode struct {
		Kind string `json:"kind" jsonschema:"enum=branch,enum=leaf"`
		*EmbeddedNode
	}

	_, err = SchemaForRaw[EmbeddedNode]()
	require.ErrorIs(t, err, errRecursiveSchemaFallback)
}

func TestSchemaFor_NestedStructTags(t *testing.T) {
	type mode struct {
		Name string `json:"name" jsonschema_description:"Run mode" jsonschema:"enum=fast,enum=safe"`
	}
	type request struct {
		Primary   mode            `json:"primary"`
		Secondary mode            `json:"secondary"`
		Optional  *mode           `json:"optional"`
		Modes     []mode          `json:"modes"`
		ByName    map[string]mode `json:"byName"`
	}

	raw, err := SchemaForRaw[request]()
	require.NoError(t, err)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(raw, &schema))

	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)

	assertModeSchema := func(t *testing.T, nested any) {
		t.Helper()
		nestedSchema, ok := nested.(map[string]any)
		require.True(t, ok)
		nestedProperties, ok := nestedSchema["properties"].(map[string]any)
		require.True(t, ok)
		modeName, ok := nestedProperties["name"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "Run mode", modeName["description"])
		assert.ElementsMatch(t, []any{"fast", "safe"}, modeName["enum"])
	}

	for _, name := range []string{"primary", "secondary", "optional"} {
		assertModeSchema(t, properties[name])
	}

	modes, ok := properties["modes"].(map[string]any)
	require.True(t, ok)
	assertModeSchema(t, modes["items"])

	byName, ok := properties["byName"].(map[string]any)
	require.True(t, ok)
	assertModeSchema(t, byName["additionalProperties"])
}

func TestSchemaFor_StructuredInputOutputExampleTags(t *testing.T) {
	type WeatherRequest struct {
		Location string `json:"location,required" jsonschema_description:"City or location"` //nolint:staticcheck // required is interpreted by schemaFor, not encoding/json
		Units    string `json:"units,omitempty" jsonschema_description:"celsius or fahrenheit" jsonschema:"enum=celsius,enum=fahrenheit"`
	}

	tool := NewTool("get_weather",
		WithInputSchema[WeatherRequest](),
	)
	require.NotNil(t, tool.RawInputSchema)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(tool.RawInputSchema, &schema))

	properties := schema["properties"].(map[string]any)

	location := properties["location"].(map[string]any)
	assert.Equal(t, "City or location", location["description"])

	units := properties["units"].(map[string]any)
	assert.Equal(t, "celsius or fahrenheit", units["description"])
	assert.ElementsMatch(t, []any{"celsius", "fahrenheit"}, units["enum"])

	required := schema["required"].([]any)
	assert.Contains(t, required, "location")
	assert.NotContains(t, required, "units")
}
