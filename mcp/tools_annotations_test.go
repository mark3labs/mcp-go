package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolMarshalJSON_Annotations(t *testing.T) {
	tests := []struct {
		name            string
		tool            Tool
		wantAnnotations any
	}{
		{
			name: "NewTool keeps its default hints",
			tool: NewTool("t"),
			wantAnnotations: map[string]any{
				"readOnlyHint":    false,
				"destructiveHint": true,
				"idempotentHint":  false,
				"openWorldHint":   true,
			},
		},
		{
			name: "WithoutDefaultAnnotations omits the field",
			tool: NewTool("t", WithoutDefaultAnnotations()),
		},
		{
			name:            "hints set after WithoutDefaultAnnotations are the only ones sent",
			tool:            NewTool("t", WithoutDefaultAnnotations(), WithReadOnlyHintAnnotation(true)),
			wantAnnotations: map[string]any{"readOnlyHint": true},
		},
		{
			name:            "a title alone is sent",
			tool:            NewTool("t", WithoutDefaultAnnotations(), WithTitleAnnotation("Lookup")),
			wantAnnotations: map[string]any{"title": "Lookup"},
		},
		{
			name: "a struct literal without annotations omits the field",
			tool: Tool{Name: "t", InputSchema: ToolInputSchema{Type: "object"}},
		},
		{
			name: "a raw schema tool without annotations omits the field",
			tool: NewToolWithRawSchema("t", "", json.RawMessage(`{"type":"object"}`)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.tool)
			require.NoError(t, err)

			var m map[string]any
			require.NoError(t, json.Unmarshal(data, &m))

			got, present := m["annotations"]
			if tt.wantAnnotations == nil {
				assert.False(t, present, "annotations should be omitted, got %s", data)
				return
			}
			assert.Equal(t, tt.wantAnnotations, got)
		})
	}
}

func TestToolMarshalJSON_AnnotationsRoundTrip(t *testing.T) {
	var tool Tool
	require.NoError(t, json.Unmarshal([]byte(`{"name":"t","inputSchema":{"type":"object"}}`), &tool))
	assert.Equal(t, ToolAnnotation{}, tool.Annotations)

	data, err := json.Marshal(tool)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"annotations"`)

	require.NoError(t, json.Unmarshal([]byte(`{"name":"t","inputSchema":{"type":"object"},"annotations":{"openWorldHint":false}}`), &tool))
	data, err = json.Marshal(tool)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"annotations":{"openWorldHint":false}`)
}
