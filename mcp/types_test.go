package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetaMarshalling(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		meta    *Meta
		expMeta *Meta
	}{
		{
			name:    "empty",
			json:    "{}",
			meta:    &Meta{},
			expMeta: &Meta{AdditionalFields: map[string]any{}},
		},
		{
			name:    "empty additional fields",
			json:    "{}",
			meta:    &Meta{AdditionalFields: map[string]any{}},
			expMeta: &Meta{AdditionalFields: map[string]any{}},
		},
		{
			name:    "string token only",
			json:    `{"progressToken":"123"}`,
			meta:    &Meta{ProgressToken: "123"},
			expMeta: &Meta{ProgressToken: "123", AdditionalFields: map[string]any{}},
		},
		{
			name:    "string token only, empty additional fields",
			json:    `{"progressToken":"123"}`,
			meta:    &Meta{ProgressToken: "123", AdditionalFields: map[string]any{}},
			expMeta: &Meta{ProgressToken: "123", AdditionalFields: map[string]any{}},
		},
		{
			name: "additional fields only",
			json: `{"a":2,"b":"1"}`,
			meta: &Meta{AdditionalFields: map[string]any{"a": 2, "b": "1"}},
			// For untyped map, numbers are always float64
			expMeta: &Meta{AdditionalFields: map[string]any{"a": float64(2), "b": "1"}},
		},
		{
			name: "progress token and additional fields",
			json: `{"a":2,"b":"1","progressToken":"123"}`,
			meta: &Meta{ProgressToken: "123", AdditionalFields: map[string]any{"a": 2, "b": "1"}},
			// For untyped map, numbers are always float64
			expMeta: &Meta{ProgressToken: "123", AdditionalFields: map[string]any{"a": float64(2), "b": "1"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.meta)
			require.NoError(t, err)
			assert.Equal(t, tc.json, string(data))

			meta := &Meta{}
			err = json.Unmarshal([]byte(tc.json), meta)
			require.NoError(t, err)
			assert.Equal(t, tc.expMeta, meta)
		})
	}
}

// TestGetDisplayName tests the display name logic for all types
func TestGetDisplayName(t *testing.T) {
	tests := []struct {
		name         string
		meta         BaseMetadata
		expectedName string
	}{
		// Tool tests
		{
			name: "tool with direct title",
			meta: &Tool{
				Name:        "test-tool",
				Title:       "Tool Title",
				Annotations: ToolAnnotation{Title: "Annotation Title"},
			},
			expectedName: "Tool Title",
		},
		{
			name: "tool with annotation title only",
			meta: &Tool{
				Name:        "test-tool",
				Annotations: ToolAnnotation{Title: "Annotation Title"},
			},
			expectedName: "Annotation Title",
		},
		{
			name:         "tool falls back to name",
			meta:         &Tool{Name: "test-tool"},
			expectedName: "test-tool",
		},

		// Prompt tests
		{
			name: "prompt with title",
			meta: &Prompt{
				Name:  "test-prompt",
				Title: "Prompt Title",
			},
			expectedName: "Prompt Title",
		},
		{
			name:         "prompt falls back to name",
			meta:         &Prompt{Name: "test-prompt"},
			expectedName: "test-prompt",
		},

		// Resource tests
		{
			name: "resource with title",
			meta: &Resource{
				Name:  "test-resource",
				Title: "Resource Title",
			},
			expectedName: "Resource Title",
		},
		{
			name:         "resource falls back to name",
			meta:         &Resource{Name: "test-resource"},
			expectedName: "test-resource",
		},

		// ResourceTemplate tests
		{
			name: "resource template with title",
			meta: &ResourceTemplate{
				Name:  "test-template",
				Title: "Template Title",
			},
			expectedName: "Template Title",
		},
		{
			name:         "resource template falls back to name",
			meta:         &ResourceTemplate{Name: "test-template"},
			expectedName: "test-template",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectedName, GetDisplayName(tt.meta))
		})
	}
}

// TestToolTitleSerialization tests that Tool title field is properly serialized
func TestToolTitleSerialization(t *testing.T) {
	tool := Tool{
		Name:        "test-tool",
		Title:       "Test Tool Title",
		Description: "A test tool",
		InputSchema: ToolInputSchema{
			Type:       "object",
			Properties: map[string]any{},
		},
		Annotations: ToolAnnotation{
			Title: "Annotation Title",
		},
	}

	// Test serialization
	data, err := json.Marshal(tool)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "test-tool", result["name"])
	assert.Equal(t, "Test Tool Title", result["title"])
	assert.Equal(t, "A test tool", result["description"])

	annotations, ok := result["annotations"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Annotation Title", annotations["title"])

	// Test deserialization
	var deserializedTool Tool
	err = json.Unmarshal(data, &deserializedTool)
	require.NoError(t, err)

	assert.Equal(t, "test-tool", deserializedTool.Name)
	assert.Equal(t, "Test Tool Title", deserializedTool.Title)
	assert.Equal(t, "A test tool", deserializedTool.Description)
	assert.Equal(t, "Annotation Title", deserializedTool.Annotations.Title)

	// Test GetTitle method
	assert.Equal(t, "Test Tool Title", deserializedTool.GetTitle())
	assert.Equal(t, "Test Tool Title", GetDisplayName(&deserializedTool))
}
