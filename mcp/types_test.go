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

func TestResourceLinkSerialization(t *testing.T) {
	resourceLink := NewResourceLink(
		"file:///example/document.pdf",
		"Sample Document",
		"A sample document for testing",
		"application/pdf",
	)

	// Test marshaling
	data, err := json.Marshal(resourceLink)
	require.NoError(t, err)

	// Test unmarshaling
	var unmarshaled ResourceLink
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	// Verify fields
	assert.Equal(t, "resource_link", unmarshaled.Type)
	assert.Equal(t, "file:///example/document.pdf", unmarshaled.URI)
	assert.Equal(t, "Sample Document", unmarshaled.Name)
	assert.Equal(t, "A sample document for testing", unmarshaled.Description)
	assert.Equal(t, "application/pdf", unmarshaled.MIMEType)
}

func TestCallToolResultWithResourceLink(t *testing.T) {
	result := &CallToolResult{
		Content: []Content{
			TextContent{
				Type: "text",
				Text: "Here's a resource link:",
			},
			NewResourceLink(
				"file:///example/test.pdf",
				"Test Document",
				"A test document",
				"application/pdf",
			),
		},
		IsError: false,
	}

	// Test marshaling
	data, err := json.Marshal(result)
	require.NoError(t, err)

	// Test unmarshalling
	var unmarshalled CallToolResult
	err = json.Unmarshal(data, &unmarshalled)
	require.NoError(t, err)

	// Verify content
	require.Len(t, unmarshalled.Content, 2)

	// Check first content (TextContent)
	textContent, ok := unmarshalled.Content[0].(TextContent)
	require.True(t, ok)
	assert.Equal(t, "text", textContent.Type)
	assert.Equal(t, "Here's a resource link:", textContent.Text)

	// Check second content (ResourceLink)
	resourceLink, ok := unmarshalled.Content[1].(ResourceLink)
	require.True(t, ok)
	assert.Equal(t, "resource_link", resourceLink.Type)
	assert.Equal(t, "file:///example/test.pdf", resourceLink.URI)
	assert.Equal(t, "Test Document", resourceLink.Name)
	assert.Equal(t, "A test document", resourceLink.Description)
	assert.Equal(t, "application/pdf", resourceLink.MIMEType)
}
