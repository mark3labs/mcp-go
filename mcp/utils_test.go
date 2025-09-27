package mcp

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestParseAnnotations(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]any
		expected *Annotations
	}{
		{
			name:     "nil data",
			data:     nil,
			expected: nil,
		},
		{
			name:     "empty data",
			data:     map[string]any{},
			expected: &Annotations{},
		},
		{
			name: "priority only",
			data: map[string]any{
				"priority": 1.5,
			},
			expected: &Annotations{
				Priority: 1.5,
			},
		},
		{
			name: "audience only",
			data: map[string]any{
				"audience": []any{"user", "assistant"},
			},
			expected: &Annotations{
				Audience: []Role{"user", "assistant"},
			},
		},
		{
			name: "priority and audience",
			data: map[string]any{
				"priority": 2.0,
				"audience": []any{"user", "assistant", "system"},
			},
			expected: &Annotations{
				Priority: 2.0,
				Audience: []Role{"user", "assistant"},
			},
		},
		{
			name: "invalid priority type",
			data: map[string]any{
				"priority": "not a number",
			},
			expected: &Annotations{},
		},
		{
			name: "invalid audience type",
			data: map[string]any{
				"audience": "not an array",
			},
			expected: &Annotations{},
		},
		{
			name: "invalid audience element type",
			data: map[string]any{
				"audience": []any{"user", 123, "assistant"},
			},
			expected: &Annotations{
				Audience: []Role{"user", "assistant"},
			},
		},
		{
			name: "audience as []string",
			data: map[string]any{
				"audience": []string{"assistant", "user"},
			},
			expected: &Annotations{
				Audience: []Role{"assistant", "user"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseAnnotations(tt.data)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseContent(t *testing.T) {
	tests := []struct {
		name        string
		contentMap  map[string]any
		expected    Content
		expectError bool
	}{
		{
			name: "text content with annotations",
			contentMap: map[string]any{
				"type": "text",
				"text": "Hello, world!",
				"annotations": map[string]any{
					"priority": 1.5,
					"audience": []any{"user"},
				},
			},
			expected: TextContent{
				Type: ContentTypeText,
				Text: "Hello, world!",
				Annotated: Annotated{
					Annotations: &Annotations{
						Priority: 1.5,
						Audience: []Role{"user"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "text content without annotations",
			contentMap: map[string]any{
				"type": "text",
				"text": "Hello, world!",
			},
			expected: TextContent{
				Type: ContentTypeText,
				Text: "Hello, world!",
			},
			expectError: false,
		},
		{
			name: "image content with annotations",
			contentMap: map[string]any{
				"type":     "image",
				"data":     "base64data",
				"mimeType": "image/png",
				"annotations": map[string]any{
					"priority": 2.0,
				},
			},
			expected: ImageContent{
				Type:     ContentTypeImage,
				Data:     "base64data",
				MIMEType: "image/png",
				Annotated: Annotated{
					Annotations: &Annotations{
						Priority: 2.0,
					},
				},
			},
			expectError: false,
		},
		{
			name: "audio content with annotations",
			contentMap: map[string]any{
				"type":     "audio",
				"data":     "base64data",
				"mimeType": "audio/mp3",
				"annotations": map[string]any{
					"audience": []any{"assistant"},
				},
			},
			expected: AudioContent{
				Type:     ContentTypeAudio,
				Data:     "base64data",
				MIMEType: "audio/mp3",
				Annotated: Annotated{
					Annotations: &Annotations{
						Audience: []Role{"assistant"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "resource link with annotations",
			contentMap: map[string]any{
				"type":        "resource_link",
				"uri":         "file:///test.txt",
				"name":        "Test File",
				"description": "A test file",
				"mimeType":    "text/plain",
				"annotations": map[string]any{
					"priority": 1.0,
				},
			},
			expected: ResourceLink{
				Type:        ContentTypeLink,
				URI:         "file:///test.txt",
				Name:        "Test File",
				Description: "A test file",
				MIMEType:    "text/plain",
				Annotated: Annotated{
					Annotations: &Annotations{
						Priority: 1.0,
					},
				},
			},
			expectError: false,
		},
		{
			name: "embedded resource with annotations",
			contentMap: map[string]any{
				"type": "resource",
				"resource": map[string]any{
					"uri":      "file:///test.txt",
					"mimeType": "text/plain",
					"text":     "Hello, world!",
				},
				"annotations": map[string]any{
					"audience": []any{"user", "assistant"},
				},
			},
			expected: EmbeddedResource{
				Type: ContentTypeResource,
				Resource: TextResourceContents{
					URI:      "file:///test.txt",
					MIMEType: "text/plain",
					Text:     "Hello, world!",
				},
				Annotated: Annotated{
					Annotations: &Annotations{
						Audience: []Role{"user", "assistant"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "missing type",
			contentMap: map[string]any{
				"text": "Hello, world!",
			},
			expected:    nil,
			expectError: true,
		},
		{
			name: "unsupported type",
			contentMap: map[string]any{
				"type": "unsupported",
			},
			expected:    nil,
			expectError: true,
		},
		{
			name: "text content missing text field",
			contentMap: map[string]any{
				"type": "text",
			},
			expected:    TextContent{Type: ContentTypeText, Text: ""},
			expectError: false,
		},
		{
			name: "image content missing data",
			contentMap: map[string]any{
				"type":     "image",
				"mimeType": "image/png",
			},
			expected:    nil,
			expectError: true,
		},
		{
			name: "audio content missing mimeType",
			contentMap: map[string]any{
				"type": "audio",
				"data": "base64data",
			},
			expected:    nil,
			expectError: true,
		},
		{
			name: "resource link missing uri",
			contentMap: map[string]any{
				"type": "resource_link",
				"name": "Test File",
			},
			expected:    nil,
			expectError: true,
		},
		{
			name: "resource link missing name",
			contentMap: map[string]any{
				"type": "resource_link",
				"uri":  "file:///test.txt",
			},
			expected:    nil,
			expectError: true,
		},
		{
			name: "embedded resource missing resource",
			contentMap: map[string]any{
				"type": "resource",
			},
			expected:    nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseContent(tt.contentMap)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)

				// Compare the actual content values
				switch exp := tt.expected.(type) {
				case TextContent:
					act, ok := result.(TextContent)
					assert.True(t, ok)
					assert.Equal(t, exp.Type, act.Type)
					assert.Equal(t, exp.Text, act.Text)
					assert.Equal(t, exp.Annotations, act.Annotations)
				case ImageContent:
					act, ok := result.(ImageContent)
					assert.True(t, ok)
					assert.Equal(t, exp.Type, act.Type)
					assert.Equal(t, exp.Data, act.Data)
					assert.Equal(t, exp.MIMEType, act.MIMEType)
					assert.Equal(t, exp.Annotations, act.Annotations)
				case AudioContent:
					act, ok := result.(AudioContent)
					assert.True(t, ok)
					assert.Equal(t, exp.Type, act.Type)
					assert.Equal(t, exp.Data, act.Data)
					assert.Equal(t, exp.MIMEType, act.MIMEType)
					assert.Equal(t, exp.Annotations, act.Annotations)
				case ResourceLink:
					act, ok := result.(ResourceLink)
					assert.True(t, ok)
					assert.Equal(t, exp.Type, act.Type)
					assert.Equal(t, exp.URI, act.URI)
					assert.Equal(t, exp.Name, act.Name)
					assert.Equal(t, exp.Description, act.Description)
					assert.Equal(t, exp.MIMEType, act.MIMEType)
					assert.Equal(t, exp.Annotations, act.Annotations)
				case EmbeddedResource:
					act, ok := result.(EmbeddedResource)
					assert.True(t, ok)
					assert.Equal(t, exp.Type, act.Type)
					assert.Equal(t, exp.Resource, act.Resource)
					assert.Equal(t, exp.Annotations, act.Annotations)
				default:
					assert.Equal(t, tt.expected, result)
				}
			}
		})
	}
}

func TestNewJSONRPCResultResponse(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		id     RequestId
		result any
		want   JSONRPCResponse
	}{
		"string result": {
			id:     NewRequestId(1),
			result: "test result",
			want: JSONRPCResponse{
				JSONRPC: JSONRPC_VERSION,
				ID:      NewRequestId(1),
				Result:  "test result",
			},
		},
		"map result": {
			id:     NewRequestId("test-id"),
			result: map[string]any{"key": "value"},
			want: JSONRPCResponse{
				JSONRPC: JSONRPC_VERSION,
				ID:      NewRequestId("test-id"),
				Result:  map[string]any{"key": "value"},
			},
		},
		"nil result": {
			id:     NewRequestId(42),
			result: nil,
			want: JSONRPCResponse{
				JSONRPC: JSONRPC_VERSION,
				ID:      NewRequestId(42),
				Result:  nil,
			},
		},
		"struct result": {
			id:     NewRequestId(0),
			result: struct{ Name string }{Name: "test"},
			want: JSONRPCResponse{
				JSONRPC: JSONRPC_VERSION,
				ID:      NewRequestId(0),
				Result:  struct{ Name string }{Name: "test"},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := NewJSONRPCResultResponse(tc.id, tc.result)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestNewJSONRPCResponse(t *testing.T) {
	t.Parallel()

	// Test the existing constructor that takes Result struct
	id := NewRequestId(1)
	result := Result{Meta: &Meta{}}

	got := NewJSONRPCResponse(id, result)
	want := JSONRPCResponse{
		JSONRPC: JSONRPC_VERSION,
		ID:      id,
		Result:  result,
	}

	require.Equal(t, want, got)
}
