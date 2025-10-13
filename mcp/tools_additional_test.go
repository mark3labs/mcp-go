package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test edge cases for CallToolRequest methods

func TestCallToolRequest_GetArgumentsWithNilArguments(t *testing.T) {
	req := CallToolRequest{}
	req.Params.Name = "test-tool"
	req.Params.Arguments = nil

	args := req.GetArguments()
	assert.Nil(t, args)
}

func TestCallToolRequest_BindArgumentsWithInvalidTarget(t *testing.T) {
	req := CallToolRequest{}
	req.Params.Arguments = map[string]any{"key": "value"}

	t.Run("nil target", func(t *testing.T) {
		err := req.BindArguments(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "non-nil pointer")
	})

	t.Run("non-pointer target", func(t *testing.T) {
		var target struct{ Key string }
		err := req.BindArguments(target)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "non-nil pointer")
	})
}

func TestCallToolRequest_TypeConversionEdgeCases(t *testing.T) {
	req := CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"string_as_int":   "not a number",
		"string_as_float": "not a float",
		"string_as_bool":  "maybe",
		"int_as_bool":     5,
		"float_as_bool":   3.14,
		"object_val":      map[string]any{"nested": "value"},
	}

	t.Run("GetInt with invalid string", func(t *testing.T) {
		result := req.GetInt("string_as_int", 42)
		assert.Equal(t, 42, result)
	})

	t.Run("GetFloat with invalid string", func(t *testing.T) {
		result := req.GetFloat("string_as_float", 1.5)
		assert.Equal(t, 1.5, result)
	})

	t.Run("GetBool with invalid string", func(t *testing.T) {
		result := req.GetBool("string_as_bool", false)
		assert.Equal(t, false, result)
	})

	t.Run("GetBool with non-zero int", func(t *testing.T) {
		result := req.GetBool("int_as_bool", false)
		assert.True(t, result)
	})

	t.Run("GetBool with non-zero float", func(t *testing.T) {
		result := req.GetBool("float_as_bool", false)
		assert.True(t, result)
	})

	t.Run("RequireInt with wrong type", func(t *testing.T) {
		_, err := req.RequireInt("object_val")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not an int")
	})

	t.Run("RequireFloat with wrong type", func(t *testing.T) {
		_, err := req.RequireFloat("object_val")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a float64")
	})

	t.Run("RequireBool with wrong type", func(t *testing.T) {
		_, err := req.RequireBool("object_val")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a bool")
	})
}

func TestCallToolRequest_SliceWithMixedTypes(t *testing.T) {
	req := CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"mixed_string_slice": []any{"valid", 123, "another"},
		"mixed_int_slice":    []any{1, "not a number", 3},
		"mixed_float_slice":  []any{1.1, "not a float", 3.3},
		"mixed_bool_slice":   []any{true, "not a bool", false},
	}

	t.Run("GetStringSlice with mixed types", func(t *testing.T) {
		result := req.GetStringSlice("mixed_string_slice", nil)
		// Should only include valid strings
		assert.Equal(t, []string{"valid", "another"}, result)
	})

	t.Run("RequireStringSlice with non-string element", func(t *testing.T) {
		_, err := req.RequireStringSlice("mixed_string_slice")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a string")
	})

	t.Run("GetIntSlice with mixed types", func(t *testing.T) {
		result := req.GetIntSlice("mixed_int_slice", nil)
		// Should only include convertible values
		assert.Equal(t, []int{1, 3}, result)
	})

	t.Run("RequireIntSlice with non-convertible element", func(t *testing.T) {
		_, err := req.RequireIntSlice("mixed_int_slice")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be converted to int")
	})

	t.Run("GetFloatSlice with mixed types", func(t *testing.T) {
		result := req.GetFloatSlice("mixed_float_slice", nil)
		assert.Equal(t, []float64{1.1, 3.3}, result)
	})

	t.Run("RequireFloatSlice with non-convertible element", func(t *testing.T) {
		_, err := req.RequireFloatSlice("mixed_float_slice")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be converted to float64")
	})

	t.Run("GetBoolSlice with mixed types", func(t *testing.T) {
		result := req.GetBoolSlice("mixed_bool_slice", nil)
		// Should skip invalid values
		assert.Equal(t, []bool{true, false}, result)
	})

	t.Run("RequireBoolSlice with non-convertible element", func(t *testing.T) {
		_, err := req.RequireBoolSlice("mixed_bool_slice")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be converted to bool")
	})
}

// Test Property Options

