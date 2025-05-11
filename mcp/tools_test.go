package mcp

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestToolWithBothSchemasError verifies that there will be feedback if the
// developer mixes raw schema with a schema provided via DSL.
func TestToolWithBothSchemasError(t *testing.T) {
	// Create a tool with both schemas set
	tool := NewTool("dual-schema-tool",
		WithDescription("A tool with both schemas set"),
		WithString("input", Description("Test input")),
	)

	_, err := json.Marshal(tool)
	assert.Nil(t, err)

	// Set the RawInputSchema as well - this should conflict with the InputSchema
	// Note: InputSchema.Type is explicitly set to "object" in NewTool
	tool.RawInputSchema = json.RawMessage(`{"type":"string"}`)

	// Attempt to marshal to JSON
	_, err = json.Marshal(tool)

	// Should return an error
	assert.ErrorIs(t, err, errToolSchemaConflict)
}

func TestToolWithRawSchema(t *testing.T) {
	// Create a complex raw schema
	rawSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Search query"},
			"limit": {"type": "integer", "minimum": 1, "maximum": 50}
		},
		"required": ["query"]
	}`)

	// Create a tool with raw schema
	tool := NewToolWithRawSchema("search-tool", "Search API", rawSchema)

	// Marshal to JSON
	data, err := json.Marshal(tool)
	assert.NoError(t, err)

	// Unmarshal to verify the structure
	var result map[string]any
	err = json.Unmarshal(data, &result)
	assert.NoError(t, err)

	// Verify tool properties
	assert.Equal(t, "search-tool", result["name"])
	assert.Equal(t, "Search API", result["description"])

	// Verify schema was properly included
	schema, ok := result["inputSchema"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "object", schema["type"])

	properties, ok := schema["properties"].(map[string]any)
	assert.True(t, ok)

	query, ok := properties["query"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "string", query["type"])

	required, ok := schema["required"].([]any)
	assert.True(t, ok)
	assert.Contains(t, required, "query")
}

func TestUnmarshalToolWithRawSchema(t *testing.T) {
	// Create a complex raw schema
	rawSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Search query"},
			"limit": {"type": "integer", "minimum": 1, "maximum": 50}
		},
		"required": ["query"]
	}`)

	// Create a tool with raw schema
	tool := NewToolWithRawSchema("search-tool", "Search API", rawSchema)

	// Marshal to JSON
	data, err := json.Marshal(tool)
	assert.NoError(t, err)

	// Unmarshal to verify the structure
	var toolUnmarshalled Tool
	err = json.Unmarshal(data, &toolUnmarshalled)
	assert.NoError(t, err)

	// Verify tool properties
	assert.Equal(t, tool.Name, toolUnmarshalled.Name)
	assert.Equal(t, tool.Description, toolUnmarshalled.Description)

	// Verify schema was properly included
	assert.Equal(t, "object", toolUnmarshalled.InputSchema.Type)
	assert.Contains(t, toolUnmarshalled.InputSchema.Properties, "query")
	assert.Subset(t, toolUnmarshalled.InputSchema.Properties["query"], map[string]any{
		"type":        "string",
		"description": "Search query",
	})
	assert.Contains(t, toolUnmarshalled.InputSchema.Properties, "limit")
	assert.Subset(t, toolUnmarshalled.InputSchema.Properties["limit"], map[string]any{
		"type":    "integer",
		"minimum": 1.0,
		"maximum": 50.0,
	})
	assert.Subset(t, toolUnmarshalled.InputSchema.Required, []string{"query"})
}

func TestUnmarshalToolWithoutRawSchema(t *testing.T) {
	// Create a tool with both schemas set
	tool := NewTool("dual-schema-tool",
		WithDescription("A tool with both schemas set"),
		WithString("input", Description("Test input")),
	)

	data, err := json.Marshal(tool)
	assert.Nil(t, err)

	// Unmarshal to verify the structure
	var toolUnmarshalled Tool
	err = json.Unmarshal(data, &toolUnmarshalled)
	assert.NoError(t, err)

	// Verify tool properties
	assert.Equal(t, tool.Name, toolUnmarshalled.Name)
	assert.Equal(t, tool.Description, toolUnmarshalled.Description)
	assert.Subset(t, toolUnmarshalled.InputSchema.Properties["input"], map[string]any{
		"type":        "string",
		"description": "Test input",
	})
	assert.Empty(t, toolUnmarshalled.InputSchema.Required)
	assert.Empty(t, toolUnmarshalled.RawInputSchema)
}

