package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCallToolResultOwnsRawStructuredContent(t *testing.T) {
	wire := []byte(`{"structuredContent":{"id":9007199254740993}}`)
	var result CallToolResult
	require.NoError(t, json.Unmarshal(wire, &result))
	clear(wire)
	require.Equal(t, `{"id":9007199254740993}`, string(result.RawStructuredContent))
}

func BenchmarkCallToolResultStructuredContent(b *testing.B) {
	for _, tc := range []struct {
		name string
		size int
	}{
		{"4KiB", 4 << 10},
		{"64KiB", 64 << 10},
	} {
		b.Run(tc.name, func(b *testing.B) {
			raw := json.RawMessage(`{"structuredContent":{"text":"` +
				strings.Repeat("x", tc.size) + `"}}`)
			b.ReportAllocs()
			for b.Loop() {
				if _, err := ParseCallToolResult(&raw); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
