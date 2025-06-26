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

func TestCallToolRequestBindArguments(t *testing.T) {
	// Define a struct to bind to
	type TestArgs struct {
		Name  string `json:"name"`
		Age   int    `json:"age"`
		Email string `json:"email"`
	}

	// Create a request with map arguments
	req := CallToolRequest{}
	req.Params.Name = "test-tool"
	req.Params.Arguments = map[string]any{
		"name":  "John Doe",
		"age":   30,
		"email": "john@example.com",
	}

	// Bind arguments to struct
	var args TestArgs
	err := req.BindArguments(&args)
	assert.NoError(t, err)
	assert.Equal(t, "John Doe", args.Name)
	assert.Equal(t, 30, args.Age)
	assert.Equal(t, "john@example.com", args.Email)
}

func TestCallToolRequestHelperFunctions(t *testing.T) {
	// Create a request with map arguments
	req := CallToolRequest{}
	req.Params.Name = "test-tool"
	req.Params.Arguments = map[string]any{
		"string_val":       "hello",
		"int_val":          42,
		"float_val":        3.14,
		"bool_val":         true,
		"string_slice_val": []any{"one", "two", "three"},
		"int_slice_val":    []any{1, 2, 3},
		"float_slice_val":  []any{1.1, 2.2, 3.3},
		"bool_slice_val":   []any{true, false, true},
	}

	// Test GetString
	assert.Equal(t, "hello", req.GetString("string_val", "default"))
	assert.Equal(t, "default", req.GetString("missing_val", "default"))

	// Test RequireString
	str, err := req.RequireString("string_val")
	assert.NoError(t, err)
	assert.Equal(t, "hello", str)
	_, err = req.RequireString("missing_val")
	assert.Error(t, err)

	// Test GetInt
	assert.Equal(t, 42, req.GetInt("int_val", 0))
	assert.Equal(t, 0, req.GetInt("missing_val", 0))

	// Test RequireInt
	i, err := req.RequireInt("int_val")
	assert.NoError(t, err)
	assert.Equal(t, 42, i)
	_, err = req.RequireInt("missing_val")
	assert.Error(t, err)

	// Test GetFloat
	assert.Equal(t, 3.14, req.GetFloat("float_val", 0.0))
	assert.Equal(t, 0.0, req.GetFloat("missing_val", 0.0))

	// Test RequireFloat
	f, err := req.RequireFloat("float_val")
	assert.NoError(t, err)
	assert.Equal(t, 3.14, f)
	_, err = req.RequireFloat("missing_val")
	assert.Error(t, err)

	// Test GetBool
	assert.Equal(t, true, req.GetBool("bool_val", false))
	assert.Equal(t, false, req.GetBool("missing_val", false))

	// Test RequireBool
	b, err := req.RequireBool("bool_val")
	assert.NoError(t, err)
	assert.Equal(t, true, b)
	_, err = req.RequireBool("missing_val")
	assert.Error(t, err)

	// Test GetStringSlice
	assert.Equal(t, []string{"one", "two", "three"}, req.GetStringSlice("string_slice_val", nil))
	assert.Equal(t, []string{"default"}, req.GetStringSlice("missing_val", []string{"default"}))

	// Test RequireStringSlice
	ss, err := req.RequireStringSlice("string_slice_val")
	assert.NoError(t, err)
	assert.Equal(t, []string{"one", "two", "three"}, ss)
	_, err = req.RequireStringSlice("missing_val")
	assert.Error(t, err)

	// Test GetIntSlice
	assert.Equal(t, []int{1, 2, 3}, req.GetIntSlice("int_slice_val", nil))
	assert.Equal(t, []int{42}, req.GetIntSlice("missing_val", []int{42}))

	// Test RequireIntSlice
	is, err := req.RequireIntSlice("int_slice_val")
	assert.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3}, is)
	_, err = req.RequireIntSlice("missing_val")
	assert.Error(t, err)

	// Test GetFloatSlice
	assert.Equal(t, []float64{1.1, 2.2, 3.3}, req.GetFloatSlice("float_slice_val", nil))
	assert.Equal(t, []float64{4.4}, req.GetFloatSlice("missing_val", []float64{4.4}))

	// Test RequireFloatSlice
	fs, err := req.RequireFloatSlice("float_slice_val")
	assert.NoError(t, err)
	assert.Equal(t, []float64{1.1, 2.2, 3.3}, fs)
	_, err = req.RequireFloatSlice("missing_val")
	assert.Error(t, err)

	// Test GetBoolSlice
	assert.Equal(t, []bool{true, false, true}, req.GetBoolSlice("bool_slice_val", nil))
	assert.Equal(t, []bool{false}, req.GetBoolSlice("missing_val", []bool{false}))

	// Test RequireBoolSlice
	bs, err := req.RequireBoolSlice("bool_slice_val")
	assert.NoError(t, err)
	assert.Equal(t, []bool{true, false, true}, bs)
	_, err = req.RequireBoolSlice("missing_val")
	assert.Error(t, err)
}

