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
				WithAnnotations([]Role{RoleUser, RoleAssistant}, 1.5)),
			expected: Resource{
				URI:  "file:///data.json",
				Name: "data.json",
				Annotated: Annotated{
					Annotations: &Annotations{
						Audience: []Role{RoleUser, RoleAssistant},
						Priority: 1.5,
					},
				},
			},
		},
		{
			name: "resource with all options",
			resource: NewResource("file:///complete.txt", "complete.txt",
				WithResourceDescription("Complete resource"),
				WithMIMEType("text/plain"),
				WithAnnotations([]Role{RoleUser}, 2.0)),
			expected: Resource{
				URI:         "file:///complete.txt",
				Name:        "complete.txt",
				Description: "Complete resource",
				MIMEType:    "text/plain",
				Annotated: Annotated{
					Annotations: &Annotations{
						Audience: []Role{RoleUser},
						Priority: 2.0,
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
				WithTemplateAnnotations([]Role{RoleUser}, 1.0)),
			validate: func(t *testing.T, template ResourceTemplate) {
				assert.Equal(t, "resources", template.Name)
				require.NotNil(t, template.Annotations)
				assert.Equal(t, []Role{RoleUser}, template.Annotations.Audience)
				assert.Equal(t, 1.0, template.Annotations.Priority)
			},
		},
		{
			name: "template with all options",
			template: NewResourceTemplate("api:///{version}/{resource}", "api-resources",
				WithTemplateDescription("API resources"),
				WithTemplateMIMEType("application/json"),
				WithTemplateAnnotations([]Role{RoleUser, RoleAssistant}, 2.5)),
			validate: func(t *testing.T, template ResourceTemplate) {
				assert.Equal(t, "api-resources", template.Name)
				assert.Equal(t, "API resources", template.Description)
				assert.Equal(t, "application/json", template.MIMEType)
				require.NotNil(t, template.Annotations)
				assert.Equal(t, []Role{RoleUser, RoleAssistant}, template.Annotations.Audience)
				assert.Equal(t, 2.5, template.Annotations.Priority)
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
			priority: 2.5,
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
			opt := WithAnnotations(tt.audience, tt.priority)
			opt(&resource)

			require.NotNil(t, resource.Annotations)
			assert.Equal(t, tt.audience, resource.Annotations.Audience)
			assert.Equal(t, tt.priority, resource.Annotations.Priority)
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
			priority: 3.0,
		},
		{
			name:     "both audiences",
			audience: []Role{RoleUser, RoleAssistant},
			priority: 1.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			template := ResourceTemplate{}
			opt := WithTemplateAnnotations(tt.audience, tt.priority)
			opt(&template)

			require.NotNil(t, template.Annotations)
			assert.Equal(t, tt.audience, template.Annotations.Audience)
			assert.Equal(t, tt.priority, template.Annotations.Priority)
		})
	}
}

func TestResourceJSONMarshaling(t *testing.T) {
	resource := NewResource("file:///test.txt", "test.txt",
		WithResourceDescription("Test file"),
		WithMIMEType("text/plain"),
		WithAnnotations([]Role{RoleUser}, 1.0))

	data, err := json.Marshal(resource)
	require.NoError(t, err)

	var unmarshaled Resource
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, resource.URI, unmarshaled.URI)
	assert.Equal(t, resource.Name, unmarshaled.Name)
	assert.Equal(t, resource.Description, unmarshaled.Description)
	assert.Equal(t, resource.MIMEType, unmarshaled.MIMEType)
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
	opt := WithAnnotations([]Role{RoleUser}, 1.0)
	opt(&resource)

	require.NotNil(t, resource.Annotations)
	assert.Equal(t, []Role{RoleUser}, resource.Annotations.Audience)
	assert.Equal(t, 1.0, resource.Annotations.Priority)
}

func TestTemplateAnnotationsCreationFromNil(t *testing.T) {
	// Test that annotations are created when nil
	template := ResourceTemplate{}
	opt := WithTemplateAnnotations([]Role{RoleAssistant}, 2.0)
	opt(&template)

	require.NotNil(t, template.Annotations)
	assert.Equal(t, []Role{RoleAssistant}, template.Annotations.Audience)
	assert.Equal(t, 2.0, template.Annotations.Priority)
}
