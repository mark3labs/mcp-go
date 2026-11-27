package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestURITemplateJSON(t *testing.T) {
	encodedZero, err := json.Marshal(URITemplate{})
	require.NoError(t, err)
	require.JSONEq(t, "null", string(encodedZero))
	template := NewResourceTemplate("file:///{name}", "file").URITemplate
	for _, value := range []any{template, *template} {
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		require.JSONEq(t, `"file:///{name}"`, string(encoded))
	}
	encoded, err := json.Marshal(map[string]URITemplate{"file": *template})
	require.NoError(t, err)
	require.JSONEq(t, `{"file":"file:///{name}"}`, string(encoded))
}

func TestURITemplateJSONRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		wire string
	}{
		{"null clears an existing template", "null"},
		{"empty string", `""`},
		{"template string", `"file:///{name}"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			template := NewResourceTemplate("old:///{id}", "old").URITemplate
			require.NoError(t, json.Unmarshal([]byte(tt.wire), template))
			encoded, err := json.Marshal(*template)
			require.NoError(t, err)
			require.JSONEq(t, tt.wire, string(encoded))
		})
	}
}