func TestFlexibleArgumentsWithMap(t *testing.T) {
	// Create a request with map arguments
	req := CallToolRequest{}
	req.Params.Name = "test-tool"
	req.Params.Arguments = map[string]any{
		"key1": "value1",
		"key2": 123,
	}

	// Test GetArguments
	args := req.GetArguments()
	assert.Equal(t, "value1", args["key1"])
	assert.Equal(t, 123, args["key2"])

	// Test GetRawArguments
	rawArgs := req.GetRawArguments()
	mapArgs, ok := rawArgs.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "value1", mapArgs["key1"])
	assert.Equal(t, 123, mapArgs["key2"])
}

func TestFlexibleArgumentsWithString(t *testing.T) {
	// Create a request with non-map arguments
	req := CallToolRequest{}
	req.Params.Name = "test-tool"
	req.Params.Arguments = "string-argument"

	// Test GetArguments (should return empty map)
	args := req.GetArguments()
	assert.Empty(t, args)

	// Test GetRawArguments
	rawArgs := req.GetRawArguments()
	strArg, ok := rawArgs.(string)
	assert.True(t, ok)
	assert.Equal(t, "string-argument", strArg)
}

func TestFlexibleArgumentsWithStruct(t *testing.T) {
	// Create a custom struct
	type CustomArgs struct {
		Field1 string `json:"field1"`
		Field2 int    `json:"field2"`
	}

	// Create a request with struct arguments
	req := CallToolRequest{}
	req.Params.Name = "test-tool"
	req.Params.Arguments = CustomArgs{
		Field1: "test",
		Field2: 42,
	}

	// Test GetArguments (should return empty map)
	args := req.GetArguments()
	assert.Empty(t, args)

	// Test GetRawArguments
	rawArgs := req.GetRawArguments()
	structArg, ok := rawArgs.(CustomArgs)
	assert.True(t, ok)
	assert.Equal(t, "test", structArg.Field1)
	assert.Equal(t, 42, structArg.Field2)
}

func TestFlexibleArgumentsJSONMarshalUnmarshal(t *testing.T) {
	// Create a request with map arguments
	req := CallToolRequest{}
	req.Params.Name = "test-tool"
	req.Params.Arguments = map[string]any{
		"key1": "value1",
		"key2": 123,
	}

	// Marshal to JSON
	data, err := json.Marshal(req)
	assert.NoError(t, err)

	// Unmarshal from JSON
	var unmarshaledReq CallToolRequest
	err = json.Unmarshal(data, &unmarshaledReq)
	assert.NoError(t, err)

	// Check if arguments are correctly unmarshaled
	args := unmarshaledReq.GetArguments()
	assert.Equal(t, "value1", args["key1"])
	assert.Equal(t, float64(123), args["key2"]) // JSON numbers are unmarshaled as float64
}

