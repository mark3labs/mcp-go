package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaFor_JSONSchemaDescriptionTag(t *testing.T) {
	type GetUserInfoRequest struct {
		Name string `json:"name" jsonschema_description:"User name to query" jsonschema:" enum=Alice,enum=Bob"`
	}

	tool := NewTool("get_user_info",
		WithInputSchema[GetUserInfoRequest](),
	)
	require.NotNil(t, tool.RawInputSchema)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(tool.RawInputSchema, &schema))

	properties := schema["properties"].(map[string]any)
	nameProp := properties["name"].(map[string]any)

	assert.Equal(t, "User name to query", nameProp["description"])
	assert.ElementsMatch(t, []any{"Alice", "Bob"}, nameProp["enum"])
}

func TestSchemaFor_JSONSchemaEnumTagWithoutLeadingSpace(t *testing.T) {
	type GetUserInfoRequest struct {
		Name string `json:"name" jsonschema_description:"User name to query" jsonschema:"enum=Alice,enum=Bob"`
	}

	tool := NewTool("get_user_info",
		WithInputSchema[GetUserInfoRequest](),
	)
	require.NotNil(t, tool.RawInputSchema)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(tool.RawInputSchema, &schema))

	properties := schema["properties"].(map[string]any)
	nameProp := properties["name"].(map[string]any)

	assert.Equal(t, "User name to query", nameProp["description"])
	assert.ElementsMatch(t, []any{"Alice", "Bob"}, nameProp["enum"])
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
