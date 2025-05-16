package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetaMarshalling(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		meta    *Meta
		expMeta *Meta
	}{
		{
			name:    "empty",
			json:    "{}",
			meta:    &Meta{},
			expMeta: &Meta{AdditionalFields: map[string]any{}},
		},
		{
			name:    "empty additional fields",
			json:    "{}",
			meta:    &Meta{AdditionalFields: map[string]any{}},
			expMeta: &Meta{AdditionalFields: map[string]any{}},
		},
		{
			name:    "string token only",
			json:    `{"progressToken":"123"}`,
			meta:    &Meta{ProgressToken: "123"},
			expMeta: &Meta{ProgressToken: "123", AdditionalFields: map[string]any{}},
		},
		{
			name:    "string token only, empty additional fields",
			json:    `{"progressToken":"123"}`,
			meta:    &Meta{ProgressToken: "123", AdditionalFields: map[string]any{}},
			expMeta: &Meta{ProgressToken: "123", AdditionalFields: map[string]any{}},
		},
		{
			name: "additional fields only",
			json: `{"a":2,"b":"1"}`,
			meta: &Meta{AdditionalFields: map[string]any{"a": 2, "b": "1"}},
			// For untyped map, numbers are always float64
			expMeta: &Meta{AdditionalFields: map[string]any{"a": float64(2), "b": "1"}},
		},
		{
			name: "progress token and additional fields",
			json: `{"a":2,"b":"1","progressToken":"123"}`,
			meta: &Meta{ProgressToken: "123", AdditionalFields: map[string]any{"a": 2, "b": "1"}},
			// For untyped map, numbers are always float64
			expMeta: &Meta{ProgressToken: "123", AdditionalFields: map[string]any{"a": float64(2), "b": "1"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.meta)
			require.NoError(t, err)
			assert.Equal(t, tc.json, string(data))

			meta := &Meta{}
			err = json.Unmarshal([]byte(tc.json), meta)
			require.NoError(t, err)
			assert.Equal(t, tc.expMeta, meta)
		})
	}
}
