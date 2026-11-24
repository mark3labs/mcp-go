package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

// BenchmarkInputSchemaValidation measures warm validation without request handling.
func BenchmarkInputSchemaValidation(b *testing.B) {
	description := mcp.Description(strings.Repeat("description ", 8))
	for _, properties := range []int{10, 100, 1000} {
		options := make([]mcp.ToolOption, 0, properties)
		for i := range properties {
			options = append(options, mcp.WithString(fmt.Sprintf("field_%04d", i), description))
		}
		structured := mcp.NewTool("validation", options...)
		raw, err := json.Marshal(structured.InputSchema)
		require.NoError(b, err)
		preencoded := mcp.NewTool(structured.Name, mcp.WithRawInputSchema(raw))

		tests := []struct {
			name string
			tool mcp.Tool
		}{
			{name: "structured", tool: structured},
			{name: "raw", tool: preencoded},
		}
		for _, test := range tests {
			b.Run(fmt.Sprintf("properties=%d/%s", properties, test.name), func(b *testing.B) {
				validator := newInputSchemaValidator()
				arguments := json.RawMessage(`{"field_0000":"value"}`)
				ran, err := validator.validate(test.tool, arguments)
				require.True(b, ran)
				require.NoError(b, err)
				b.ReportAllocs()
				for b.Loop() {
					ran, err := validator.validate(test.tool, arguments)
					if err != nil || !ran {
						b.Fatalf("validation ran: %t, error: %v", ran, err)
					}
				}
			})
		}
	}
}
