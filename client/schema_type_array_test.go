package client

import (
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListToolsSchemaTypeArrayHTTP checks discovery, output schema preservation,
// and subsequent tool calls through each listing API over a local HTTP server.
func TestListToolsSchemaTypeArrayHTTP(t *testing.T) {
	const listing = `[
		{"name":"get_me","inputSchema":{"type":"object"},"outputSchema":{"type":["object","null"]}},
		{"name":"plain","inputSchema":{"type":"object"},"outputSchema":{"type":"object"}}
	]`
	for _, method := range []string{"ListTools", "ListToolsByPage", "IterTools"} {
		t.Run(method, func(t *testing.T) {
			c, version := connectHeaderClient(t, &headerToolsServer{t: t, listings: []string{listing}}, nil)
			require.Equal(t, mcp.ProtocolVersion20260728, version)
			var tools []mcp.Tool
			switch method {
			case "ListTools":
				result, err := c.ListTools(t.Context(), mcp.ListToolsRequest{})
				require.NoError(t, err)
				tools = result.Tools
			case "ListToolsByPage":
				result, err := c.ListToolsByPage(t.Context(), mcp.ListToolsRequest{})
				require.NoError(t, err)
				tools = result.Tools
			case "IterTools":
				for tool, err := range c.IterTools(t.Context(), mcp.ListToolsRequest{}) {
					require.NoError(t, err)
					tools = append(tools, tool)
				}
			}
			require.Len(t, tools, 2)
			data, err := json.Marshal(tools[0])
			require.NoError(t, err)
			var wire struct {
				OutputSchema struct {
					Type json.RawMessage `json:"type"`
				} `json:"outputSchema"`
			}
			require.NoError(t, json.Unmarshal(data, &wire))
			assert.JSONEq(t, `["object","null"]`, string(wire.OutputSchema.Type))
			request := mcp.CallToolRequest{}
			request.Params.Name = "get_me"
			result, err := c.CallTool(t.Context(), request)
			require.NoError(t, err)
			assert.False(t, result.IsError)
		})
	}
}