func TestToolWithObjectAndArray(t *testing.T) {
	// Create a tool with both object and array properties
	tool := NewTool("reading-list",
		WithDescription("A tool for managing reading lists"),
		WithObject("preferences",
			Description("User preferences for the reading list"),
			Properties(map[string]any{
				"theme": map[string]any{
					"type":        "string",
					"description": "UI theme preference",
					"enum":        []string{"light", "dark"},
				},
				"maxItems": map[string]any{
					"type":        "number",
					"description": "Maximum number of items in the list",
					"minimum":     1,
					"maximum":     100,
				},
			})),
		WithArray("books",
			Description("List of books to read"),
			Required(),
			Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title": map[string]any{
						"type":        "string",
						"description": "Book title",
						"required":    true,
					},
					"author": map[string]any{
						"type":        "string",
						"description": "Book author",
					},
					"year": map[string]any{
						"type":        "number",
						"description": "Publication year",
						"minimum":     1000,
					},
				},
			})))

	// Marshal to JSON
	data, err := json.Marshal(tool)
	assert.NoError(t, err)

	// Unmarshal to verify the structure
	var result map[string]any
	err = json.Unmarshal(data, &result)
	assert.NoError(t, err)

	// Verify tool properties
	assert.Equal(t, "reading-list", result["name"])
	assert.Equal(t, "A tool for managing reading lists", result["description"])

	// Verify schema was properly included
	schema, ok := result["inputSchema"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "object", schema["type"])

	// Verify properties
	properties, ok := schema["properties"].(map[string]any)
	assert.True(t, ok)

	// Verify preferences object
	preferences, ok := properties["preferences"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "object", preferences["type"])
	assert.Equal(t, "User preferences for the reading list", preferences["description"])

	prefProps, ok := preferences["properties"].(map[string]any)
	assert.True(t, ok)
	assert.Contains(t, prefProps, "theme")
	assert.Contains(t, prefProps, "maxItems")

	// Verify books array
	books, ok := properties["books"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "array", books["type"])
	assert.Equal(t, "List of books to read", books["description"])

	// Verify array items schema
	items, ok := books["items"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "object", items["type"])

	itemProps, ok := items["properties"].(map[string]any)
	assert.True(t, ok)
	assert.Contains(t, itemProps, "title")
	assert.Contains(t, itemProps, "author")
	assert.Contains(t, itemProps, "year")

	// Verify required fields
	required, ok := schema["required"].([]any)
	assert.True(t, ok)
	assert.Contains(t, required, "books")
}

func TestParseToolCallToolRequest(t *testing.T) {
	request := CallToolRequest{}
	request.Params.Name = "test-tool"
	request.Params.Arguments = map[string]any{
		"bool_value":    "true",
		"int64_value":   "123456789",
		"int32_value":   "123456789",
		"int16_value":   "123456789",
		"int8_value":    "123456789",
		"int_value":     "123456789",
		"uint_value":    "123456789",
		"uint64_value":  "123456789",
		"uint32_value":  "123456789",
		"uint16_value":  "123456789",
		"uint8_value":   "123456789",
		"float32_value": "3.14",
		"float64_value": "3.1415926",
		"string_value":  "hello",
	}
	param1 := ParseBoolean(request, "bool_value", false)
	assert.Equal(t, fmt.Sprintf("%T", param1), "bool")

	param2 := ParseInt64(request, "int64_value", 1)
	assert.Equal(t, fmt.Sprintf("%T", param2), "int64")

	param3 := ParseInt32(request, "int32_value", 1)
	assert.Equal(t, fmt.Sprintf("%T", param3), "int32")

	param4 := ParseInt16(request, "int16_value", 1)
	assert.Equal(t, fmt.Sprintf("%T", param4), "int16")

	param5 := ParseInt8(request, "int8_value", 1)
	assert.Equal(t, fmt.Sprintf("%T", param5), "int8")

	param6 := ParseInt(request, "int_value", 1)
	assert.Equal(t, fmt.Sprintf("%T", param6), "int")

	param7 := ParseUInt(request, "uint_value", 1)
	assert.Equal(t, fmt.Sprintf("%T", param7), "uint")

	param8 := ParseUInt64(request, "uint64_value", 1)
	assert.Equal(t, fmt.Sprintf("%T", param8), "uint64")

	param9 := ParseUInt32(request, "uint32_value", 1)
	assert.Equal(t, fmt.Sprintf("%T", param9), "uint32")

	param10 := ParseUInt16(request, "uint16_value", 1)
	assert.Equal(t, fmt.Sprintf("%T", param10), "uint16")

	param11 := ParseUInt8(request, "uint8_value", 1)
	assert.Equal(t, fmt.Sprintf("%T", param11), "uint8")

	param12 := ParseFloat32(request, "float32_value", 1.0)
	assert.Equal(t, fmt.Sprintf("%T", param12), "float32")

	param13 := ParseFloat64(request, "float64_value", 1.0)
	assert.Equal(t, fmt.Sprintf("%T", param13), "float64")

	param14 := ParseString(request, "string_value", "")
	assert.Equal(t, fmt.Sprintf("%T", param14), "string")

	param15 := ParseInt64(request, "string_value", 1)
	assert.Equal(t, fmt.Sprintf("%T", param15), "int64")
	t.Logf("param15 type: %T,value:%v", param15, param15)

}
