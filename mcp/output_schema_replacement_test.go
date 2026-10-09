package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOutputSchemaOptionsReplacePreviousSchema(t *testing.T) {
	type oldOutput struct {
		Old string `json:"old"`
	}
	type newOutput struct {
		New string `json:"new,omitempty"`
	}
	cache := NewSchemaCache()
	tests := []struct {
		name   string
		first  ToolOption
		second ToolOption
	}{
		{"reflected", WithOutputSchema[oldOutput](), WithOutputSchema[newOutput]()},
		{"cached", WithCachedOutputSchema[oldOutput](cache), WithCachedOutputSchema[newOutput](cache)},
		{"nil cache", WithCachedOutputSchema[oldOutput](nil), WithCachedOutputSchema[newOutput](nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewTool("replace", tt.first, tt.second)
			want := NewTool("fresh", tt.second)
			assert.Equal(t, want.OutputSchema, got.OutputSchema)
		})
	}
}
