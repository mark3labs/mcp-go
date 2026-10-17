package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewResource(t *testing.T) {
	tests := []struct {
		name     string
		resource Resource
		expected Resource
	}{
		{
			name:     "basic resource",
			resource: NewResource("file:///test.txt", "test.txt"),
			expected: Resource{
				URI:  "file:///test.txt",
				Name: "test.txt",
			},
		},
		{
			name: "resource with description",
			resource: NewResource("file:///doc.md", "doc.md",
				WithResourceDescription("A markdown document")),
			expected: Resource{
				URI:         "file:///doc.md",
				Name:        "doc.md",
				Description: "A markdown document",
			},
		},
		{
			name: "resource with MIME type",
			resource: NewResource("file:///image.png", "image.png",
				WithMIMEType("image/png")),
			expected: Resource{
				URI:      "file:///image.png",
				Name:     "image.png",
				MIMEType: "image/png",
			},
		},
		{
			name: "resource with annotations",
			resource: NewResource("file:///data.json", "data.json",
				WithAnnotations([]Role{RoleUser, RoleAssistant}, 0.5, "")),
			expected: Resource{
				URI:  "file:///data.json",
				Name: "data.json",
				Annotated: Annotated{
					Annotations: &Annotations{
						Audience: []Role{RoleUser, RoleAssistant},
						Priority: ptr(0.5),
					},
				},
			},
		},
		{
			name: "resource with all options",
			resource: NewResource("file:///complete.txt", "complete.txt",
				WithResourceDescription("Complete resource"),
				WithMIMEType("text/plain"),
				WithAnnotations([]Role{RoleUser}, 1.0, "")),
			expected: Resource{
				URI:         "file:///complete.txt",
				Name:        "complete.txt",
				Description: "Complete resource",
				MIMEType:    "text/plain",
				Annotated: Annotated{
					Annotations: &Annotations{
						Audience: []Role{RoleUser},
						Priority: ptr(1.0),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected.URI, tt.resource.URI)
			assert.Equal(t, tt.expected.Name, tt.resource.Name)
			assert.Equal(t, tt.expected.Description, tt.resource.Description)
			assert.Equal(t, tt.expected.MIMEType, tt.resource.MIMEType)
			assert.Equal(t, tt.expected.Annotations, tt.resource.Annotations)
		})
	}
}

func TestNewResourceTemplate(t *testing.T) {
	tests := []struct {
		name     string
		template ResourceTemplate
		validate func(t *testing.T, template ResourceTemplate)
	}{
		{
			name:     "basic template",
			template: NewResourceTemplate("file:///{path}", "files"),
			validate: func(t *testing.T, template ResourceTemplate) {
				assert.NotNil(t, template.URITemplate)
				assert.Equal(t, "files", template.Name)
			},
		},
		{
			name: "template with description",
			template: NewResourceTemplate("file:///{dir}/{file}", "directory-files",
				WithTemplateDescription("Files in directories")),
			validate: func(t *testing.T, template ResourceTemplate) {
				assert.Equal(t, "directory-files", template.Name)
				assert.Equal(t, "Files in directories", template.Description)
			},
		},
		{
			name: "template with MIME type",
			template: NewResourceTemplate("file:///{name}.txt", "text-files",
				WithTemplateMIMEType("text/plain")),
			validate: func(t *testing.T, template ResourceTemplate) {
				assert.Equal(t, "text-files", template.Name)
				assert.Equal(t, "text/plain", template.MIMEType)
			},
		},
		{
			name: "template with annotations",
			template: NewResourceTemplate("file:///{id}", "resources",
				WithTemplateAnnotations([]Role{RoleUser}, 1.0, "")),
			validate: func(t *testing.T, template ResourceTemplate) {
				assert.Equal(t, "resources", template.Name)
				require.NotNil(t, template.Annotations)
				assert.Equal(t, []Role{RoleUser}, template.Annotations.Audience)
				assert.Equal(t, 1.0, *template.Annotations.Priority)
			},
		},
		{
			name: "template with all options",
			template: NewResourceTemplate("api:///{version}/{resource}", "api-resources",
				WithTemplateDescription("API resources"),
				WithTemplateMIMEType("application/json"),
				WithTemplateAnnotations([]Role{RoleUser, RoleAssistant}, 0.8, "")),
			validate: func(t *testing.T, template ResourceTemplate) {
				assert.Equal(t, "api-resources", template.Name)
				assert.Equal(t, "API resources", template.Description)
				assert.Equal(t, "application/json", template.MIMEType)
				require.NotNil(t, template.Annotations)
				assert.Equal(t, []Role{RoleUser, RoleAssistant}, template.Annotations.Audience)
				assert.Equal(t, 0.8, *template.Annotations.Priority)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.validate(t, tt.template)
		})
	}
}

func TestWithResourceDescription(t *testing.T) {
	resource := Resource{}
	opt := WithResourceDescription("Test resource")
	opt(&resource)

	assert.Equal(t, "Test resource", resource.Description)
}

func TestWithMIMEType(t *testing.T) {
	resource := Resource{}
	opt := WithMIMEType("application/json")
	opt(&resource)

	assert.Equal(t, "application/json", resource.MIMEType)
}

func TestWithAnnotations(t *testing.T) {
	tests := []struct {
		name     string
		audience []Role
		priority float64
	}{
		{
			name:     "user audience",
			audience: []Role{RoleUser},
			priority: 1.0,
		},
		{
			name:     "multiple audiences",
			audience: []Role{RoleUser, RoleAssistant},
			priority: 0.8,
		},
		{
			name:     "empty audience",
			audience: []Role{},
			priority: 0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := Resource{}
			opt := WithAnnotations(tt.audience, tt.priority, "")
			opt(&resource)

			require.NotNil(t, resource.Annotations)
			assert.Equal(t, tt.audience, resource.Annotations.Audience)
			assert.Equal(t, tt.priority, *resource.Annotations.Priority)
		})
	}
}

