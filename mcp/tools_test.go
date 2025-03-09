package mcp

import (
	"encoding/json"
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
	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	assert.NoError(t, err)

	// Verify tool properties
	assert.Equal(t, "search-tool", result["name"])
	assert.Equal(t, "Search API", result["description"])

	// Verify schema was properly included
	schema, ok := result["inputSchema"].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "object", schema["type"])

	properties, ok := schema["properties"].(map[string]interface{})
	assert.True(t, ok)

	query, ok := properties["query"].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "string", query["type"])

	required, ok := schema["required"].([]interface{})
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
	assert.Subset(t, toolUnmarshalled.InputSchema.Properties["query"], map[string]interface{}{
		"type":        "string",
		"description": "Search query",
	})
	assert.Contains(t, toolUnmarshalled.InputSchema.Properties, "limit")
	assert.Subset(t, toolUnmarshalled.InputSchema.Properties["limit"], map[string]interface{}{
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
	assert.Subset(t, toolUnmarshalled.InputSchema.Properties["input"], map[string]interface{}{
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
			Properties(map[string]interface{}{
				"theme": map[string]interface{}{
					"type":        "string",
					"description": "UI theme preference",
					"enum":        []string{"light", "dark"},
				},
				"maxItems": map[string]interface{}{
					"type":        "number",
					"description": "Maximum number of items in the list",
					"minimum":     1,
					"maximum":     100,
				},
			})),
		WithArray("books",
			Description("List of books to read"),
			Required(),
			Items(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Book title",
						"required":    true,
					},
					"author": map[string]interface{}{
						"type":        "string",
						"description": "Book author",
					},
					"year": map[string]interface{}{
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
	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	assert.NoError(t, err)

	// Verify tool properties
	assert.Equal(t, "reading-list", result["name"])
	assert.Equal(t, "A tool for managing reading lists", result["description"])

	// Verify schema was properly included
	schema, ok := result["inputSchema"].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "object", schema["type"])

	// Verify properties
	properties, ok := schema["properties"].(map[string]interface{})
	assert.True(t, ok)

	// Verify preferences object
	preferences, ok := properties["preferences"].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "object", preferences["type"])
	assert.Equal(t, "User preferences for the reading list", preferences["description"])

	prefProps, ok := preferences["properties"].(map[string]interface{})
	assert.True(t, ok)
	assert.Contains(t, prefProps, "theme")
	assert.Contains(t, prefProps, "maxItems")

	// Verify books array
	books, ok := properties["books"].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "array", books["type"])
	assert.Equal(t, "List of books to read", books["description"])

	// Verify array items schema
	items, ok := books["items"].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "object", items["type"])

	itemProps, ok := items["properties"].(map[string]interface{})
	assert.True(t, ok)
	assert.Contains(t, itemProps, "title")
	assert.Contains(t, itemProps, "author")
	assert.Contains(t, itemProps, "year")

	// Verify required fields
	required, ok := schema["required"].([]interface{})
	assert.True(t, ok)
	assert.Contains(t, required, "books")
}
