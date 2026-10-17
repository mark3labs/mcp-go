package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResultParsersPreserveExplicitEmptyArrays(t *testing.T) {
	tests := []struct {
		name, raw string
		parse     func(*json.RawMessage) (any, error)
	}{
		{"prompt", `{"messages":[]}`, func(r *json.RawMessage) (any, error) { return ParseGetPromptResult(r) }},
		{"resource", `{"contents":[]}`, func(r *json.RawMessage) (any, error) { return ParseReadResourceResult(r) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := json.RawMessage(tt.raw)
			result, err := tt.parse(&raw)
			require.NoError(t, err)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.JSONEq(t, tt.raw, string(encoded))
		})
	}
}
