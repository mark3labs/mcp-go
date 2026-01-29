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

func TestToolWithAny(t *testing.T) {
	const desc = "Can be any value: string, number, bool, object, or slice"

	tool := NewTool("any-tool",
		WithDescription("A tool with an 'any' type property"),
		WithAny("data",
			Description(desc),
			Required(),
		),
	)

	data, err := json.Marshal(tool)
	assert.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	assert.NoError(t, err)

	assert.Equal(t, "any-tool", result["name"])

	schema, ok := result["inputSchema"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "object", schema["type"])

	properties, ok := schema["properties"].(map[string]any)
	assert.True(t, ok)

	dataProp, ok := properties["data"].(map[string]any)
	assert.True(t, ok)
	_, typeExists := dataProp["type"]
	assert.False(t, typeExists, "The 'any' type property should not have a 'type' field")
	assert.Equal(t, desc, dataProp["description"])

	required, ok := schema["required"].([]any)
	assert.True(t, ok)
	assert.Contains(t, required, "data")

	type testStruct struct {
		A string `json:"A"`
	}
	testCases := []struct {
		description string
		arg         any
		expect      any
	}{{
		description: "string",
		arg:         "hello world",
		expect:      "hello world",
	}, {
		description: "integer",
		arg:         123,
		expect:      float64(123), // JSON unmarshals numbers to float64
	}, {
		description: "float",
		arg:         3.14,
		expect:      3.14,
	}, {
		description: "boolean",
		arg:         true,
		expect:      true,
	}, {
		description: "object",
		arg:         map[string]any{"key": "value"},
		expect:      map[string]any{"key": "value"},
	}, {
		description: "slice",
		arg:         []any{1, "two", false},
		expect:      []any{float64(1), "two", false},
	}, {
		description: "struct",
		arg:         testStruct{A: "B"},
		expect:      map[string]any{"A": "B"},
	}}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("with_%s", tc.description), func(t *testing.T) {
			req := CallToolRequest{
				Request: Request{},
				Params: CallToolParams{
					Name: "any-tool",
					Arguments: map[string]any{
						"data": tc.arg,
					},
				},
			}

			// Marshal and unmarshal to simulate a real request
			reqBytes, err := json.Marshal(req)
			assert.NoError(t, err)

			var unmarshaledReq CallToolRequest
			err = json.Unmarshal(reqBytes, &unmarshaledReq)
			assert.NoError(t, err)

			args := unmarshaledReq.GetArguments()
			value, ok := args["data"]
			assert.True(t, ok)
			assert.Equal(t, tc.expect, value)
		})
	}
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

// TestToolWithInputSchema tests that the WithInputSchema function
// generates an MCP-compatible JSON output schema for a tool
func TestToolWithInputSchema(t *testing.T) {
	type TestInput struct {
		Name  string `json:"name" jsonschema_description:"Person's name" jsonschema:"required"`
		Age   int    `json:"age" jsonschema_description:"Person's age"`
		Email string `json:"email,omitempty" jsonschema_description:"Email address" jsonschema:"required"`
	}

	tool := NewTool("test_tool",
		WithDescription("Test tool with output schema"),
		WithInputSchema[TestInput](),
	)

	// Check that RawOutputSchema was set
	assert.NotNil(t, tool.RawInputSchema)

	// Marshal and verify structure
	data, err := json.Marshal(tool)
	assert.NoError(t, err)

	var toolData map[string]any
	err = json.Unmarshal(data, &toolData)
	assert.NoError(t, err)

	// Verify inputSchema exists
	inputSchema, exists := toolData["inputSchema"]
	assert.True(t, exists)
	assert.NotNil(t, inputSchema)

	// Verify required list exists
	schemaMap, ok := inputSchema.(map[string]any)
	assert.True(t, ok)
	requiredList, exists := schemaMap["required"]
	assert.True(t, exists)
	assert.NotNil(t, requiredList)

	// Verify properties exist
	properties, exists := schemaMap["properties"]
	assert.True(t, exists)
	propertiesMap, ok := properties.(map[string]any)
	assert.True(t, ok)

	// Verify specific properties
	assert.Contains(t, propertiesMap, "name")
	assert.Contains(t, propertiesMap, "age")
	assert.Contains(t, propertiesMap, "email")
}

