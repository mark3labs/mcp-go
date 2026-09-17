package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseTaskResultResult checks that the tasks/result payload is read from
// the top level of the response, not from a nested "result" key.
func TestParseTaskResultResult(t *testing.T) {
	tests := []struct {
		name           string
		raw            string
		wantText       string
		wantIsError    bool
		wantStructured any
		wantResultType ResultType
	}{
		{
			name:           "content and structuredContent at the top level",
			raw:            `{"_meta":{"io.modelcontextprotocol/related-task":{"taskId":"task-1"}},"resultType":"complete","content":[{"type":"text","text":"done"}],"structuredContent":{"rows":2},"isError":false}`,
			wantText:       "done",
			wantStructured: map[string]any{"rows": float64(2)},
			wantResultType: ResultTypeComplete,
		},
		{
			name:           "error result keeps isError",
			raw:            `{"resultType":"complete","content":[{"type":"text","text":"boom"}],"isError":true}`,
			wantText:       "boom",
			wantIsError:    true,
			wantResultType: ResultTypeComplete,
		},
		{
			name: "empty result stays empty",
			raw:  `{"_meta":{"io.modelcontextprotocol/related-task":{"taskId":"task-2"}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := json.RawMessage(tt.raw)
			result, err := ParseTaskResultResult(&raw)
			require.NoError(t, err)

			assert.Equal(t, tt.wantIsError, result.IsError)
			assert.Equal(t, tt.wantStructured, result.StructuredContent)
			assert.Equal(t, tt.wantResultType, result.ResultType)

			if tt.wantText == "" {
				assert.Empty(t, result.Content)
				return
			}
			require.Len(t, result.Content, 1)
			text, ok := result.Content[0].(TextContent)
			require.True(t, ok, "expected TextContent, got %T", result.Content[0])
			assert.Equal(t, tt.wantText, text.Text)
		})
	}
}

// TestParseTaskResultResultRoundTrip feeds the parser what the server actually
// marshals: TaskResultResult goes out as-is, so it has to come back intact.
func TestParseTaskResultResultRoundTrip(t *testing.T) {
	sent := TaskResultResult{
		Result:            Result{Meta: NewMetaFromMap(map[string]any{"trace": "abc"}), ResultType: ResultTypeComplete},
		Content:           []Content{TextContent{Type: "text", Text: "hello"}},
		StructuredContent: map[string]any{"ok": true},
		IsError:           true,
	}

	encoded, err := json.Marshal(sent)
	require.NoError(t, err)

	raw := json.RawMessage(encoded)
	got, err := ParseTaskResultResult(&raw)
	require.NoError(t, err)

	require.Len(t, got.Content, 1)
	assert.Equal(t, sent.Content[0], got.Content[0])
	assert.Equal(t, sent.StructuredContent, got.StructuredContent)
	assert.Equal(t, sent.IsError, got.IsError)
	assert.Equal(t, sent.ResultType, got.ResultType)
	assert.Equal(t, "abc", got.Meta.AdditionalFields["trace"])
}
