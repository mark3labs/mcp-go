package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseTaskResultPreservesStructuredJSONPrecision(t *testing.T) {
	raw := json.RawMessage(`{"structuredContent":{"id":9007199254740993,"fraction":1.234567890123456789},"content":[]}`)
	result, err := ParseTaskResultResult(&raw)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &got))
	require.JSONEq(t, `{"id":9007199254740993,"fraction":1.234567890123456789}`, string(got["structuredContent"]))
	require.Contains(t, string(got["structuredContent"]), "9007199254740993")
	require.Contains(t, string(got["structuredContent"]), "1.234567890123456789")
}
