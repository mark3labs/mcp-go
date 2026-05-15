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

// TestResourceLinkTitleAndSize mirrors TestResourceTitleAndSize: per the MCP
// spec a ResourceLink carries the same metadata as a Resource, so Title and
// Size must round-trip through JSON the same way they do for Resource. The
// pointer Size lets an explicit zero stay distinguishable from an unset value.
func TestResourceLinkTitleAndSize(t *testing.T) {
	tests := []struct {
		name        string
		title       string
		size        *int64
		wantJSON    []string
		notWantJSON []string
	}{
		{
			name:        "title and size omitted when unset",
			notWantJSON: []string{`"title"`, `"size"`},
		},
		{
			name:     "title and size round-trip when set",
			title:    "X File",
			size:     ToInt64Ptr(1024),
			wantJSON: []string{`"title":"X File"`, `"size":1024`},
		},
		{
			name:     "explicit zero size is preserved",
			size:     ToInt64Ptr(0),
			wantJSON: []string{`"size":0`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rl := NewResourceLink("file:///x.txt", "x.txt", "", "")
			rl.Title = tt.title
			rl.Size = tt.size

			data, err := json.Marshal(rl)
			require.NoError(t, err)
			s := string(data)
			for _, want := range tt.wantJSON {
				assert.Contains(t, s, want)
			}
			for _, notWant := range tt.notWantJSON {
				assert.NotContains(t, s, notWant)
			}

			var rt ResourceLink
			require.NoError(t, json.Unmarshal(data, &rt))
			assert.Equal(t, tt.title, rt.Title)
			assert.Equal(t, tt.size, rt.Size)
		})
	}
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

func TestResourceContentsMetaField(t *testing.T) {
	tests := []struct {
		name         string
		inputJSON    string
		expectedType string
		expectedMeta map[string]any
	}{
		{
			name: "TextResourceContents with empty _meta",
			inputJSON: `{
				"uri":"file://empty-meta.txt",
				"mimeType":"text/plain",
				"text":"x",
				"_meta": {}
			}`,
			expectedType: "text",
			expectedMeta: map[string]any{},
		},
		{
			name: "TextResourceContents with _meta field",
			inputJSON: `{
				"uri": "file://test.txt",
				"mimeType": "text/plain",
				"text": "Hello World",
				"_meta": {
					"mcpui.dev/ui-preferred-frame-size": ["800px", "600px"],
					"mcpui.dev/ui-initial-render-data": {
						"test": "value"
					}
				}
			}`,
			expectedType: "text",
			expectedMeta: map[string]any{
				"mcpui.dev/ui-preferred-frame-size": []any{"800px", "600px"},
				"mcpui.dev/ui-initial-render-data": map[string]any{
					"test": "value",
				},
			},
		},
		{
			name: "BlobResourceContents with _meta field",
			inputJSON: `{
				"uri": "file://image.png",
				"mimeType": "image/png",
				"blob": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg==",
				"_meta": {
					"width": 100,
					"height": 100,
					"format": "PNG"
				}
			}`,
			expectedType: "blob",
			expectedMeta: map[string]any{
				"width":  float64(100), // JSON numbers are always float64
				"height": float64(100),
				"format": "PNG",
			},
		},
		{
			name: "TextResourceContents without _meta field",
			inputJSON: `{
				"uri": "file://simple.txt",
				"mimeType": "text/plain",
				"text": "Simple content"
			}`,
			expectedType: "text",
			expectedMeta: nil,
		},
		{
			name: "BlobResourceContents without _meta field",
			inputJSON: `{
				"uri": "file://simple.png",
				"mimeType": "image/png",
				"blob": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg=="
			}`,
			expectedType: "blob",
			expectedMeta: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Parse the JSON as a generic map first
			var contentMap map[string]any
			err := json.Unmarshal([]byte(tc.inputJSON), &contentMap)
			require.NoError(t, err)

			// Use ParseResourceContents to convert to ResourceContents
			resourceContent, err := ParseResourceContents(contentMap)
			require.NoError(t, err)
			require.NotNil(t, resourceContent)

			switch tc.expectedType {
			case "text":
				textContent, ok := resourceContent.(TextResourceContents)
				require.True(t, ok, "Expected TextResourceContents")

				assert.Equal(t, contentMap["uri"], textContent.URI)
				assert.Equal(t, contentMap["mimeType"], textContent.MIMEType)
				assert.Equal(t, contentMap["text"], textContent.Text)

				assert.Equal(t, tc.expectedMeta, textContent.Meta)

			case "blob":
				blobContent, ok := resourceContent.(BlobResourceContents)
				require.True(t, ok, "Expected BlobResourceContents")

				assert.Equal(t, contentMap["uri"], blobContent.URI)
				assert.Equal(t, contentMap["mimeType"], blobContent.MIMEType)
				assert.Equal(t, contentMap["blob"], blobContent.Blob)

				assert.Equal(t, tc.expectedMeta, blobContent.Meta)
			}

			// Test round-trip marshaling to ensure _meta is preserved
			marshaledJSON, err := json.Marshal(resourceContent)
			require.NoError(t, err)

			var marshaledMap map[string]any
			err = json.Unmarshal(marshaledJSON, &marshaledMap)
			require.NoError(t, err)

			// Verify _meta field is preserved in marshaled output
			v, ok := marshaledMap["_meta"]
			if tc.expectedMeta != nil {
				// Special case: empty maps are omitted due to omitempty tag
				if len(tc.expectedMeta) == 0 {
					assert.False(t, ok, "_meta should be omitted when empty due to omitempty")
				} else {
					require.True(t, ok, "_meta should be present")
					assert.Equal(t, tc.expectedMeta, v)
				}
			} else {
				assert.False(t, ok, "_meta should be omitted when nil")
			}
		})
	}
}

