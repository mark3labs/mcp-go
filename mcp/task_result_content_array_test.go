package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskResultContentArray(t *testing.T) {
	tests := []struct {
		name    string
		content []Content
		want    string
	}{
		{"nil", nil, `[]`},
		{"empty", []Content{}, `[]`},
		{"nonempty", []Content{NewTextContent("done")}, `[{"type":"text","text":"done"}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TaskResultResult{Content: tt.content, RawStructuredContent: json.RawMessage(`{"id":9007199254740993}`), IsError: true}
			data, err := json.Marshal(result)
			require.NoError(t, err)
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(data, &fields))
			assert.JSONEq(t, tt.want, string(fields["content"]))
			assert.JSONEq(t, `{"id":9007199254740993}`, string(fields["structuredContent"]))
			assert.Equal(t, "true", string(fields["isError"]))
		})
	}
}

func TestParseTaskResultPreservesEmptyContentArray(t *testing.T) {
	raw := json.RawMessage(`{"content":[],"structuredContent":{"ok":true}}`)
	result, err := ParseTaskResultResult(&raw)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	assert.JSONEq(t, string(raw), string(encoded))
}
