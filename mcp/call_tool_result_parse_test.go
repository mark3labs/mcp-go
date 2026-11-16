package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCallToolResult_ContentAlternatives(t *testing.T) {
	tests := []struct {
		name           string
		raw            string
		wantErr        string
		wantStructured any
		wantContent    int
	}{
		{name: "content", raw: `{"content":[{"type":"text","text":"ok"}]}`, wantContent: 1},
		{name: "structured content only", raw: `{"structuredContent":{"data":"structured only"}}`, wantStructured: map[string]any{"data": "structured only"}},
		{name: "structured content array", raw: `{"structuredContent":[1,2]}`, wantStructured: []any{float64(1), float64(2)}},
		{name: "input required", raw: `{"resultType":"input_required","inputRequests":{},"requestState":"s"}`},
		{name: "null structured content", raw: `{"structuredContent":null}`, wantErr: "content is missing"},
		{name: "null structured content with spaces", raw: `{"structuredContent": null }`, wantErr: "content is missing"},
		{name: "neither", raw: `{"isError":true}`, wantErr: "content is missing"},
		{name: "empty object", raw: `{}`, wantErr: "content is missing"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := json.RawMessage(tt.raw)
			result, err := ParseCallToolResult(&raw)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Len(t, result.Content, tt.wantContent)
			assert.Equal(t, tt.wantStructured, result.StructuredContent)
		})
	}
}
