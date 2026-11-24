package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestToolSchemaWithoutTypeValidation(t *testing.T) {
	const object = `{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`
	schemas := []struct {
		name    string
		schema  string
		valid   any
		invalid any
	}{
		{name: "allOf", schema: `{"allOf":[` + object + `]}`, valid: map[string]any{"value": "hello"}, invalid: map[string]any{"value": 7}},
		{name: "reference", schema: `{"$ref":"#/$defs/value","$defs":{"value":` + object + `}}`, valid: map[string]any{"value": "hello"}, invalid: map[string]any{"value": 7}},
		{name: "additional properties", schema: `{"additionalProperties":false}`, valid: map[string]any{}, invalid: map[string]any{"value": "hello"}},
		{name: "explicit type control", schema: object, valid: map[string]any{"value": "hello"}, invalid: map[string]any{"value": 7}},
	}
	for _, schema := range schemas {
		t.Run(schema.name, func(t *testing.T) {
			tests := []struct {
				name    string
				input   any
				output  any
				wantErr string
				called  bool
			}{
				{name: "valid input and output", input: schema.valid, output: schema.valid, called: true},
				{name: "invalid input", input: schema.invalid, output: schema.valid, wantErr: "input schema validation failed"},
				{name: "invalid output", input: schema.valid, output: schema.invalid, wantErr: "output schema validation failed", called: true},
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					var tool mcp.Tool
					require.NoError(t, json.Unmarshal([]byte(`{"name":"no_type","inputSchema":`+schema.schema+`,"outputSchema":`+schema.schema+`}`), &tool))
					srv := NewMCPServer("no_type", "1.0", WithInputSchemaValidation(), WithOutputSchemaValidation())
					called := false
					srv.AddTool(tool, func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						called = true
						return mcp.NewToolResultStructuredOnly(tt.output), nil
					})
					response := callTool(t, srv, tool.Name, tt.input)
					if tt.wantErr == "" {
						requireToolSuccess(t, response)
					} else {
						requireToolErrorContaining(t, response, tt.wantErr)
					}
					assert.Equal(t, tt.called, called)
				})
			}
		})
	}
}
