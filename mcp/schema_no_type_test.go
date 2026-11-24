package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolSchemaWithoutTypeRoundTrip(t *testing.T) {
	const object = `{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`
	tests := []struct {
		name    string
		schema  string
		valid   any
		invalid any
	}{
		{name: "allOf", schema: `{"allOf":[` + object + `]}`, valid: map[string]any{"value": "hello"}, invalid: map[string]any{"value": 7}},
		{name: "reference", schema: `{"$ref":"#/$defs/value","$defs":{"value":` + object + `}}`, valid: map[string]any{"value": "hello"}, invalid: map[string]any{"value": 7}},
		{name: "additional properties", schema: `{"additionalProperties":false}`, valid: map[string]any{}, invalid: map[string]any{"value": "hello"}},
		{name: "required", schema: `{"required":["value"]}`, valid: map[string]any{"value": "hello"}, invalid: map[string]any{}},
		{name: "properties", schema: `{"properties":{"value":{"type":"string"}}}`, valid: map[string]any{"value": "hello"}, invalid: map[string]any{"value": 7}},
		{name: "explicit type control", schema: object, valid: map[string]any{"value": "hello"}, invalid: map[string]any{"value": 7}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := compileRoundTripSchema(t, []byte(tt.schema))
			require.NoError(t, original.Validate(tt.valid))
			require.Error(t, original.Validate(tt.invalid))
			before := decodeRoundTripJSON(t, tt.schema).(map[string]any)
			for name, encoded := range roundTripToolSchemas(t, tt.schema) {
				t.Run(name, func(t *testing.T) {
					require.NotEmpty(t, encoded, "a schema without a top-level type must not be dropped")
					wire := decodeRoundTripJSON(t, string(encoded)).(map[string]any)
					if value, ok := before["type"]; ok {
						assert.Equal(t, value, wire["type"])
					} else {
						assert.NotContains(t, wire, "type", "an absent type must not become an empty string")
					}
					compiled := compileRoundTripSchema(t, encoded)
					assert.NoError(t, compiled.Validate(tt.valid))
					assert.Error(t, compiled.Validate(tt.invalid))
				})
			}
		})
	}
}