// TestNewItemsAPICompatibility tests that the new Items API functions
// generate the same schema as the original Items() function with manual schema objects
func TestNewItemsAPICompatibility(t *testing.T) {
	tests := []struct {
		name    string
		oldTool Tool
		newTool Tool
	}{
		{
			name: "WithStringItems basic",
			oldTool: NewTool("old-string-array",
				WithDescription("Tool with string array using old API"),
				WithArray("items",
					Description("List of string items"),
					Items(map[string]any{
						"type": "string",
					}),
				),
			),
			newTool: NewTool("new-string-array",
				WithDescription("Tool with string array using new API"),
				WithArray("items",
					Description("List of string items"),
					WithStringItems(),
				),
			),
		},
		{
			name: "WithStringEnumItems",
			oldTool: NewTool("old-enum-array",
				WithDescription("Tool with enum array using old API"),
				WithArray("status",
					Description("Filter by status"),
					Items(map[string]any{
						"type": "string",
						"enum": []string{"active", "inactive", "pending"},
					}),
				),
			),
			newTool: NewTool("new-enum-array",
				WithDescription("Tool with enum array using new API"),
				WithArray("status",
					Description("Filter by status"),
					WithStringEnumItems([]string{"active", "inactive", "pending"}),
				),
			),
		},
		{
			name: "WithStringItems with options",
			oldTool: NewTool("old-string-with-opts",
				WithDescription("Tool with string array with options using old API"),
				WithArray("names",
					Description("List of names"),
					Items(map[string]any{
						"type":      "string",
						"minLength": 1,
						"maxLength": 50,
					}),
				),
			),
			newTool: NewTool("new-string-with-opts",
				WithDescription("Tool with string array with options using new API"),
				WithArray("names",
					Description("List of names"),
					WithStringItems(MinLength(1), MaxLength(50)),
				),
			),
		},
		{
			name: "WithNumberItems basic",
			oldTool: NewTool("old-number-array",
				WithDescription("Tool with number array using old API"),
				WithArray("scores",
					Description("List of scores"),
					Items(map[string]any{
						"type": "number",
					}),
				),
			),
			newTool: NewTool("new-number-array",
				WithDescription("Tool with number array using new API"),
				WithArray("scores",
					Description("List of scores"),
					WithNumberItems(),
				),
			),
		},
		{
			name: "WithNumberItems with constraints",
			oldTool: NewTool("old-number-with-constraints",
				WithDescription("Tool with constrained number array using old API"),
				WithArray("ratings",
					Description("List of ratings"),
					Items(map[string]any{
						"type":    "number",
						"minimum": 0.0,
						"maximum": 10.0,
					}),
				),
			),
			newTool: NewTool("new-number-with-constraints",
				WithDescription("Tool with constrained number array using new API"),
				WithArray("ratings",
					Description("List of ratings"),
					WithNumberItems(Min(0), Max(10)),
				),
			),
		},
		{
			name: "WithBooleanItems basic",
			oldTool: NewTool("old-boolean-array",
				WithDescription("Tool with boolean array using old API"),
				WithArray("flags",
					Description("List of feature flags"),
					Items(map[string]any{
						"type": "boolean",
					}),
				),
			),
			newTool: NewTool("new-boolean-array",
				WithDescription("Tool with boolean array using new API"),
				WithArray("flags",
					Description("List of feature flags"),
					WithBooleanItems(),
				),
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal both tools to JSON
			oldData, err := json.Marshal(tt.oldTool)
			assert.NoError(t, err)

			newData, err := json.Marshal(tt.newTool)
			assert.NoError(t, err)

			// Unmarshal to maps for comparison
			var oldResult, newResult map[string]any
			err = json.Unmarshal(oldData, &oldResult)
			assert.NoError(t, err)

			err = json.Unmarshal(newData, &newResult)
			assert.NoError(t, err)

			// Compare the inputSchema properties (ignoring tool names and descriptions)
			oldSchema := oldResult["inputSchema"].(map[string]any)
			newSchema := newResult["inputSchema"].(map[string]any)

			oldProperties := oldSchema["properties"].(map[string]any)
			newProperties := newSchema["properties"].(map[string]any)

			// Get the array property (should be the only one in these tests)
			var oldArrayProp, newArrayProp map[string]any
			for _, prop := range oldProperties {
				if propMap, ok := prop.(map[string]any); ok && propMap["type"] == "array" {
					oldArrayProp = propMap
					break
				}
			}
			for _, prop := range newProperties {
				if propMap, ok := prop.(map[string]any); ok && propMap["type"] == "array" {
					newArrayProp = propMap
					break
				}
			}

			assert.NotNil(t, oldArrayProp, "Old tool should have array property")
			assert.NotNil(t, newArrayProp, "New tool should have array property")

			// Compare the items schema - this is the critical part
			oldItems := oldArrayProp["items"]
			newItems := newArrayProp["items"]

			assert.Equal(t, oldItems, newItems, "Items schema should be identical between old and new API")

			// Also compare other array properties like description
			assert.Equal(t, oldArrayProp["description"], newArrayProp["description"], "Array descriptions should match")
			assert.Equal(t, oldArrayProp["type"], newArrayProp["type"], "Array types should match")
		})
	}
}