func TestWithTemplateDescription(t *testing.T) {
	template := ResourceTemplate{}
	opt := WithTemplateDescription("Test template")
	opt(&template)

	assert.Equal(t, "Test template", template.Description)
}

func TestWithTemplateMIMEType(t *testing.T) {
	template := ResourceTemplate{}
	opt := WithTemplateMIMEType("text/html")
	opt(&template)

	assert.Equal(t, "text/html", template.MIMEType)
}

func TestWithTemplateAnnotations(t *testing.T) {
	tests := []struct {
		name     string
		audience []Role
		priority float64
	}{
		{
			name:     "assistant audience",
			audience: []Role{RoleAssistant},
			priority: 1.0,
		},
		{
			name:     "both audiences",
			audience: []Role{RoleUser, RoleAssistant},
			priority: 0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			template := ResourceTemplate{}
			opt := WithTemplateAnnotations(tt.audience, tt.priority, "")
			opt(&template)

			require.NotNil(t, template.Annotations)
			assert.Equal(t, tt.audience, template.Annotations.Audience)
			assert.Equal(t, tt.priority, *template.Annotations.Priority)
		})
	}
}

func TestResourceJSONMarshaling(t *testing.T) {
	resource := NewResource("file:///test.txt", "test.txt",
		WithResourceDescription("Test file"),
		WithMIMEType("text/plain"),
		WithAnnotations([]Role{RoleUser}, 1.0, "2025-01-01T12:00:00Z"))

	data, err := json.Marshal(resource)
	require.NoError(t, err)

	var unmarshaled Resource
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, resource.URI, unmarshaled.URI)
	assert.Equal(t, resource.Name, unmarshaled.Name)
	assert.Equal(t, resource.Description, unmarshaled.Description)
	assert.Equal(t, resource.MIMEType, unmarshaled.MIMEType)
	require.NotNil(t, unmarshaled.Annotations)
	assert.Equal(t, "2025-01-01T12:00:00Z", resource.Annotations.LastModified)
	assert.Equal(t, resource.Annotations.LastModified, unmarshaled.Annotations.LastModified)
}

func TestResourceTemplateJSONMarshaling(t *testing.T) {
	template := NewResourceTemplate("file:///{path}", "files",
		WithTemplateDescription("File resources"),
		WithTemplateMIMEType("text/plain"))

	data, err := json.Marshal(template)
	require.NoError(t, err)

	var unmarshaled ResourceTemplate
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, template.Name, unmarshaled.Name)
	assert.Equal(t, template.Description, unmarshaled.Description)
	assert.Equal(t, template.MIMEType, unmarshaled.MIMEType)
	assert.NotNil(t, unmarshaled.URITemplate)
}

func TestAnnotationsCreationFromNil(t *testing.T) {
	// Test that annotations are created when nil
	resource := Resource{}
	opt := WithAnnotations([]Role{RoleUser}, 1.0, "")
	opt(&resource)

	require.NotNil(t, resource.Annotations)
	assert.Equal(t, []Role{RoleUser}, resource.Annotations.Audience)
	assert.Equal(t, 1.0, *resource.Annotations.Priority)
}