func TestPropertyOptions(t *testing.T) {
	t.Run("MaxProperties", func(t *testing.T) {
		schema := make(map[string]any)
		opt := MaxProperties(10)
		opt(schema)
		assert.Equal(t, 10, schema["maxProperties"])
	})

	t.Run("MinProperties", func(t *testing.T) {
		schema := make(map[string]any)
		opt := MinProperties(2)
		opt(schema)
		assert.Equal(t, 2, schema["minProperties"])
	})

	t.Run("PropertyNames", func(t *testing.T) {
		schema := make(map[string]any)
		nameSchema := map[string]any{"pattern": "^[a-z]+$"}
		opt := PropertyNames(nameSchema)
		opt(schema)
		assert.Equal(t, nameSchema, schema["propertyNames"])
	})

	t.Run("AdditionalProperties with bool", func(t *testing.T) {
		schema := make(map[string]any)
		opt := AdditionalProperties(false)
		opt(schema)
		assert.Equal(t, false, schema["additionalProperties"])
	})

	t.Run("AdditionalProperties with schema", func(t *testing.T) {
		schema := make(map[string]any)
		propSchema := map[string]any{"type": "string"}
		opt := AdditionalProperties(propSchema)
		opt(schema)
		assert.Equal(t, propSchema, schema["additionalProperties"])
	})
}

// Test Array Options

func TestArrayOptions(t *testing.T) {
	t.Run("MinItems", func(t *testing.T) {
		schema := make(map[string]any)
		opt := MinItems(1)
		opt(schema)
		assert.Equal(t, 1, schema["minItems"])
	})

	t.Run("MaxItems", func(t *testing.T) {
		schema := make(map[string]any)
		opt := MaxItems(100)
		opt(schema)
		assert.Equal(t, 100, schema["maxItems"])
	})

	t.Run("UniqueItems", func(t *testing.T) {
		schema := make(map[string]any)
		opt := UniqueItems(true)
		opt(schema)
		assert.Equal(t, true, schema["uniqueItems"])
	})

	t.Run("Items with custom schema", func(t *testing.T) {
		schema := make(map[string]any)
		itemSchema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
		}
		opt := Items(itemSchema)
		opt(schema)
		assert.Equal(t, itemSchema, schema["items"])
	})
}

// Test Tool Annotations

func TestToolAnnotations(t *testing.T) {
	t.Run("WithTitleAnnotation", func(t *testing.T) {
		tool := NewTool("test")
		opt := WithTitleAnnotation("Test Tool")
		opt(&tool)
		assert.Equal(t, "Test Tool", tool.Annotations.Title)
	})

	t.Run("WithReadOnlyHintAnnotation", func(t *testing.T) {
		tool := NewTool("test")
		opt := WithReadOnlyHintAnnotation(true)
		opt(&tool)
		require.NotNil(t, tool.Annotations.ReadOnlyHint)
		assert.True(t, *tool.Annotations.ReadOnlyHint)
	})

	t.Run("WithDestructiveHintAnnotation", func(t *testing.T) {
		tool := NewTool("test")
		opt := WithDestructiveHintAnnotation(false)
		opt(&tool)
		require.NotNil(t, tool.Annotations.DestructiveHint)
		assert.False(t, *tool.Annotations.DestructiveHint)
	})

	t.Run("WithIdempotentHintAnnotation", func(t *testing.T) {
		tool := NewTool("test")
		opt := WithIdempotentHintAnnotation(true)
		opt(&tool)
		require.NotNil(t, tool.Annotations.IdempotentHint)
		assert.True(t, *tool.Annotations.IdempotentHint)
	})

	t.Run("WithOpenWorldHintAnnotation", func(t *testing.T) {
		tool := NewTool("test")
		opt := WithOpenWorldHintAnnotation(false)
		opt(&tool)
		require.NotNil(t, tool.Annotations.OpenWorldHint)
		assert.False(t, *tool.Annotations.OpenWorldHint)
	})

	t.Run("WithToolAnnotation full", func(t *testing.T) {
		tool := NewTool("test")
		annotation := ToolAnnotation{
			Title:           "Custom Tool",
			ReadOnlyHint:    ToBoolPtr(true),
			DestructiveHint: ToBoolPtr(false),
			IdempotentHint:  ToBoolPtr(true),
			OpenWorldHint:   ToBoolPtr(false),
		}
		opt := WithToolAnnotation(annotation)
		opt(&tool)

		assert.Equal(t, "Custom Tool", tool.Annotations.Title)
		require.NotNil(t, tool.Annotations.ReadOnlyHint)
		assert.True(t, *tool.Annotations.ReadOnlyHint)
		require.NotNil(t, tool.Annotations.DestructiveHint)
		assert.False(t, *tool.Annotations.DestructiveHint)
		require.NotNil(t, tool.Annotations.IdempotentHint)
		assert.True(t, *tool.Annotations.IdempotentHint)
		require.NotNil(t, tool.Annotations.OpenWorldHint)
		assert.False(t, *tool.Annotations.OpenWorldHint)
	})
}

