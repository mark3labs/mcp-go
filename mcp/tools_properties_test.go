package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test property option functions with 0% coverage

func TestDefaultString(t *testing.T) {
	schema := make(map[string]any)
	opt := DefaultString("default value")
	opt(schema)
	assert.Equal(t, "default value", schema["default"])
}

func TestEnum(t *testing.T) {
	schema := make(map[string]any)
	opt := Enum("red", "green", "blue")
	opt(schema)
	assert.Equal(t, []string{"red", "green", "blue"}, schema["enum"])
}

func TestPattern(t *testing.T) {
	schema := make(map[string]any)
	opt := Pattern("^[a-z]+$")
	opt(schema)
	assert.Equal(t, "^[a-z]+$", schema["pattern"])
}

func TestDefaultNumber(t *testing.T) {
	schema := make(map[string]any)
	opt := DefaultNumber(42.5)
	opt(schema)
	assert.Equal(t, 42.5, schema["default"])
}

func TestMultipleOf(t *testing.T) {
	schema := make(map[string]any)
	opt := MultipleOf(5.0)
	opt(schema)
	assert.Equal(t, 5.0, schema["multipleOf"])
}

func TestDefaultBool(t *testing.T) {
	schema := make(map[string]any)
	opt := DefaultBool(true)
	opt(schema)
	assert.Equal(t, true, schema["default"])
}

func TestDefaultArray(t *testing.T) {
	schema := make(map[string]any)
	opt := DefaultArray([]string{"a", "b", "c"})
	opt(schema)
	assert.Equal(t, []string{"a", "b", "c"}, schema["default"])
}

func TestTitle(t *testing.T) {
	schema := make(map[string]any)
	opt := Title("Field Title")
	opt(schema)
	assert.Equal(t, "Field Title", schema["title"])
}

func TestWithBoolean(t *testing.T) {
	tool := NewTool("test",
		WithBoolean("enabled",
			Description("Enable feature"),
			Required(),
			DefaultBool(false)))

	// Marshal and verify
	data, err := json.Marshal(tool)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	schema := result["inputSchema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	enabled := properties["enabled"].(map[string]any)

	assert.Equal(t, "boolean", enabled["type"])
	assert.Equal(t, "Enable feature", enabled["description"])
	assert.Equal(t, false, enabled["default"])

	required := schema["required"].([]any)
	assert.Contains(t, required, "enabled")
}

func TestWithNumber(t *testing.T) {
	tool := NewTool("test",
		WithNumber("score",
			Description("Score value"),
			Required(),
			DefaultNumber(0.0),
			Min(0.0),
			Max(100.0),
			MultipleOf(0.5)))

	// Marshal and verify
	data, err := json.Marshal(tool)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	schema := result["inputSchema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	score := properties["score"].(map[string]any)

	assert.Equal(t, "number", score["type"])
	assert.Equal(t, "Score value", score["description"])
	assert.Equal(t, 0.0, score["default"])
	assert.Equal(t, 0.0, score["minimum"])
	assert.Equal(t, 100.0, score["maximum"])
	assert.Equal(t, 0.5, score["multipleOf"])

	required := schema["required"].([]any)
	assert.Contains(t, required, "score")
}

func TestWithRawInputSchema(t *testing.T) {
	rawSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"custom": {"type": "string"}
		}
	}`)

	// Use NewToolWithRawSchema instead of NewTool to avoid conflict
	tool := NewToolWithRawSchema("test", "Tool with raw input schema", rawSchema)

	// Verify RawInputSchema is set
	assert.NotNil(t, tool.RawInputSchema)
	assert.Equal(t, rawSchema, tool.RawInputSchema)

	// Marshal and verify
	data, err := json.Marshal(tool)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	inputSchema := result["inputSchema"].(map[string]any)
	assert.Equal(t, "object", inputSchema["type"])

	properties := inputSchema["properties"].(map[string]any)
	assert.Contains(t, properties, "custom")
}

func TestWithRawInputSchemaOption(t *testing.T) {
	// Test the WithRawInputSchema option function directly
	rawSchema := json.RawMessage(`{"type": "string"}`)

	tool := Tool{}
	opt := WithRawInputSchema(rawSchema)
	opt(&tool)

	assert.Equal(t, rawSchema, tool.RawInputSchema)
}

func TestWithRawOutputSchema(t *testing.T) {
	rawSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"result": {"type": "string"}
		}
	}`)

	tool := NewTool("test",
		WithString("input", Required()),
		WithRawOutputSchema(rawSchema))

	// Verify RawOutputSchema is set
	assert.NotNil(t, tool.RawOutputSchema)
	assert.Equal(t, rawSchema, tool.RawOutputSchema)

	// Marshal and verify
	data, err := json.Marshal(tool)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	outputSchema := result["outputSchema"].(map[string]any)
	assert.Equal(t, "object", outputSchema["type"])

	properties := outputSchema["properties"].(map[string]any)
	assert.Contains(t, properties, "result")
}