// TestToolWithOutputSchema tests that the WithOutputSchema function
// generates an MCP-compatible JSON output schema for a tool
func TestToolWithOutputSchema(t *testing.T) {
	type TestOutput struct {
		Name  string `json:"name" jsonschema_description:"Person's name"`
		Age   int    `json:"age" jsonschema_description:"Person's age"`
		Email string `json:"email,omitempty" jsonschema_description:"Email address"`
	}

	tests := []struct {
		name                 string
		tool                 Tool
		expectedOutputSchema bool
	}{
		{
			name: "default behavior",
			tool: NewTool("test_tool",
				WithDescription("Test tool with output schema"),
				WithOutputSchema[TestOutput](),
				WithString("input", Required()),
			),
			expectedOutputSchema: true,
		},
		{
			name: "no output schema is set",
			tool: NewTool("test_tool",
				WithDescription("Test tool with no output schema"),
				WithString("input", Required()),
			),
			expectedOutputSchema: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal and verify structure
			data, err := json.Marshal(tt.tool)
			assert.NoError(t, err)

			var toolData map[string]any
			err = json.Unmarshal(data, &toolData)
			assert.NoError(t, err)

			// Verify outputSchema exists
			outputSchema, exists := toolData["outputSchema"]
			if tt.expectedOutputSchema {
				assert.True(t, exists)
				assert.NotNil(t, outputSchema)
			} else {
				assert.False(t, exists)
				assert.Nil(t, outputSchema)
			}
		})
	}
}

// TestNewToolResultStructured tests that the NewToolResultStructured function
// creates a CallToolResult with both structured and text content
func TestNewToolResultStructured(t *testing.T) {
	testData := map[string]any{
		"message": "Success",
		"count":   42,
		"active":  true,
	}

	result := NewToolResultStructured(testData, "Fallback text")

	assert.Len(t, result.Content, 1)

	textContent, ok := result.Content[0].(TextContent)
	assert.True(t, ok)
	assert.Equal(t, "Fallback text", textContent.Text)
	assert.NotNil(t, result.StructuredContent)
}

// TestCallToolResultMarshalJSON tests the custom JSON marshaling of CallToolResult
func TestCallToolResultMarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		result   CallToolResult
		expected map[string]any
	}{
		{
			name: "basic result with text content",
			result: CallToolResult{
				Result: Result{
					Meta: NewMetaFromMap(map[string]any{"key": "value"}),
				},
				Content: []Content{
					TextContent{Type: "text", Text: "Hello, world!"},
				},
				IsError: false,
			},
			expected: map[string]any{
				"_meta": map[string]any{"key": "value"},
				"content": []any{
					map[string]any{
						"type": "text",
						"text": "Hello, world!",
					},
				},
			},
		},
		{
			name: "result with structured content",
			result: CallToolResult{
				Result: Result{
					Meta: NewMetaFromMap(map[string]any{"key": "value"}),
				},
				Content: []Content{
					TextContent{Type: "text", Text: "Operation completed"},
				},
				StructuredContent: map[string]any{
					"status":  "success",
					"count":   42,
					"message": "Data processed successfully",
				},
				IsError: false,
			},
			expected: map[string]any{
				"_meta": map[string]any{"key": "value"},
				"content": []any{
					map[string]any{
						"type": "text",
						"text": "Operation completed",
					},
				},
				"structuredContent": map[string]any{
					"status":  "success",
					"count":   float64(42), // JSON numbers are unmarshaled as float64
					"message": "Data processed successfully",
				},
			},
		},
		{
			name: "error result",
			result: CallToolResult{
				Result: Result{
					Meta: NewMetaFromMap(map[string]any{"error_code": "E001"}),
				},
				Content: []Content{
					TextContent{Type: "text", Text: "An error occurred"},
				},
				IsError: true,
			},
			expected: map[string]any{
				"_meta": map[string]any{"error_code": "E001"},
				"content": []any{
					map[string]any{
						"type": "text",
						"text": "An error occurred",
					},
				},
				"isError": true,
			},
		},
		{
			name: "result with multiple content types",
			result: CallToolResult{
				Result: Result{
					Meta: NewMetaFromMap(map[string]any{"session_id": "12345"}),
				},
				Content: []Content{
					TextContent{Type: "text", Text: "Processing complete"},
					ImageContent{Type: "image", Data: "base64-encoded-image-data", MIMEType: "image/jpeg"},
				},
				StructuredContent: map[string]any{
					"processed_items": 100,
					"errors":          0,
				},
				IsError: false,
			},
			expected: map[string]any{
				"_meta": map[string]any{"session_id": "12345"},
				"content": []any{
					map[string]any{
						"type": "text",
						"text": "Processing complete",
					},
					map[string]any{
						"type":     "image",
						"data":     "base64-encoded-image-data",
						"mimeType": "image/jpeg",
					},
				},
				"structuredContent": map[string]any{
					"processed_items": float64(100),
					"errors":          float64(0),
				},
			},
		},
		{
			name: "result with nil structured content",
			result: CallToolResult{
				Result: Result{
					Meta: NewMetaFromMap(map[string]any{"key": "value"}),
				},
				Content: []Content{
					TextContent{Type: "text", Text: "Simple result"},
				},
				StructuredContent: nil,
				IsError:           false,
			},
			expected: map[string]any{
				"_meta": map[string]any{"key": "value"},
				"content": []any{
					map[string]any{
						"type": "text",
						"text": "Simple result",
					},
				},
			},
		},
		{
			name: "result with empty content array",
			result: CallToolResult{
				Result: Result{
					Meta: NewMetaFromMap(map[string]any{"key": "value"}),
				},
				Content: []Content{},
				StructuredContent: map[string]any{
					"data": "structured only",
				},
				IsError: false,
			},
			expected: map[string]any{
				"_meta":   map[string]any{"key": "value"},
				"content": []any{},
				"structuredContent": map[string]any{
					"data": "structured only",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal the result
			data, err := json.Marshal(tt.result)
			assert.NoError(t, err)

			// Unmarshal to map for comparison
			var result map[string]any
			err = json.Unmarshal(data, &result)
			assert.NoError(t, err)

			// Compare expected fields
			for key, expectedValue := range tt.expected {
				assert.Contains(t, result, key, "Result should contain key: %s", key)
				assert.Equal(t, expectedValue, result[key], "Value for key %s should match", key)
			}

			// Verify that unexpected fields are not present
			for key := range result {
				if key != "_meta" && key != "content" && key != "structuredContent" && key != "isError" {
					t.Errorf("Unexpected field in result: %s", key)
				}
			}
		})
	}
}