// Test HasOutputSchema method with empty schema
func TestHasOutputSchemaEmpty(t *testing.T) {
	tool := NewTool("test-tool")

	// Empty schema should return false
	assert.False(t, tool.HasOutputSchema())
}

// Test HasOutputSchema method with defined schema
func TestHasOutputSchemaWithSchema(t *testing.T) {
	schema := json.RawMessage(`{"type": "object", "properties": {"result": {"type": "string"}}}`)
	tool := NewTool("test-tool", WithOutputSchema(schema))

	// Should return true when schema is defined
	assert.True(t, tool.HasOutputSchema())
}

// Test Tool JSON marshaling includes output schema when defined
func TestToolMarshalWithOutputSchema(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"temperature": {"type": "string", "description": "Temperature value"},
			"condition": {"type": "string"}
		},
		"required": ["temperature"]
	}`)

	tool := NewTool("weather-tool",
		WithDescription("Get weather information"),
		WithString("location", Required()),
		WithOutputSchema(schema),
	)

	// Marshal to JSON
	data, err := json.Marshal(tool)
	assert.NoError(t, err)

	// Unmarshal to verify structure
	var result map[string]any
	err = json.Unmarshal(data, &result)
	assert.NoError(t, err)

	// Check that outputSchema is present
	outputSchema, exists := result["outputSchema"]
	assert.True(t, exists)

	schema2, ok := outputSchema.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "object", schema2["type"])

	// Check properties
	properties, ok := schema2["properties"].(map[string]any)
	assert.True(t, ok)
	assert.Contains(t, properties, "temperature")
	assert.Contains(t, properties, "condition")

	// Check required fields
	required, ok := schema2["required"].([]any)
	assert.True(t, ok)
	assert.Contains(t, required, "temperature")
}

// Test Tool JSON marshaling omits output schema when empty
func TestToolMarshalWithoutOutputSchema(t *testing.T) {
	tool := NewTool("simple-tool",
		WithDescription("Simple tool without output schema"),
		WithString("input", Required()),
	)

	// Marshal to JSON
	data, err := json.Marshal(tool)
	assert.NoError(t, err)

	// Unmarshal to verify structure
	var result map[string]any
	err = json.Unmarshal(data, &result)
	assert.NoError(t, err)

	// Check that outputSchema is not present
	_, exists := result["outputSchema"]
	assert.False(t, exists)
}

// Test WithOutputSchema function
func TestWithOutputSchema(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"data": {"type": "string"}
		},
		"required": ["data"]
	}`)

	tool := NewTool("custom-tool", WithOutputSchema(schema))

	assert.True(t, tool.HasOutputSchema())
	cleaned, _ := ExtractMCPSchema(schema)
	assert.Equal(t, cleaned, tool.OutputSchema)
}