func TestToolGetName(t *testing.T) {
	tool := NewTool("my-tool")
	assert.Equal(t, "my-tool", tool.GetName())
}

func TestToolInputSchemaMarshalJSON(t *testing.T) {
	schema := ToolInputSchema{
		Type: "object",
		Properties: map[string]any{
			"name": map[string]any{"type": "string"},
		},
		Required: []string{"name"},
		Defs: map[string]any{
			"CustomType": map[string]any{"type": "string"},
		},
	}

	data, err := json.Marshal(schema)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "object", result["type"])
	assert.Contains(t, result, "properties")
	assert.Contains(t, result, "required")
	assert.Contains(t, result, "$defs")
}

func TestWithStringWithAllOptions(t *testing.T) {
	tool := NewTool("test",
		WithString("name",
			Description("User name"),
			Title("Name"),
			Required(),
			DefaultString("John"),
			MinLength(1),
			MaxLength(50),
			Pattern("^[A-Za-z]+$"),
			Enum("John", "Jane", "Bob")))

	// Marshal and verify
	data, err := json.Marshal(tool)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	schema := result["inputSchema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	name := properties["name"].(map[string]any)

	assert.Equal(t, "string", name["type"])
	assert.Equal(t, "User name", name["description"])
	assert.Equal(t, "Name", name["title"])
	assert.Equal(t, "John", name["default"])
	assert.Equal(t, float64(1), name["minLength"])
	assert.Equal(t, float64(50), name["maxLength"])
	assert.Equal(t, "^[A-Za-z]+$", name["pattern"])

	enum := name["enum"].([]any)
	assert.Len(t, enum, 3)
	assert.Contains(t, enum, "John")
	assert.Contains(t, enum, "Jane")
	assert.Contains(t, enum, "Bob")
}

func TestWithObjectWithAllOptions(t *testing.T) {
	tool := NewTool("test",
		WithObject("config",
			Description("Configuration object"),
			Title("Config"),
			Required(),
			Properties(map[string]any{
				"host": map[string]any{"type": "string"},
				"port": map[string]any{"type": "number"},
			}),
			MinProperties(1),
			MaxProperties(10),
			AdditionalProperties(map[string]any{"type": "string"}),
			PropertyNames(map[string]any{"pattern": "^[a-z]+$"})))

	// Marshal and verify
	data, err := json.Marshal(tool)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	schema := result["inputSchema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	config := properties["config"].(map[string]any)

	assert.Equal(t, "object", config["type"])
	assert.Equal(t, "Configuration object", config["description"])
	assert.Equal(t, "Config", config["title"])
	assert.Equal(t, float64(1), config["minProperties"])
	assert.Equal(t, float64(10), config["maxProperties"])

	configProps := config["properties"].(map[string]any)
	assert.Contains(t, configProps, "host")
	assert.Contains(t, configProps, "port")

	additionalProps := config["additionalProperties"].(map[string]any)
	assert.Equal(t, "string", additionalProps["type"])

	propertyNames := config["propertyNames"].(map[string]any)
	assert.Equal(t, "^[a-z]+$", propertyNames["pattern"])
}