// TestCallToolResultUnmarshalJSON tests the custom JSON unmarshaling of CallToolResult
func TestCallToolResultUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		jsonData string
		expected CallToolResult
		wantErr  bool
	}{
		{
			name: "basic result with text content",
			jsonData: `{
				"_meta": {"key": "value"},
				"content": [
					{"type": "text", "text": "Hello, world!"}
				]
			}`,
			expected: CallToolResult{
				Result: Result{
					Meta: NewMetaFromMap(map[string]any{"key": "value"}),
				},
				Content: []Content{
					TextContent{Type: "text", Text: "Hello, world!"},
				},
				IsError: false,
			},
			wantErr: false,
		},
		{
			name: "result with structured content",
			jsonData: `{
				"_meta": {"key": "value"},
				"content": [
					{"type": "text", "text": "Operation completed"}
				],
				"structuredContent": {
					"status": "success",
					"count": 42,
					"message": "Data processed successfully"
				}
			}`,
			expected: CallToolResult{
				Result: Result{
					Meta: NewMetaFromMap(map[string]any{"key": "value"}),
				},
				Content: []Content{
					TextContent{Type: "text", Text: "Operation completed"},
				},
				StructuredContent: map[string]any{
					"status":  "success",
					"count":   float64(42),
					"message": "Data processed successfully",
				},
				IsError: false,
			},
			wantErr: false,
		},
		{
			name: "error result",
			jsonData: `{
				"_meta": {"error_code": "E001"},
				"content": [
					{"type": "text", "text": "An error occurred"}
				],
				"isError": true
			}`,
			expected: CallToolResult{
				Result: Result{
					Meta: NewMetaFromMap(map[string]any{"error_code": "E001"}),
				},
				Content: []Content{
					TextContent{Type: "text", Text: "An error occurred"},
				},
				IsError: true,
			},
			wantErr: false,
		},
		{
			name: "result with multiple content types",
			jsonData: `{
				"_meta": {"session_id": "12345"},
				"content": [
					{"type": "text", "text": "Processing complete"},
					{"type": "image", "data": "base64-encoded-image-data", "mimeType": "image/jpeg"}
				],
				"structuredContent": {
					"processed_items": 100,
					"errors": 0
				}
			}`,
			expected: CallToolResult{
				Result: Result{
					Meta: NewMetaFromMap(map[string]any{"session_id": "12345"}),
				},
				Content: []Content{
					TextContent{Type: "text", Text: "Processing complete"},
					ImageContent{Type: "image", Data: "base64-encoded-image-data", MIMEType: "image/jpeg"},
				},
				StructuredContent: map[string]any{
					"processed_items": float64(100),
					"errors":          float64(0),
				},
				IsError: false,
			},
			wantErr: false,
		},
		{
			name: "result with nil structured content",
			jsonData: `{
				"_meta": {"key": "value"},
				"content": [
					{"type": "text", "text": "Simple result"}
				]
			}`,
			expected: CallToolResult{
				Result: Result{
					Meta: NewMetaFromMap(map[string]any{"key": "value"}),
				},
				Content: []Content{
					TextContent{Type: "text", Text: "Simple result"},
				},
				StructuredContent: nil,
				IsError:           false,
			},
			wantErr: false,
		},
		{
			name: "result with empty content array",
			jsonData: `{
				"_meta": {"key": "value"},
				"content": [],
				"structuredContent": {
					"data": "structured only"
				}
			}`,
			expected: CallToolResult{
				Result: Result{
					Meta: NewMetaFromMap(map[string]any{"key": "value"}),
				},
				Content: []Content{},
				StructuredContent: map[string]any{
					"data": "structured only",
				},
				IsError: false,
			},
			wantErr: false,
		},
		{
			name:     "invalid JSON",
			jsonData: `{invalid json}`,
			wantErr:  true,
		},
		{
			name: "result with missing content field",
			jsonData: `{
				"_meta": {"key": "value"},
				"structuredContent": {"data": "no content"}
			}`,
			expected: CallToolResult{
				Result: Result{
					Meta: NewMetaFromMap(map[string]any{"key": "value"}),
				},
				Content: nil,
				StructuredContent: map[string]any{
					"data": "no content",
				},
				IsError: false,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result CallToolResult
			err := json.Unmarshal([]byte(tt.jsonData), &result)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)

			// Compare Meta
			if tt.expected.Meta != nil {
				assert.Equal(t, tt.expected.Meta, result.Meta)
			}

			// Compare Content
			assert.Len(t, result.Content, len(tt.expected.Content))
			for i, expectedContent := range tt.expected.Content {
				if i < len(result.Content) {
					// Compare content types and values
					switch expected := expectedContent.(type) {
					case TextContent:
						if actual, ok := result.Content[i].(TextContent); ok {
							assert.Equal(t, expected.Text, actual.Text)
						} else {
							t.Errorf("Expected TextContent at index %d, got %T", i, result.Content[i])
						}
					case ImageContent:
						if actual, ok := result.Content[i].(ImageContent); ok {
							assert.Equal(t, expected.Data, actual.Data)
							assert.Equal(t, expected.MIMEType, actual.MIMEType)
						} else {
							t.Errorf("Expected ImageContent at index %d, got %T", i, result.Content[i])
						}
					}
				}
			}

			// Compare StructuredContent
			assert.Equal(t, tt.expected.StructuredContent, result.StructuredContent)

			// Compare IsError
			assert.Equal(t, tt.expected.IsError, result.IsError)
		})
	}
}