// Test NewStructuredToolResult function
func TestNewStructuredToolResult(t *testing.T) {
	// This test is removed as NewStructuredToolResult is deprecated
	// Use NewToolResultStructured[T]() instead
}

// Test NewStructuredToolError function
func TestNewStructuredToolError(t *testing.T) {
	// This test is removed as NewStructuredToolError is deprecated
	// Use NewToolResultErrorStructured[T]() instead
}

// Test WithOutputType function with struct-based schema generation
func TestWithOutputType(t *testing.T) {
	type WeatherOutput struct {
		Temperature float64 `json:"temperature" jsonschema:"description=Temperature in Celsius"`
		Condition   string  `json:"condition" jsonschema:"required"`
		Humidity    int     `json:"humidity,omitempty" jsonschema:"minimum=0,maximum=100"`
	}

	tool := NewTool("weather-tool",
		WithDescription("Get weather information"),
		WithString("location", Required()),
		WithOutputType[WeatherOutput](),
	)

	assert.True(t, tool.HasOutputSchema())
	assert.NotNil(t, tool.OutputSchema)

	// Marshal and verify JSON structure
	data, err := json.Marshal(tool)
	assert.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	assert.NoError(t, err)

	// Check that outputSchema is present and valid
	outputSchema, exists := result["outputSchema"]
	assert.True(t, exists)

	schema, ok := outputSchema.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "object", schema["type"])

	// Check properties exist
	properties, ok := schema["properties"].(map[string]any)
	assert.True(t, ok)
	assert.Contains(t, properties, "temperature")
	assert.Contains(t, properties, "condition")
	assert.Contains(t, properties, "humidity")
}

// Test new helper functions
func TestNewToolResultStructured(t *testing.T) {
	type ResponseData struct {
		Message string `json:"message"`
		Status  int    `json:"status"`
	}

	data := ResponseData{
		Message: "Operation successful",
		Status:  200,
	}

	result := NewToolResultStructured(data)

	assert.False(t, result.IsError)
	assert.Equal(t, data, result.StructuredContent)
	assert.Len(t, result.Content, 1)

	textContent, ok := result.Content[0].(TextContent)
	assert.True(t, ok)
	assert.Equal(t, "text", textContent.Type)

	// Verify the JSON content matches the structured content
	var parsedJSON ResponseData
	err := json.Unmarshal([]byte(textContent.Text), &parsedJSON)
	assert.NoError(t, err)
	assert.Equal(t, data.Message, parsedJSON.Message)
	assert.Equal(t, data.Status, parsedJSON.Status)
}

func TestNewToolResultWithStructured(t *testing.T) {
	type ResponseData struct {
		Value int `json:"value"`
	}

	data := ResponseData{Value: 42}
	textContent := NewTextContent("Custom text content")
	imageContent := NewImageContent("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg==", "image/png")

	result := NewToolResultWithStructured([]Content{textContent, imageContent}, data)

	assert.False(t, result.IsError)
	assert.Equal(t, data, result.StructuredContent)
	assert.Len(t, result.Content, 2)

	// Verify content is preserved as-is
	assert.Equal(t, textContent, result.Content[0])
	assert.Equal(t, imageContent, result.Content[1])
}

func TestNewToolResultErrorStructured(t *testing.T) {
	type ErrorData struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}

	errorData := ErrorData{
		Code:    "TIMEOUT",
		Message: "Request timed out",
	}

	result := NewToolResultErrorStructured(errorData)

	assert.True(t, result.IsError)
	assert.Equal(t, errorData, result.StructuredContent)
	assert.Len(t, result.Content, 1)

	textContent, ok := result.Content[0].(TextContent)
	assert.True(t, ok)
	assert.Equal(t, "text", textContent.Type)

	// Verify the JSON content matches the structured content
	var parsedJSON ErrorData
	err := json.Unmarshal([]byte(textContent.Text), &parsedJSON)
	assert.NoError(t, err)
	assert.Equal(t, errorData.Code, parsedJSON.Code)
	assert.Equal(t, errorData.Message, parsedJSON.Message)
}

