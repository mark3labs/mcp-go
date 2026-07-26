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
