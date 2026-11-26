package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestSchemaJSONForMatchesWireEncoding checks that validation and tool listing
// use the same structured schema, including custom values and property order.
func TestSchemaJSONForMatchesWireEncoding(t *testing.T) {
	var schema mcp.ToolInputSchema
	require.NoError(t, json.Unmarshal([]byte(`{
		"type":["object","null"],
		"properties":{"z":{"type":"number"},"a":{"type":"string"}},
		"description":"<input> & output",
		"additionalProperties":false
	}`), &schema))
	schema.Properties["z"].(map[string]any)["maximum"] = json.Number("9007199254740993")
	schema.AdditionalFields["examples"] = json.RawMessage(`[ { "a": "<example>" } ]`)
	tool := mcp.Tool{Name: "validation", InputSchema: schema}

	wire, err := json.Marshal(tool)
	require.NoError(t, err)
	var listing struct {
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	require.NoError(t, json.Unmarshal(wire, &listing))
	encoded, ok := schemaJSONFor(tool)
	require.True(t, ok)
	require.Equal(t, listing.InputSchema, json.RawMessage(encoded))
	require.Contains(t, string(encoded), `"maximum":9007199254740993`)
}

// TestSchemaJSONForRejectsExcessiveNesting preserves encoding/json's depth check.
func TestSchemaJSONForRejectsExcessiveNesting(t *testing.T) {
	var example any = "leaf"
	for range 10_001 {
		example = []any{example}
	}
	tool := mcp.NewTool("validation")
	tool.InputSchema.AdditionalFields = map[string]any{"examples": example}
	_, err := json.Marshal(tool.InputSchema)
	require.Error(t, err)
	_, ok := schemaJSONFor(tool)
	require.False(t, ok)
}
