package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func benchServer() *MCPServer {
	s := NewMCPServer("bench", "1.0.0", WithToolCapabilities(false))
	s.AddTool(mcp.NewTool("echo", mcp.WithString("text")), func(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	})
	return s
}

func callMsg(size int) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"text":%q}}}`, strings.Repeat("a", size)))
}

func respMsg(size int) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"jsonrpc":"2.0","id":7,"result":{"content":[{"type":"text","text":%q}]}}`, strings.Repeat("a", size)))
}

func BenchmarkDispatchToolCall(b *testing.B) {
	for _, size := range []int{16, 64 << 10, 1 << 20} {
		s := benchServer()
		msg := callMsg(size)
		ctx := b.Context()
		b.Run(fmt.Sprintf("args=%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(msg)))
			for i := 0; i < b.N; i++ {
				if s.HandleMessage(ctx, msg) == nil {
					b.Fatal("nil response")
				}
			}
		})
	}
}

func BenchmarkDispatchClientResponse(b *testing.B) {
	for _, size := range []int{16, 64 << 10, 1 << 20} {
		s := benchServer()
		msg := respMsg(size)
		ctx := b.Context()
		b.Run(fmt.Sprintf("result=%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(msg)))
			for i := 0; i < b.N; i++ {
				s.HandleMessage(ctx, msg)
			}
		})
	}
}
