package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallToolParams_RawArgumentsDoNotAliasInput(t *testing.T) {
	data := []byte(`{"name":"echo","arguments":{"text":"hello"}}`)

	var p CallToolParams
	require.NoError(t, json.Unmarshal(data, &p))

	for i := range data {
		data[i] = 'x'
	}

	assert.JSONEq(t, `{"text":"hello"}`, string(p.RawArguments))
	assert.Equal(t, map[string]any{"text": "hello"}, p.Arguments)
}

func TestCallToolParams_RawArgumentsResetOnReuse(t *testing.T) {
	var p CallToolParams
	require.NoError(t, json.Unmarshal([]byte(`{"name":"a","arguments":{"k":1}}`), &p))
	require.NoError(t, json.Unmarshal([]byte(`{"name":"b"}`), &p))

	assert.Nil(t, p.RawArguments)
	assert.Nil(t, p.Arguments)
}
