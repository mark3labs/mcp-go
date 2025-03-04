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