func TestNewToolResultErrorWithStructured(t *testing.T) {
	type ErrorData struct {
		Code string `json:"code"`
	}

	errorData := ErrorData{Code: "ERR_404"}
	textContent := NewTextContent("Not found error occurred")

	result := NewToolResultErrorWithStructured([]Content{textContent}, errorData)

	assert.True(t, result.IsError)
	assert.Equal(t, errorData, result.StructuredContent)
	assert.Len(t, result.Content, 1)
	assert.Equal(t, textContent, result.Content[0])
}

// Test validation with new API
func TestValidateOutputWithSchema(t *testing.T) {
	type ValidOutput struct {
		Message string `json:"message" jsonschema:"required"`
		Count   int    `json:"count" jsonschema:"minimum=0"`
	}

	tool := NewTool("test-tool",
		WithString("input", Required()),
		WithOutputType[ValidOutput](),
	)

	// Test valid structured content
	validData := ValidOutput{Message: "Success", Count: 5}
	validResult := NewToolResultStructured(validData)

	err := tool.ValidateOutput(validResult)
	assert.NoError(t, err, "Valid structured content should pass validation")

	// Test invalid structured content (missing required field)
	invalidData := map[string]any{"count": 10} // missing required "message"
	invalidResult := &CallToolResult{
		IsError:           false,
		StructuredContent: invalidData,
		Content:           []Content{NewTextContent("test")},
	}

	err = tool.ValidateOutput(invalidResult)
	assert.Error(t, err, "Invalid structured content should fail validation")
}

// Test ValidateOutput behavior with different scenarios
func TestValidateOutput(t *testing.T) {
	// Test case 1: Tool without output schema should not validate
	toolNoSchema := NewTool("no-schema-tool", WithString("input", Required()))
	result := NewToolResultText("Just text content")

	err := toolNoSchema.ValidateOutput(result)
	assert.NoError(t, err, "Tool without output schema should not validate")

	// Test case 2: Error result should skip validation
	schema := json.RawMessage(`{"type": "object", "properties": {"message": {"type": "string"}}, "required": ["message"]}`)
	toolWithSchema := NewTool("schema-tool",
		WithString("input", Required()),
		WithOutputSchema(schema),
	)
	errorResult := &CallToolResult{IsError: true, StructuredContent: map[string]any{"invalid": "data"}}

	err = toolWithSchema.ValidateOutput(errorResult)
	assert.NoError(t, err, "Error result should skip validation")

	// Test case 3: Tool with output schema but no structured content should return error
	resultNoStructured := NewToolResultText("Just text content")

	err = toolWithSchema.ValidateOutput(resultNoStructured)
	assert.Error(t, err, "Tool with output schema but no structured content should return error")
	assert.Contains(t, err.Error(), "requires structured output")
}