// Test Tool with both InputSchema and OutputSchema

func TestToolWithBothSchemas(t *testing.T) {
	type Input struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}

	type Output struct {
		Results []string `json:"results"`
		Count   int      `json:"count"`
	}

	tool := NewTool("search",
		WithDescription("Search with typed input and output"),
		WithInputSchema[Input](),
		WithOutputSchema[Output]())

	// Verify tool can be marshaled
	data, err := json.Marshal(tool)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	// Verify both schemas exist
	assert.Contains(t, result, "inputSchema")
	assert.Contains(t, result, "outputSchema")

	// Verify outputSchema structure
	outputSchema, ok := result["outputSchema"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "object", outputSchema["type"])
}

// Test RawOutputSchema conflict

func TestToolWithBothOutputSchemasError(t *testing.T) {
	tool := NewTool("test",
		WithString("input", Required()))

	// Set OutputSchema via DSL
	tool.OutputSchema = ToolOutputSchema{
		Type:       "object",
		Properties: map[string]any{"result": map[string]any{"type": "string"}},
	}

	// Also set RawOutputSchema - should conflict
	tool.RawOutputSchema = json.RawMessage(`{"type":"string"}`)

	// Attempt to marshal
	_, err := json.Marshal(tool)
	assert.ErrorIs(t, err, errToolSchemaConflict)
}

// Test array property options in tool

func TestToolWithArrayConstraints(t *testing.T) {
	tool := NewTool("list-tool",
		WithDescription("Tool with constrained arrays"),
		WithArray("tags",
			Description("List of tags"),
			Required(),
			WithStringItems(MinLength(1), MaxLength(50)),
			MinItems(1),
			MaxItems(10),
			UniqueItems(true)),
		WithArray("scores",
			Description("List of scores"),
			WithNumberItems(Min(0), Max(100)),
			MinItems(1)))

	// Marshal and verify
	data, err := json.Marshal(tool)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	schema := result["inputSchema"].(map[string]any)
	properties := schema["properties"].(map[string]any)

	// Verify tags array
	tags := properties["tags"].(map[string]any)
	assert.Equal(t, "array", tags["type"])
	assert.Equal(t, float64(1), tags["minItems"])
	assert.Equal(t, float64(10), tags["maxItems"])
	assert.Equal(t, true, tags["uniqueItems"])

	// Verify items schema for tags
	tagsItems := tags["items"].(map[string]any)
	assert.Equal(t, "string", tagsItems["type"])
	assert.Equal(t, float64(1), tagsItems["minLength"])
	assert.Equal(t, float64(50), tagsItems["maxLength"])

	// Verify scores array
	scores := properties["scores"].(map[string]any)
	assert.Equal(t, "array", scores["type"])
	assert.Equal(t, float64(1), scores["minItems"])

	// Verify items schema for scores
	scoresItems := scores["items"].(map[string]any)
	assert.Equal(t, "number", scoresItems["type"])
	assert.Equal(t, float64(0), scoresItems["minimum"])
	assert.Equal(t, float64(100), scoresItems["maximum"])
}

// Test object property options

func TestToolWithObjectConstraints(t *testing.T) {
	tool := NewTool("object-tool",
		WithDescription("Tool with object constraints"),
		WithObject("metadata",
			Description("Metadata object"),
			Properties(map[string]any{
				"created": map[string]any{"type": "string"},
				"updated": map[string]any{"type": "string"},
			}),
			MinProperties(1),
			MaxProperties(5),
			AdditionalProperties(false)))

	// Marshal and verify
	data, err := json.Marshal(tool)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	schema := result["inputSchema"].(map[string]any)
	properties := schema["properties"].(map[string]any)

	metadata := properties["metadata"].(map[string]any)
	assert.Equal(t, "object", metadata["type"])
	assert.Equal(t, float64(1), metadata["minProperties"])
	assert.Equal(t, float64(5), metadata["maxProperties"])
	assert.Equal(t, false, metadata["additionalProperties"])

	// Verify nested properties
	metaProps := metadata["properties"].(map[string]any)
	assert.Contains(t, metaProps, "created")
	assert.Contains(t, metaProps, "updated")
}

// Test BindArguments with json.RawMessage

func TestCallToolRequest_BindArgumentsWithRawJSON(t *testing.T) {
	type Args struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	rawJSON := json.RawMessage(`{"name": "test", "value": 42}`)

	req := CallToolRequest{}
	req.Params.Arguments = rawJSON

	var args Args
	err := req.BindArguments(&args)
	require.NoError(t, err)

	assert.Equal(t, "test", args.Name)
	assert.Equal(t, 42, args.Value)
}
