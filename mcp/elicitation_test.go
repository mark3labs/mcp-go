package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestElicitationParamsSerialization(t *testing.T) {
	tests := []struct {
		name     string
		params   ElicitationParams
		expected string
	}{
		{
			name: "Form Mode Default",
			params: ElicitationParams{
				Message: "Please enter data",
				RequestedSchema: map[string]any{
					"type": "string",
				},
			},
			expected: `{"message":"Please enter data","requestedSchema":{"type":"string"}}`,
		},
		{
			name: "Form Mode Explicit",
			params: ElicitationParams{
				Mode:    ElicitationModeForm,
				Message: "Please enter data",
				RequestedSchema: map[string]any{
					"type": "string",
				},
			},
			expected: `{"mode":"form","message":"Please enter data","requestedSchema":{"type":"string"}}`,
		},
		{
			name: "URL Mode",
			params: ElicitationParams{
				Mode:          ElicitationModeURL,
				Message:       "Please auth",
				ElicitationID: "123",
				URL:           "https://example.com/auth",
			},
			expected: `{"mode":"url","message":"Please auth","elicitationId":"123","url":"https://example.com/auth"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.params)
			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(data))

			// Round trip
			var decoded ElicitationParams
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)

			assert.Equal(t, tt.params.Message, decoded.Message)
			assert.Equal(t, tt.params.Mode, decoded.Mode)

			if tt.params.Mode == ElicitationModeURL {
				assert.Equal(t, tt.params.ElicitationID, decoded.ElicitationID)
				assert.Equal(t, tt.params.URL, decoded.URL)
			}
		})
	}
}

func TestElicitationCapabilitySerialization(t *testing.T) {
	// Test empty struct for backward compatibility
	cap := ElicitationCapability{}
	data, err := json.Marshal(cap)
	require.NoError(t, err)
	assert.JSONEq(t, "{}", string(data))

	// Test with Form support
	cap = ElicitationCapability{
		Form: &struct{}{},
	}
	data, err = json.Marshal(cap)
	require.NoError(t, err)
	assert.JSONEq(t, `{"form":{}}`, string(data))

	// Test with URL support
	cap = ElicitationCapability{
		URL: &struct{}{},
	}
	data, err = json.Marshal(cap)
	require.NoError(t, err)
	assert.JSONEq(t, `{"url":{}}`, string(data))
}