func TestEnsureOutputSchemaValidatorThreadSafety(t *testing.T) {
	// Create a tool with output schema but no pre-compiled validator
	outputSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"message": {"type": "string"},
			"status": {"type": "integer"}
		},
		"required": ["message"]
	}`)

	tool := NewTool("thread-safe-tool",
		WithDescription("A tool to test thread safety"),
		WithOutputSchema(outputSchema),
	)

	// Run multiple goroutines concurrently to call ensureOutputSchemaValidator
	const numGoroutines = 100
	errors := make(chan error, numGoroutines)

	for range numGoroutines {
		go func() {
			err := tool.ensureOutputSchemaValidator()
			errors <- err
		}()
	}

	// Collect all errors
	for i := 0; i < numGoroutines; i++ {
		err := <-errors
		assert.NoError(t, err, "ensureOutputSchemaValidator should not return error")
	}

	// Verify the validator was properly initialized
	assert.NotNil(t, tool.validatorState, "validatorState should be initialized")
	assert.NotNil(t, tool.validatorState.validator, "validator should be initialized")
	assert.NoError(t, tool.validatorState.initErr, "initErr should be nil for successful compilation")

	// Test validation with the initialized validator
	result := &CallToolResult{
		Content: []Content{NewTextContent("Success")},
		StructuredContent: map[string]any{
			"message": "test message",
			"status":  200,
		},
		IsError: false,
	}

	err := tool.ValidateOutput(result)
	assert.NoError(t, err, "ValidateOutput should succeed with valid data")
}

func TestEnsureOutputSchemaValidatorWithOutputType(t *testing.T) {
	type TestOutput struct {
		Message string `json:"message" jsonschema:"required"`
		Status  int    `json:"status" jsonschema:"minimum=100,maximum=599"`
	}

	// Create a tool with WithOutputType (which pre-compiles the validator)
	tool := NewTool("pre-compiled-tool",
		WithDescription("A tool with pre-compiled validator"),
		WithOutputType[TestOutput](),
	)

	// Verify the validator is already set
	assert.NotNil(t, tool.validatorState, "validatorState should be initialized")
	assert.NotNil(t, tool.validatorState.validator, "validator should be pre-compiled")
	assert.NoError(t, tool.validatorState.initErr, "initErr should be nil for pre-compiled validator")

	// Call ensureOutputSchemaValidator multiple times concurrently
	const numGoroutines = 50
	errors := make(chan error, numGoroutines)

	for range numGoroutines {
		go func() {
			err := tool.ensureOutputSchemaValidator()
			errors <- err
		}()
	}

	// Collect all errors
	for range numGoroutines {
		err := <-errors
		assert.NoError(t, err, "ensureOutputSchemaValidator should not return error")
	}

	// Test validation still works correctly
	result := &CallToolResult{
		Content: []Content{NewTextContent("Success")},
		StructuredContent: TestOutput{
			Message: "test message",
			Status:  200,
		},
		IsError: false,
	}

	err := tool.ValidateOutput(result)
	assert.NoError(t, err, "ValidateOutput should succeed with valid data")
}

func TestEnsureOutputSchemaValidatorErrorConsistency(t *testing.T) {
	// Create a tool with invalid output schema to test error handling consistency
	invalidSchema := json.RawMessage(`{
		"type": "invalid-type-that-should-cause-error",
		"$ref": "#/invalid/reference"
	}`)

	tool := NewTool("error-test-tool",
		WithDescription("A tool to test error consistency"),
		WithOutputSchema(invalidSchema),
	)

	// Run multiple goroutines concurrently
	const numGoroutines = 50
	errors := make(chan error, numGoroutines)

	for range numGoroutines {
		go func() {
			err := tool.ensureOutputSchemaValidator()
			errors <- err
		}()
	}

	// Collect all errors - they should all be consistent
	var firstErr error
	var errorCount, successCount int
	for i := range numGoroutines {
		err := <-errors
		if i == 0 {
			firstErr = err
		}

		if err != nil {
			errorCount++
		} else {
			successCount++
		}

		// All results should be consistent - either all errors or all success
		if (firstErr == nil) != (err == nil) {
			t.Errorf("Inconsistent error handling in goroutine %d: first error was %v, but got %v", i, firstErr, err)
		}
	}

	// Should either be all errors or all success
	if errorCount > 0 && successCount > 0 {
		t.Errorf("Mixed results: %d errors and %d successes - should be consistent", errorCount, successCount)
	}

	// Verify that subsequent calls return the same result
	for i := range 5 {
		finalErr := tool.ensureOutputSchemaValidator()
		if (firstErr == nil) != (finalErr == nil) {
			t.Errorf("Error state changed after concurrent access: original was %v, subsequent call %d returned %v", firstErr, i, finalErr)
		}
	}
}
