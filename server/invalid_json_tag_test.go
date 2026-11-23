package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestToolValidation_InvalidJSONTagNames(t *testing.T) {
	type request struct {
		Value string `json:"bad\\name"`
		Count int    `json:"bad\"count,string"`
	}
	tests := []struct {
		name    string
		options []ServerOption
	}{
		{name: "input", options: []ServerOption{WithInputSchemaValidation()}},
		{name: "output", options: []ServerOption{WithOutputSchemaValidation()}},
		{name: "input and output", options: []ServerOption{WithInputSchemaValidation(), WithOutputSchemaValidation()}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv := NewMCPServer("test", "1.0", test.options...)
			called := false
			tool := mcp.NewTool("echo", mcp.WithInputSchema[request](), mcp.WithOutputSchema[request]())
			srv.AddTool(tool, mcp.NewStructuredToolHandler(func(_ context.Context, _ mcp.CallToolRequest, args request) (request, error) {
				called = true
				require.Equal(t, request{Value: "hello", Count: 7}, args)
				return args, nil
			}))
			result := requireToolSuccess(t, callTool(t, srv, "echo", map[string]any{"Value": "hello", "Count": "7"}))
			require.True(t, called)
			encoded, err := json.Marshal(result.StructuredContent)
			require.NoError(t, err)
			require.JSONEq(t, `{"Value":"hello","Count":"7"}`, string(encoded))
		})
	}
}