func TestTemplateAnnotationsCreationFromNil(t *testing.T) {
	// Test that annotations are created when nil
	template := ResourceTemplate{}
	opt := WithTemplateAnnotations([]Role{RoleAssistant}, 0.5, "")
	opt(&template)

	require.NotNil(t, template.Annotations)
	assert.Equal(t, []Role{RoleAssistant}, template.Annotations.Audience)
	assert.Equal(t, 0.5, *template.Annotations.Priority)
}

func ptr(v float64) *float64 { return &v }

func TestWithResourceIcons(t *testing.T) {
	resource := Resource{}
	icons := []Icon{
		{Src: "icon1.png", MIMEType: "image/png"},
		{Src: "icon2.svg", Sizes: []string{"any"}},
	}
	opt := WithResourceIcons(icons...)
	opt(&resource)

	assert.Equal(t, icons, resource.Icons)
}

func TestWithTemplateIcons(t *testing.T) {
	template := ResourceTemplate{}
	icons := []Icon{
		{Src: "template-icon.png"},
	}
	opt := WithTemplateIcons(icons...)
	opt(&template)

	assert.Equal(t, icons, template.Icons)
}

func TestValidateISO8601Timestamp(t *testing.T) {
	tests := []struct {
		name      string
		timestamp string
		wantErr   bool
	}{
		{
			name:      "valid timestamp Z",
			timestamp: "2025-01-12T15:00:58Z",
			wantErr:   false,
		},
		{
			name:      "valid timestamp offset",
			timestamp: "2025-01-12T15:00:58+05:30",
			wantErr:   false,
		},
		{
			name:      "empty timestamp",
			timestamp: "",
			wantErr:   false,
		},
		{
			name:      "invalid format",
			timestamp: "2025/01/12 15:00:58",
			wantErr:   true,
		},
		{
			name:      "invalid date",
			timestamp: "not-a-date",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateISO8601Timestamp(tt.timestamp)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestWithLastModified(t *testing.T) {
	resource := Resource{}
	timestamp := "2025-01-12T15:00:58Z"
	opt := WithLastModified(timestamp)
	opt(&resource)

	require.NotNil(t, resource.Annotations)
	assert.Equal(t, timestamp, resource.Annotations.LastModified)
}

func TestWithAnnotationsIncludingLastModified(t *testing.T) {
	resource := Resource{}
	timestamp := "2025-01-12T15:00:58Z"
	opt := WithAnnotations([]Role{RoleUser}, 1.0, timestamp)
	opt(&resource)

	require.NotNil(t, resource.Annotations)
	assert.Equal(t, timestamp, resource.Annotations.LastModified)
	assert.Equal(t, 1.0, *resource.Annotations.Priority)
}

func TestWithAnnotationsAndLastModifiedCombined(t *testing.T) {
	t.Run("WithAnnotations then WithLastModified", func(t *testing.T) {
		ts1 := "2025-01-01T00:00:00Z"
		ts2 := "2025-01-02T00:00:00Z"

		// Apply WithAnnotations first, then WithLastModified
		resource := NewResource("file:///test", "test",
			WithAnnotations([]Role{RoleUser}, 1.0, ts1),
			WithLastModified(ts2),
		)

		require.NotNil(t, resource.Annotations)
		assert.Equal(t, ts2, resource.Annotations.LastModified, "WithLastModified should overwrite timestamp")
		assert.Equal(t, 1.0, *resource.Annotations.Priority, "Priority should remain")
	})

	t.Run("WithLastModified then WithAnnotations", func(t *testing.T) {
		resource := Resource{}
		ts1 := "2025-01-01T00:00:00Z"
		ts2 := "2025-01-02T00:00:00Z"

		opt1 := WithLastModified(ts1)
		opt2 := WithAnnotations([]Role{RoleUser}, 1.0, ts2)
		opt1(&resource)
		opt2(&resource)

		require.NotNil(t, resource.Annotations)
		assert.Equal(t, ts2, resource.Annotations.LastModified, "WithAnnotations should overwrite timestamp")
		assert.Equal(t, 1.0, *resource.Annotations.Priority)
	})
}

func TestWithTemplateAnnotationsIncludingLastModified(t *testing.T) {
	template := ResourceTemplate{}
	timestamp := "2025-01-12T15:00:58Z"
	opt := WithTemplateAnnotations([]Role{RoleAssistant}, 0.5, timestamp)
	opt(&template)

	require.NotNil(t, template.Annotations)
	assert.Equal(t, timestamp, template.Annotations.LastModified)
	assert.Equal(t, 0.5, *template.Annotations.Priority)
}