// TestCallToolResultRoundTrip tests that marshaling and unmarshaling preserves all data
func TestCallToolResultRoundTrip(t *testing.T) {
	original := CallToolResult{
		Result: Result{
			Meta: NewMetaFromMap(map[string]any{
				"session_id": "12345",
				"user_id":    "user123",
				"timestamp":  "2024-01-01T00:00:00Z",
			}),
		},
		Content: []Content{
			TextContent{Type: "text", Text: "Operation started"},
			ImageContent{Type: "image", Data: "base64-encoded-chart-data", MIMEType: "image/png"},
			TextContent{Type: "text", Text: "Operation completed successfully"},
		},
		StructuredContent: map[string]any{
			"status":          "success",
			"processed_count": float64(150.0),
			"error_count":     float64(0.0),
			"warnings":        []any{"Minor issue detected"},
			"metadata": map[string]any{
				"version": "1.0.0",
				"build":   "2024-01-01",
			},
		},
		IsError: false,
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	assert.NoError(t, err)

	// Unmarshal back
	var unmarshaled CallToolResult
	err = json.Unmarshal(data, &unmarshaled)
	assert.NoError(t, err)

	// Verify all fields are preserved
	assert.Equal(t, original.Meta, unmarshaled.Meta)
	assert.Equal(t, original.IsError, unmarshaled.IsError)
	assert.Equal(t, original.StructuredContent, unmarshaled.StructuredContent)

	// Verify content array
	assert.Len(t, unmarshaled.Content, len(original.Content))
	for i, expectedContent := range original.Content {
		if i < len(unmarshaled.Content) {
			switch expected := expectedContent.(type) {
			case TextContent:
				if actual, ok := unmarshaled.Content[i].(TextContent); ok {
					assert.Equal(t, expected.Text, actual.Text)
				} else {
					t.Errorf("Expected TextContent at index %d, got %T", i, unmarshaled.Content[i])
				}
			case ImageContent:
				if actual, ok := unmarshaled.Content[i].(ImageContent); ok {
					assert.Equal(t, expected.Data, actual.Data)
					assert.Equal(t, expected.MIMEType, actual.MIMEType)
				} else {
					t.Errorf("Expected ImageContent at index %d, got %T", i, unmarshaled.Content[i])
				}
			}
		}
	}
}

// TestCallToolResultEdgeCases tests edge cases for CallToolResult marshaling/unmarshaling
func TestCallToolResultEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		result   CallToolResult
		jsonData string
	}{
		{
			name: "result with complex structured content",
			result: CallToolResult{
				Content: []Content{
					TextContent{Type: "text", Text: "Complex data returned"},
				},
				StructuredContent: map[string]any{
					"nested": map[string]any{
						"array": []any{1, 2, 3, "string", true, nil},
						"object": map[string]any{
							"deep": map[string]any{
								"value": "very deep",
							},
						},
					},
					"mixed_types": []any{
						map[string]any{"type": "object"},
						"string",
						42.5,
						true,
						nil,
					},
				},
			},
		},
		{
			name: "result with empty structured content object",
			result: CallToolResult{
				Content: []Content{
					TextContent{Type: "text", Text: "Empty structured content"},
				},
				StructuredContent: map[string]any{},
			},
		},
		{
			name: "result with null structured content in JSON",
			jsonData: `{
				"content": [{"type": "text", "text": "Null structured content"}],
				"structuredContent": null
			}`,
		},
		{
			name: "result with missing isError field",
			jsonData: `{
				"content": [{"type": "text", "text": "No error field"}]
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var data []byte
			var err error

			if tt.jsonData != "" {
				// Test unmarshaling from JSON
				var result CallToolResult
				err = json.Unmarshal([]byte(tt.jsonData), &result)
				assert.NoError(t, err)

				// Verify the result can be marshaled back
				data, err = json.Marshal(result)
				assert.NoError(t, err)
			} else {
				// Test marshaling the result
				data, err = json.Marshal(tt.result)
				assert.NoError(t, err)

				// Verify it can be unmarshaled back
				var result CallToolResult
				err = json.Unmarshal(data, &result)
				assert.NoError(t, err)
			}

			// Verify the JSON is valid
			var jsonMap map[string]any
			err = json.Unmarshal(data, &jsonMap)
			assert.NoError(t, err)
		})
	}
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

// TestToolMetaMarshaling tests that the Meta field is properly marshaled as _meta in JSON output
func TestToolMetaMarshaling(t *testing.T) {
	meta := map[string]any{"version": "1.0.0", "author": "test"}
	// Marshal the tool to JSON
	data, err := json.Marshal(Tool{
		Name:        "test-tool",
		Description: "A test tool with meta data",
		Meta:        NewMetaFromMap(meta),
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "Test input",
				},
			},
		},
	})
	assert.NoError(t, err)

	// Unmarshal to map for comparison
	var result map[string]any
	err = json.Unmarshal(data, &result)
	assert.NoError(t, err)

	// Check if _meta field is present and correct
	assert.Contains(t, result, "_meta", "Tool with Meta should include _meta field")
	assert.Equal(t, meta, result["_meta"], "_meta field should match expected value")
}

func TestToolMetaMarshalingOmitsWhenNil(t *testing.T) {
	// Marshal a tool without Meta
	data, err := json.Marshal(Tool{
		Name:        "test-tool-no-meta",
		Description: "A test tool without meta data",
		InputSchema: ToolInputSchema{
			Type:       "object",
			Properties: map[string]any{},
		},
	})
	assert.NoError(t, err)

	// Unmarshal to map
	var result map[string]any
	err = json.Unmarshal(data, &result)
	assert.NoError(t, err)

	// Check that _meta field is not present
	assert.NotContains(t, result, "_meta", "Tool without Meta should not include _meta field")
}

func TestToolArgumentsSchema_UnmarshalWithDefinitions(t *testing.T) {
	// Test that "definitions" (JSON Schema draft-07) is properly unmarshaled into Defs field
	jsonData := `{
		"type": "object",
		"properties": {
			"operation": {
				"$ref": "#/definitions/operation_type"
			}
		},
		"required": ["operation"],
		"definitions": {
			"operation_type": {
				"type": "string",
				"enum": ["create", "read", "update", "delete"]
			}
		}
	}`

	var schema ToolArgumentsSchema
	err := json.Unmarshal([]byte(jsonData), &schema)
	assert.NoError(t, err)

	// Verify the schema was properly unmarshaled
	assert.Equal(t, "object", schema.Type)
	assert.Contains(t, schema.Properties, "operation")
	assert.Equal(t, []string{"operation"}, schema.Required)

	// Most importantly: verify that "definitions" was read into Defs field
	assert.NotNil(t, schema.Defs)
	assert.Contains(t, schema.Defs, "operation_type")

	operationType, ok := schema.Defs["operation_type"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "string", operationType["type"])
	assert.NotNil(t, operationType["enum"])
}

func TestToolArgumentsSchema_UnmarshalWithDefs(t *testing.T) {
	// Test that "$defs" (JSON Schema 2019-09+) is properly unmarshaled into Defs field
	jsonData := `{
		"type": "object",
		"properties": {
			"operation": {
				"$ref": "#/$defs/operation_type"
			}
		},
		"required": ["operation"],
		"$defs": {
			"operation_type": {
				"type": "string",
				"enum": ["create", "read", "update", "delete"]
			}
		}
	}`

	var schema ToolArgumentsSchema
	err := json.Unmarshal([]byte(jsonData), &schema)
	assert.NoError(t, err)

	// Verify the schema was properly unmarshaled
	assert.Equal(t, "object", schema.Type)
	assert.Contains(t, schema.Properties, "operation")
	assert.Equal(t, []string{"operation"}, schema.Required)

	// Verify that "$defs" was read into Defs field
	assert.NotNil(t, schema.Defs)
	assert.Contains(t, schema.Defs, "operation_type")

	operationType, ok := schema.Defs["operation_type"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "string", operationType["type"])
	assert.NotNil(t, operationType["enum"])
}

func TestToolArgumentsSchema_UnmarshalPrefersDefs(t *testing.T) {
	// Test that if both "$defs" and "definitions" are present, "$defs" takes precedence
	jsonData := `{
		"type": "object",
		"$defs": {
			"from_defs": {
				"type": "string"
			}
		},
		"definitions": {
			"from_definitions": {
				"type": "integer"
			}
		}
	}`

	var schema ToolArgumentsSchema
	err := json.Unmarshal([]byte(jsonData), &schema)
	assert.NoError(t, err)

	// $defs should take precedence
	assert.Contains(t, schema.Defs, "from_defs")
	assert.NotContains(t, schema.Defs, "from_definitions")
}

func TestToolArgumentsSchema_MarshalRoundTrip(t *testing.T) {
	// Test that marshaling and unmarshaling preserves definitions
	original := ToolArgumentsSchema{
		Type: "object",
		Properties: map[string]any{
			"field": map[string]any{
				"$ref": "#/$defs/my_type",
			},
		},
		Required: []string{"field"},
		Defs: map[string]any{
			"my_type": map[string]any{
				"type": "string",
				"enum": []string{"a", "b", "c"},
			},
		},
	}

	// Marshal
	data, err := json.Marshal(original)
	assert.NoError(t, err)

	// Unmarshal
	var unmarshaled ToolArgumentsSchema
	err = json.Unmarshal(data, &unmarshaled)
	assert.NoError(t, err)

	// Verify round-trip
	assert.Equal(t, original.Type, unmarshaled.Type)
	assert.Equal(t, original.Required, unmarshaled.Required)
	assert.NotNil(t, unmarshaled.Defs)
	assert.Contains(t, unmarshaled.Defs, "my_type")
}

func TestWithToolIcons(t *testing.T) {
	tool := Tool{}
	icons := []Icon{
		{Src: "tool-icon.png", MIMEType: "image/png"},
	}
	opt := WithToolIcons(icons...)
	opt(&tool)

	assert.Equal(t, icons, tool.Icons)
}

func TestToolArgumentsSchema_UnmarshalWithAdditionalPropertiesFalse(t *testing.T) {
	jsonData := `{
		"type": "object",
		"properties": {
			"name": {"type": "string"}
		},
		"additionalProperties": false
	}`

	var schema ToolArgumentsSchema
	err := json.Unmarshal([]byte(jsonData), &schema)
	assert.NoError(t, err)

	assert.Equal(t, "object", schema.Type)
	assert.Contains(t, schema.Properties, "name")
	assert.Equal(t, false, schema.AdditionalProperties)
}

func TestToolArgumentsSchema_UnmarshalWithAdditionalPropertiesSchema(t *testing.T) {
	jsonData := `{
		"type": "object",
		"properties": {
			"name": {"type": "string"}
		},
		"additionalProperties": {"type": "string"}
	}`

	var schema ToolArgumentsSchema
	err := json.Unmarshal([]byte(jsonData), &schema)
	assert.NoError(t, err)

	assert.Equal(t, "object", schema.Type)
	additionalProps, ok := schema.AdditionalProperties.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "string", additionalProps["type"])
}

func TestToolArgumentsSchema_MarshalWithAdditionalProperties(t *testing.T) {
	schema := ToolArgumentsSchema{
		Type:                 "object",
		AdditionalProperties: false,
	}

	data, err := json.Marshal(schema)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"additionalProperties":false`)
}

