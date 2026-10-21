package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaTagFallbackPreservesArrayLength(t *testing.T) {
	type item struct {
		Mode string `json:"mode" jsonschema:"enum=fast,enum=safe"`
	}
	type request struct {
		Pair  [2]item `json:"pair"`
		Items []item  `json:"items"`
	}
	raw, err := SchemaForRaw[request]()
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(raw, &schema))
	pair := schema["properties"].(map[string]any)["pair"].(map[string]any)
	assert.Equal(t, float64(2), pair["minItems"])
	assert.Equal(t, float64(2), pair["maxItems"])
	items := schema["properties"].(map[string]any)["items"].(map[string]any)
	assert.NotContains(t, items, "minItems")
	assert.NotContains(t, items, "maxItems")
}