func TestParseResourceContentsInvalidMeta(t *testing.T) {
	tests := []struct {
		name        string
		inputJSON   string
		expectedErr string
	}{
		{
			name: "TextResourceContents with invalid _meta (string)",
			inputJSON: `{
				"uri": "file://test.txt",
				"mimeType": "text/plain",
				"text": "Hello World",
				"_meta": "invalid_meta_string"
			}`,
			expectedErr: "_meta must be an object",
		},
		{
			name: "TextResourceContents with invalid _meta (number)",
			inputJSON: `{
				"uri": "file://test.txt",
				"mimeType": "text/plain",
				"text": "Hello World",
				"_meta": 123
			}`,
			expectedErr: "_meta must be an object",
		},
		{
			name: "TextResourceContents with invalid _meta (array)",
			inputJSON: `{
				"uri": "file://test.txt",
				"mimeType": "text/plain",
				"text": "Hello World",
				"_meta": ["invalid", "array"]
			}`,
			expectedErr: "_meta must be an object",
		},
		{
			name: "TextResourceContents with invalid _meta (boolean)",
			inputJSON: `{
				"uri": "file://test.txt",
				"mimeType": "text/plain",
				"text": "Hello World",
				"_meta": true
			}`,
			expectedErr: "_meta must be an object",
		},
		{
			name: "TextResourceContents with invalid _meta (null)",
			inputJSON: `{
				"uri": "file://test.txt",
				"mimeType": "text/plain",
				"text": "Hello World",
				"_meta": null
			}`,
			expectedErr: "_meta must be an object",
		},
		{
			name: "BlobResourceContents with invalid _meta (string)",
			inputJSON: `{
				"uri": "file://image.png",
				"mimeType": "image/png",
				"blob": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg==",
				"_meta": "invalid_meta_string"
			}`,
			expectedErr: "_meta must be an object",
		},
		{
			name: "BlobResourceContents with invalid _meta (number)",
			inputJSON: `{
				"uri": "file://image.png",
				"mimeType": "image/png",
				"blob": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg==",
				"_meta": 456
			}`,
			expectedErr: "_meta must be an object",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Parse the JSON as a generic map first
			var contentMap map[string]any
			err := json.Unmarshal([]byte(tc.inputJSON), &contentMap)
			require.NoError(t, err)

			// Use ParseResourceContents to convert to ResourceContents
			resourceContent, err := ParseResourceContents(contentMap)

			// Expect an error
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.expectedErr)
			assert.Nil(t, resourceContent)
		})
	}
}

func TestCompleteParamsUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name        string
		inputJSON   string
		expected    CompleteParams
		expectedErr string
	}{
		{
			name: "PromptReference",
			inputJSON: `{
				"ref": {
					"type": "ref/prompt",
					"name": "test-prompt"
				},
				"argument": {
					"name": "test-arg",
					"value": "test-value"
				}
			}`,
			expectedErr: "",
			expected: CompleteParams{
				Ref: PromptReference{
					Type: "ref/prompt",
					Name: "test-prompt",
				},
				Argument: CompleteArgument{
					Name:  "test-arg",
					Value: "test-value",
				},
			},
		},
		{
			name: "ResourceReference",
			inputJSON: `{
				"ref": {
					"type": "ref/resource",
					"uri": "file://{param}/example"
				},
				"argument": {
					"name": "param",
					"value": "test-value"
				}
			}`,
			expectedErr: "",
			expected: CompleteParams{
				Ref: ResourceReference{
					Type: "ref/resource",
					URI:  "file://{param}/example",
				},
				Argument: CompleteArgument{
					Name:  "param",
					Value: "test-value",
				},
			},
		},
		{
			name: "Invalid reference type",
			inputJSON: `{
				"ref": {
					"type": "invalid",
					"name": "test-prompt"
				}
			}`,
			expectedErr: "unknown reference type: invalid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got CompleteParams
			err := json.Unmarshal([]byte(tc.inputJSON), &got)
			if tc.expectedErr != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedErr)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, got)
			}
		})
	}
}

func TestPaginatedParamsMetaMarshalling(t *testing.T) {
	// Marshalling the full request (rather than just req.Params) is what
	// exercises the embedding/shadowing in PaginatedRequest -> Request.
	// Without Meta on PaginatedParams, the inner Request.Params.Meta would
	// be silently dropped at the outer-struct level.
	t.Run("ListToolsRequest with _meta serializes the meta field on params", func(t *testing.T) {
		req := ListToolsRequest{}
		req.Params.Cursor = "page-2"
		req.Params.Meta = &Meta{
			AdditionalFields: map[string]any{"orgId": "org-1", "appId": "app-7"},
		}

		data, err := json.Marshal(req)
		require.NoError(t, err)

		var got map[string]any
		require.NoError(t, json.Unmarshal(data, &got))
		params, ok := got["params"].(map[string]any)
		require.True(t, ok, "params object missing from marshaled request: %s", data)
		assert.Equal(t, "page-2", params["cursor"])
		meta, ok := params["_meta"].(map[string]any)
		require.True(t, ok, "_meta object missing from marshaled params: %s", data)
		assert.Equal(t, "org-1", meta["orgId"])
		assert.Equal(t, "app-7", meta["appId"])
	})

	t.Run("ListToolsRequest without _meta omits the meta field on params", func(t *testing.T) {
		req := ListToolsRequest{}
		req.Params.Cursor = "page-2"

		data, err := json.Marshal(req)
		require.NoError(t, err)

		var got map[string]any
		require.NoError(t, json.Unmarshal(data, &got))
		params, ok := got["params"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "page-2", params["cursor"])
		_, hasMeta := params["_meta"]
		assert.False(t, hasMeta, "_meta should be omitted: %s", data)
	})

	t.Run("ListToolsRequest with only _meta serializes _meta and omits cursor", func(t *testing.T) {
		req := ListToolsRequest{}
		req.Params.Meta = &Meta{
			AdditionalFields: map[string]any{"orgId": "org-1"},
		}

		data, err := json.Marshal(req)
		require.NoError(t, err)

		var got map[string]any
		require.NoError(t, json.Unmarshal(data, &got))
		params, ok := got["params"].(map[string]any)
		require.True(t, ok)
		_, hasCursor := params["cursor"]
		assert.False(t, hasCursor, "cursor should be omitted: %s", data)
		meta, ok := params["_meta"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "org-1", meta["orgId"])
	})

	t.Run("PaginatedParams round-trips _meta via JSON", func(t *testing.T) {
		original := PaginatedParams{
			Cursor: "abc",
			Meta: &Meta{
				ProgressToken:    "tok-1",
				AdditionalFields: map[string]any{"k": "v"},
			},
		}
		data, err := json.Marshal(original)
		require.NoError(t, err)

		var got PaginatedParams
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Equal(t, original.Cursor, got.Cursor)
		require.NotNil(t, got.Meta)
		assert.Equal(t, original.Meta.ProgressToken, got.Meta.ProgressToken)
		assert.Equal(t, "v", got.Meta.AdditionalFields["k"])
	})
}