func TestToolArgumentsSchema_MarshalOmitsNilAdditionalProperties(t *testing.T) {
	schema := ToolArgumentsSchema{
		Type:                 "object",
		AdditionalProperties: nil,
	}

	data, err := json.Marshal(schema)
	assert.NoError(t, err)
	assert.NotContains(t, string(data), "additionalProperties")
}

func TestWithSchemaAdditionalProperties(t *testing.T) {
	tool := NewTool(
		"strict-tool",
		WithSchemaAdditionalProperties(false),
	)

	assert.Equal(t, false, tool.InputSchema.AdditionalProperties)

	data, err := json.Marshal(tool)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"additionalProperties":false`)
}

// TestToolExecutionMarshaling tests that the Execution field is properly marshaled in JSON output
func TestToolExecutionMarshaling(t *testing.T) {
	tests := []struct {
		name            string
		tool            Tool
		expectExecution bool
		expectedSupport TaskSupport
	}{
		{
			name: "tool with task support forbidden",
			tool: Tool{
				Name:        "forbidden-tool",
				Description: "A tool that forbids task augmentation",
				InputSchema: ToolInputSchema{
					Type:       "object",
					Properties: map[string]any{},
				},
				Execution: &ToolExecution{
					TaskSupport: TaskSupportForbidden,
				},
			},
			expectExecution: true,
			expectedSupport: TaskSupportForbidden,
		},
		{
			name: "tool with task support optional",
			tool: Tool{
				Name:        "optional-tool",
				Description: "A tool that optionally supports task augmentation",
				InputSchema: ToolInputSchema{
					Type:       "object",
					Properties: map[string]any{},
				},
				Execution: &ToolExecution{
					TaskSupport: TaskSupportOptional,
				},
			},
			expectExecution: true,
			expectedSupport: TaskSupportOptional,
		},
		{
			name: "tool with task support required",
			tool: Tool{
				Name:        "required-tool",
				Description: "A tool that requires task augmentation",
				InputSchema: ToolInputSchema{
					Type:       "object",
					Properties: map[string]any{},
				},
				Execution: &ToolExecution{
					TaskSupport: TaskSupportRequired,
				},
			},
			expectExecution: true,
			expectedSupport: TaskSupportRequired,
		},
		{
			name: "tool without execution field",
			tool: Tool{
				Name:        "no-execution-tool",
				Description: "A tool without execution configuration",
				InputSchema: ToolInputSchema{
					Type:       "object",
					Properties: map[string]any{},
				},
				Execution: nil,
			},
			expectExecution: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal the tool to JSON
			data, err := json.Marshal(tt.tool)
			assert.NoError(t, err)

			// Unmarshal to map for comparison
			var result map[string]any
			err = json.Unmarshal(data, &result)
			assert.NoError(t, err)

			if tt.expectExecution {
				// Check if execution field is present
				assert.Contains(t, result, "execution", "Tool should include execution field")

				execution, ok := result["execution"].(map[string]any)
				assert.True(t, ok, "Execution field should be a map")

				taskSupport, ok := execution["taskSupport"].(string)
				assert.True(t, ok, "taskSupport should be a string")
				assert.Equal(t, string(tt.expectedSupport), taskSupport, "taskSupport value should match")
			} else {
				// Check that execution field is not present
				assert.NotContains(t, result, "execution", "Tool without Execution should not include execution field")
			}
		})
	}
}

// TestTaskSupportConstants verifies the TaskSupport constants have the correct values
func TestTaskSupportConstants(t *testing.T) {
	assert.Equal(t, TaskSupport("forbidden"), TaskSupportForbidden)
	assert.Equal(t, TaskSupport("optional"), TaskSupportOptional)
	assert.Equal(t, TaskSupport("required"), TaskSupportRequired)
}

// TestWithTaskSupport tests the WithTaskSupport option for configuring tool task support
func TestWithTaskSupport(t *testing.T) {
	tests := []struct {
		name            string
		taskSupport     TaskSupport
		expectedSupport TaskSupport
	}{
		{
			name:            "task support forbidden",
			taskSupport:     TaskSupportForbidden,
			expectedSupport: TaskSupportForbidden,
		},
		{
			name:            "task support optional",
			taskSupport:     TaskSupportOptional,
			expectedSupport: TaskSupportOptional,
		},
		{
			name:            "task support required",
			taskSupport:     TaskSupportRequired,
			expectedSupport: TaskSupportRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a tool with task support
			tool := NewTool("test-tool",
				WithDescription("A test tool"),
				WithTaskSupport(tt.taskSupport),
			)

			// Verify the Execution field is set
			assert.NotNil(t, tool.Execution, "Execution should not be nil")
			assert.Equal(t, tt.expectedSupport, tool.Execution.TaskSupport, "TaskSupport should match")

			// Marshal to JSON and verify structure
			data, err := json.Marshal(tool)
			assert.NoError(t, err)

			var result map[string]any
			err = json.Unmarshal(data, &result)
			assert.NoError(t, err)

			// Verify execution field is present in JSON
			assert.Contains(t, result, "execution", "Tool should include execution field in JSON")

			execution, ok := result["execution"].(map[string]any)
			assert.True(t, ok, "Execution field should be a map")

			taskSupport, ok := execution["taskSupport"].(string)
			assert.True(t, ok, "taskSupport should be a string")
			assert.Equal(t, string(tt.expectedSupport), taskSupport, "taskSupport value should match in JSON")
		})
	}
}

// TestWithTaskSupport_InitializesExecution tests that WithTaskSupport creates Execution if nil
func TestWithTaskSupport_InitializesExecution(t *testing.T) {
	// Create a tool without any execution configuration
	tool := Tool{
		Name:        "test-tool",
		Description: "A test tool",
		InputSchema: ToolInputSchema{
			Type:       "object",
			Properties: map[string]any{},
		},
		Execution: nil,
	}

	// Verify Execution is nil
	assert.Nil(t, tool.Execution)

	// Apply WithTaskSupport option
	option := WithTaskSupport(TaskSupportOptional)
	option(&tool)

	// Verify Execution is now initialized
	assert.NotNil(t, tool.Execution, "WithTaskSupport should initialize Execution if nil")
	assert.Equal(t, TaskSupportOptional, tool.Execution.TaskSupport)
}

// TestWithTaskSupport_PreservesExistingExecution tests that WithTaskSupport doesn't overwrite existing Execution
func TestWithTaskSupport_PreservesExistingExecution(t *testing.T) {
	// Create a tool with existing Execution
	existingExecution := &ToolExecution{
		TaskSupport: TaskSupportForbidden,
	}

	tool := Tool{
		Name:        "test-tool",
		Description: "A test tool",
		InputSchema: ToolInputSchema{
			Type:       "object",
			Properties: map[string]any{},
		},
		Execution: existingExecution,
	}

	// Apply WithTaskSupport option
	option := WithTaskSupport(TaskSupportRequired)
	option(&tool)

	// Verify Execution is the same instance (pointer equality)
	assert.Same(t, existingExecution, tool.Execution, "WithTaskSupport should preserve existing Execution instance")
	// Verify TaskSupport was updated
	assert.Equal(t, TaskSupportRequired, tool.Execution.TaskSupport)
}

// TestCallToolRequest_WithTaskParams tests that CallToolRequest properly unmarshals task params
func TestCallToolRequest_WithTaskParams(t *testing.T) {
	tests := []struct {
		name     string
		jsonData string
		expected CallToolRequest
		wantErr  bool
	}{
		{
			name: "request with task params",
			jsonData: `{
				"method": "tools/call",
				"params": {
					"name": "test-tool",
					"arguments": {
						"input": "test"
					},
					"task": {
						"ttl": 300
					}
				}
			}`,
			expected: CallToolRequest{
				Request: Request{
					Method: "tools/call",
				},
				Params: CallToolParams{
					Name: "test-tool",
					Arguments: map[string]any{
						"input": "test",
					},
					Task: &TaskParams{
						TTL: ToInt64Ptr(300),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "request without task params",
			jsonData: `{
				"method": "tools/call",
				"params": {
					"name": "test-tool",
					"arguments": {
						"input": "test"
					}
				}
			}`,
			expected: CallToolRequest{
				Request: Request{
					Method: "tools/call",
				},
				Params: CallToolParams{
					Name: "test-tool",
					Arguments: map[string]any{
						"input": "test",
					},
					Task: nil,
				},
			},
			wantErr: false,
		},
		{
			name: "request with null task params",
			jsonData: `{
				"method": "tools/call",
				"params": {
					"name": "test-tool",
					"arguments": {
						"input": "test"
					},
					"task": null
				}
			}`,
			expected: CallToolRequest{
				Request: Request{
					Method: "tools/call",
				},
				Params: CallToolParams{
					Name: "test-tool",
					Arguments: map[string]any{
						"input": "test",
					},
					Task: nil,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result CallToolRequest
			err := json.Unmarshal([]byte(tt.jsonData), &result)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)

			// Compare method
			assert.Equal(t, tt.expected.Method, result.Method)

			// Compare tool name
			assert.Equal(t, tt.expected.Params.Name, result.Params.Name)

			// Compare arguments
			expectedArgs, _ := tt.expected.Params.Arguments.(map[string]any)
			resultArgs := result.GetArguments()
			assert.Equal(t, expectedArgs, resultArgs)

			// Compare task params
			if tt.expected.Params.Task != nil {
				assert.NotNil(t, result.Params.Task, "Task params should not be nil")
				assert.Equal(t, tt.expected.Params.Task.TTL, result.Params.Task.TTL)
			} else {
				assert.Nil(t, result.Params.Task, "Task params should be nil")
			}
		})
	}
}

// TestCallToolRequest_WithTaskParams_RoundTrip tests that marshaling and unmarshaling preserves task params
func TestCallToolRequest_WithTaskParams_RoundTrip(t *testing.T) {
	original := CallToolRequest{
		Request: Request{
			Method: "tools/call",
		},
		Params: CallToolParams{
			Name: "async-tool",
			Arguments: map[string]any{
				"operation": "process",
				"count":     42,
			},
			Task: &TaskParams{
				TTL: ToInt64Ptr(600),
			},
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	assert.NoError(t, err)

	// Unmarshal back
	var unmarshaled CallToolRequest
	err = json.Unmarshal(data, &unmarshaled)
	assert.NoError(t, err)

	// Verify all fields are preserved
	assert.Equal(t, original.Method, unmarshaled.Method)
	assert.Equal(t, original.Params.Name, unmarshaled.Params.Name)

	// Compare arguments
	assert.Equal(t, "process", unmarshaled.GetString("operation", ""))
	assert.Equal(t, 42, unmarshaled.GetInt("count", 0))

	// Compare task params
	assert.NotNil(t, unmarshaled.Params.Task)
	assert.Equal(t, original.Params.Task.TTL, unmarshaled.Params.Task.TTL)
}
