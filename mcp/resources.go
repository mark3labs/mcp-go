package mcp

import (
	"time"

	"github.com/yosida95/uritemplate/v3"
)

// ResourceOption is a function that configures a Resource.
// It provides a flexible way to set various properties of a Resource using the functional options pattern.
type ResourceOption func(*Resource)

// NewResource creates a new Resource with the given URI, name and options.
// The resource will be configured based on the provided options.
// Options are applied in order, allowing for flexible resource configuration.
func NewResource(uri string, name string, opts ...ResourceOption) Resource {
	resource := Resource{
		URI:  uri,
		Name: name,
	}

	for _, opt := range opts {
		opt(&resource)
	}

	return resource
}

// WithResourceDescription adds a description to the Resource.
// The description should provide a clear, human-readable explanation of what the resource represents.
func WithResourceDescription(description string) ResourceOption {
	return func(r *Resource) {
		r.Description = description
	}
}

// WithMIMEType sets the MIME type for the Resource.
// This should indicate the format of the resource's contents.
func WithMIMEType(mimeType string) ResourceOption {
	return func(r *Resource) {
		r.MIMEType = mimeType
	}
}

// WithAnnotations adds annotations to the Resource.
// Annotations can provide additional metadata about the resource's intended use.
func WithAnnotations(audience []Role, priority float64, lastModified string) ResourceOption {
	return func(r *Resource) {
		if r.Annotations == nil {
			r.Annotations = &Annotations{}
		}
		r.Annotations.Audience = audience
		r.Annotations.Priority = &priority
		r.Annotations.LastModified = lastModified
	}
}

// WithLastModified adds a last modified timestamp to the Resource.
// The timestamp should be in ISO 8601 format (e.g., "2025-01-12T15:00:58Z").
// Callers should use ValidateISO8601Timestamp to validate the timestamp before use.
func WithLastModified(timestamp string) ResourceOption {
	return func(r *Resource) {
		if r.Annotations == nil {
			r.Annotations = &Annotations{}
		}
		r.Annotations.LastModified = timestamp
	}
}

// ResourceTemplateOption is a function that configures a ResourceTemplate.
// It provides a flexible way to set various properties of a ResourceTemplate using the functional options pattern.
type ResourceTemplateOption func(*ResourceTemplate)

// NewResourceTemplate creates a new ResourceTemplate with the given URI template, name and options.
// The template will be configured based on the provided options.
// Options are applied in order, allowing for flexible template configuration.
func NewResourceTemplate(uriTemplate string, name string, opts ...ResourceTemplateOption) ResourceTemplate {
	template := ResourceTemplate{
		URITemplate: &URITemplate{Template: uritemplate.MustNew(uriTemplate)},
		Name:        name,
	}

	for _, opt := range opts {
		opt(&template)
	}

	return template
}

// WithTemplateDescription adds a description to the ResourceTemplate.
// The description should provide a clear, human-readable explanation of what resources this template represents.
func WithTemplateDescription(description string) ResourceTemplateOption {
	return func(t *ResourceTemplate) {
		t.Description = description
	}
}

// WithTemplateMIMEType sets the MIME type for the ResourceTemplate.
// This should only be set if all resources matching this template will have the same type.
func WithTemplateMIMEType(mimeType string) ResourceTemplateOption {
	return func(t *ResourceTemplate) {
		t.MIMEType = mimeType
	}
}

// WithTemplateAnnotations adds annotations to the ResourceTemplate.
// Annotations can provide additional metadata about the template's intended use.
func WithTemplateAnnotations(audience []Role, priority float64, lastModified string) ResourceTemplateOption {
	return func(t *ResourceTemplate) {
		if t.Annotations == nil {
			t.Annotations = &Annotations{}
		}
		t.Annotations.Audience = audience
		t.Annotations.Priority = &priority
		t.Annotations.LastModified = lastModified
	}
}

// ValidateISO8601Timestamp checks if a string is a valid ISO 8601 timestamp
func ValidateISO8601Timestamp(timestamp string) error {
	if timestamp == "" {
		return nil // Empty is valid (optional field)
	}
	// Use time.RFC3339 for ISO 8601 compatibility
	_, err := time.Parse(time.RFC3339, timestamp)
	return err
}

// WithResourceIcons adds icons to the Resource.
// Icons provide visual identifiers for the resource.
func WithResourceIcons(icons ...Icon) ResourceOption {
	return func(r *Resource) {
		r.Icons = icons
	}
}

// WithTemplateIcons adds icons to the ResourceTemplate.
// Icons provide visual identifiers for the resource template.
func WithTemplateIcons(icons ...Icon) ResourceTemplateOption {
	return func(rt *ResourceTemplate) {
		rt.Icons = icons
	}
}
