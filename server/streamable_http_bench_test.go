package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func modernCallMsg(method string, size int) []byte {
	params := fmt.Sprintf(`"name":"echo","arguments":{"text":%q}`, strings.Repeat("a", size))
	if method != "tools/call" {
		params = ""
	}
	sep := ""
	if params != "" {
		sep = ","
	}
	return fmt.Appendf(nil, `{"jsonrpc":"2.0","id":1,"method":%q,"params":{%s%s"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`, method, params, sep)
}

func BenchmarkStreamableHTTPModern(b *testing.B) {
	name, _ := mcp.EncodeHeaderValue("echo")
	cases := []struct {
		method string
		size   int
	}{
		{"tools/call", 16},
		{"tools/call", 64 << 10},
		{"tools/call", 1 << 20},
		{"tools/list", 0},
	}
	for _, c := range cases {
		h := NewStreamableHTTPServer(benchServer(), WithDisableLocalhostProtection(true))
		body := modernCallMsg(c.method, c.size)
		b.Run(fmt.Sprintf("%s/args=%d", strings.ReplaceAll(c.method, "/", "_"), c.size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			for i := 0; i < b.N; i++ {
				req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", "application/json, text/event-stream")
				req.Header.Set(mcp.HeaderProtocolVersion, "2026-07-28")
				req.Header.Set(mcp.HeaderMethod, c.method)
				if c.method == "tools/call" {
					req.Header.Set(mcp.HeaderName, name)
				}
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"result"`)) {
					b.Fatalf("status %d: %s", rec.Code, rec.Body.String())
				}
			}
		})
	}
}

func BenchmarkStreamableHTTPToolCall(b *testing.B) {
	for _, size := range []int{16, 64 << 10, 1 << 20} {
		h := NewStreamableHTTPServer(benchServer(), WithStateLess(true), WithDisableLocalhostProtection(true))
		body := []byte(callMsg(size))
		b.Run(fmt.Sprintf("args=%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			for i := 0; i < b.N; i++ {
				req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", "application/json, text/event-stream")
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					b.Fatalf("status %d: %s", rec.Code, rec.Body.String())
				}
				_, _ = io.Copy(io.Discard, rec.Body)
			}
		})
	}
}
