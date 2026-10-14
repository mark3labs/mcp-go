package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// bigRawSchema builds an input schema of about size bytes; annotated adds one
// x-mcp-header property.
func bigRawSchema(size int, annotated bool) json.RawMessage {
	var b strings.Builder
	b.WriteString(`{"type":"object","properties":{`)
	if annotated {
		b.WriteString(`"region":{"type":"string","x-mcp-header":"Region"},`)
	} else {
		b.WriteString(`"region":{"type":"string"},`)
	}
	for i := 0; b.Len() < size-40; i++ {
		fmt.Fprintf(&b, `"p%03d":{"type":"string","description":"property number %03d"},`, i, i)
	}
	b.WriteString(`"last":{"type":"string"}}}`)
	return json.RawMessage(b.String())
}

// BenchmarkParamHeadersValidation measures the header check a 2026-07-28
// tools/call pays per request, against an 8 KB schema.
func BenchmarkParamHeadersValidation(b *testing.B) {
	for _, tc := range []struct {
		name      string
		annotated bool
	}{{"annotated", true}, {"plain", false}} {
		b.Run(tc.name, func(b *testing.B) {
			srv := NewMCPServer("bench", "1.0.0", WithToolCapabilities(true))
			srv.AddTool(mcp.NewToolWithRawSchema("big", "a big tool", bigRawSchema(8192, tc.annotated)), nil)
			message := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"big","arguments":{"region":"us-east-1"}}}`)
			headers := http.Header{}
			headers.Set(mcp.HeaderProtocolVersion, mcp.ProtocolVersion20260728)
			headers.Set(mcp.HeaderMethod, string(mcp.MethodToolsCall))
			headers.Set(mcp.HeaderName, "big")
			if tc.annotated {
				headers.Set(mcp.HeaderParamPrefix+"Region", "us-east-1")
			}
			ctx := b.Context()
			if err := srv.validateStandardHeadersForMessage(ctx, headers, mcp.ProtocolVersion20260728, mcp.MethodToolsCall, message); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				_ = srv.validateStandardHeadersForMessage(ctx, headers, mcp.ProtocolVersion20260728, mcp.MethodToolsCall, message)
			}
		})
	}
}
