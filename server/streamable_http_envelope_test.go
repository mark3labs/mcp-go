package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStreamableHTTP_PostEnvelopeAndEra(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		version    string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "legacy tool call",
			body:       `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hi"}}}`,
			wantStatus: http.StatusOK,
			wantBody:   `"result":{"content":[{"type":"text","text":"ok"}]}`,
		},
		{
			name:       "params array reaches the method handler",
			body:       `{"jsonrpc":"2.0","id":2,"method":"ping","params":[1]}`,
			wantStatus: http.StatusOK,
			wantBody:   `"code":-32600`,
		},
		{
			name:       "malformed _meta reaches the method handler",
			body:       `{"jsonrpc":"2.0","id":3,"method":"ping","params":{"_meta":"x"}}`,
			wantStatus: http.StatusOK,
			wantBody:   `"code":-32600`,
		},
		{
			name:       "modern version in _meta is detected",
			body:       `{"jsonrpc":"2.0","id":4,"method":"ping","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `"code":-32020`,
		},
		{
			name:       "modern version in header is detected",
			body:       `{"jsonrpc":"2.0","id":5,"method":"ping"}`,
			version:    "2026-07-28",
			wantStatus: http.StatusBadRequest,
			wantBody:   `"code":-32602`,
		},
		{
			name:       "invalid json",
			body:       `{"jsonrpc":"2.0","id":6,`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `"code":-32700`,
		},
		{
			name:       "client response",
			body:       `{"jsonrpc":"2.0","id":7,"result":{}}`,
			wantStatus: http.StatusAccepted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewStreamableHTTPServer(benchServer(), WithStateLess(true), WithDisableLocalhostProtection(true))
			req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(tt.body)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			if tt.version != "" {
				req.Header.Set("MCP-Protocol-Version", tt.version)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.wantBody)
		})
	}
}