func TestSamplingCapability_JSON(t *testing.T) {
	t.Run("empty capability marshals as empty object", func(t *testing.T) {
		caps := ClientCapabilities{Sampling: &SamplingCapability{}}
		data, err := json.Marshal(caps)
		require.NoError(t, err)
		assert.JSONEq(t, `{"sampling":{}}`, string(data))
	})

	t.Run("nil sampling field is omitted", func(t *testing.T) {
		caps := ClientCapabilities{}
		data, err := json.Marshal(caps)
		require.NoError(t, err)
		assert.JSONEq(t, `{}`, string(data))
	})

	t.Run("context sub-capability marshals nested", func(t *testing.T) {
		caps := ClientCapabilities{Sampling: &SamplingCapability{
			Context: &struct{}{},
		}}
		data, err := json.Marshal(caps)
		require.NoError(t, err)
		assert.JSONEq(t, `{"sampling":{"context":{}}}`, string(data))
	})

	t.Run("tools sub-capability marshals nested", func(t *testing.T) {
		caps := ClientCapabilities{Sampling: &SamplingCapability{
			Tools: &struct{}{},
		}}
		data, err := json.Marshal(caps)
		require.NoError(t, err)
		assert.JSONEq(t, `{"sampling":{"tools":{}}}`, string(data))
	})

	t.Run("both sub-capabilities round-trip", func(t *testing.T) {
		original := ClientCapabilities{Sampling: &SamplingCapability{
			Context: &struct{}{},
			Tools:   &struct{}{},
		}}
		data, err := json.Marshal(original)
		require.NoError(t, err)
		assert.JSONEq(t, `{"sampling":{"context":{},"tools":{}}}`, string(data))

		var got ClientCapabilities
		require.NoError(t, json.Unmarshal(data, &got))
		require.NotNil(t, got.Sampling)
		require.NotNil(t, got.Sampling.Context)
		require.NotNil(t, got.Sampling.Tools)
	})

	t.Run("unmarshalling bare sampling object yields empty capability", func(t *testing.T) {
		var got ClientCapabilities
		require.NoError(t, json.Unmarshal([]byte(`{"sampling":{}}`), &got))
		require.NotNil(t, got.Sampling)
		assert.Nil(t, got.Sampling.Context)
		assert.Nil(t, got.Sampling.Tools)
	})

	t.Run("server capability uses the same shape", func(t *testing.T) {
		caps := ServerCapabilities{Sampling: &SamplingCapability{
			Context: &struct{}{},
		}}
		data, err := json.Marshal(caps)
		require.NoError(t, err)
		assert.JSONEq(t, `{"sampling":{"context":{}}}`, string(data))
	})
}

func TestCreateMessageParams_ToolsJSON(t *testing.T) {
	t.Run("tools and toolChoice are omitted when nil", func(t *testing.T) {
		params := CreateMessageParams{
			Messages:  []SamplingMessage{{Role: RoleUser, Content: NewTextContent("hi")}},
			MaxTokens: 10,
		}
		data, err := json.Marshal(params)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "tools")
		assert.NotContains(t, string(data), "toolChoice")
	})

	t.Run("tools and toolChoice marshal and round-trip", func(t *testing.T) {
		params := CreateMessageParams{
			Messages:  []SamplingMessage{{Role: RoleUser, Content: NewTextContent("hi")}},
			MaxTokens: 10,
			Tools: []Tool{
				{Name: "get_weather", Description: "Look up weather"},
			},
			ToolChoice: &ToolChoice{Mode: ToolChoiceModeRequired},
		}
		data, err := json.Marshal(params)
		require.NoError(t, err)
		assert.Contains(t, string(data), `"tools"`)
		assert.Contains(t, string(data), `"toolChoice":{"mode":"required"}`)

		var got CreateMessageParams
		require.NoError(t, json.Unmarshal(data, &got))
		require.Len(t, got.Tools, 1)
		assert.Equal(t, "get_weather", got.Tools[0].Name)
		require.NotNil(t, got.ToolChoice)
		assert.Equal(t, ToolChoiceModeRequired, got.ToolChoice.Mode)
	})

	t.Run("empty ToolChoice marshals as empty object", func(t *testing.T) {
		params := CreateMessageParams{
			Messages:   []SamplingMessage{{Role: RoleUser, Content: NewTextContent("hi")}},
			MaxTokens:  10,
			ToolChoice: &ToolChoice{},
		}
		data, err := json.Marshal(params)
		require.NoError(t, err)
		assert.Contains(t, string(data), `"toolChoice":{}`)
	})

	t.Run("tool choice mode constants match spec", func(t *testing.T) {
		assert.Equal(t, ToolChoiceMode("auto"), ToolChoiceModeAuto)
		assert.Equal(t, ToolChoiceMode("required"), ToolChoiceModeRequired)
		assert.Equal(t, ToolChoiceMode("none"), ToolChoiceModeNone)
	})
}
