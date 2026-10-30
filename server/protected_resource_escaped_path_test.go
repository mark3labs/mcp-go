package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/server"
)

func TestProtectedResourceMetadataEscapedPaths(t *testing.T) {
	const prefix = "/.well-known/oauth-protected-resource"
	tests := []struct {
		name         string
		resourcePath string
		requestPath  string
		wantMetadata bool
	}{
		{"root", "", "", true},
		{"ordinary path", "/tenant/acme/mcp", "/tenant/acme/mcp", true},
		{"space", "/tenant/acme%20corp/mcp", "/tenant/acme%20corp/mcp", true},
		{"unicode", "/tenant/%E4%B8%AD%E6%96%87/mcp", "/tenant/%E4%B8%AD%E6%96%87/mcp", true},
		{"percent", "/tenant/acme%25/mcp", "/tenant/acme%25/mcp", true},
		{"encoded slash", "/tenant/acme%2Fcorp/mcp", "/tenant/acme%2Fcorp/mcp", true},
		{"escape case", "/tenant/acme%2Fcorp/mcp", "/tenant/acme%2fcorp/mcp", true},
		{"unreserved character", "/tenant/acme/mcp", "/tenant/%61cme/mcp", true},
		{"slash remains a segment boundary", "/tenant/acme%2Fcorp/mcp", "/tenant/acme/corp/mcp", false},
		{"encoded braces", "/tenant/%7Bteam%7D/mcp", "/tenant/%7Bteam%7D/mcp", true},
		{"braces remain literal", "/tenant/%7Bteam%7D/mcp", "/tenant/other/mcp", false},
	}
	transports := []struct {
		name       string
		newHandler func(server.ProtectedResourceMetadataConfig) http.Handler
		missStatus int
	}{
		{
			name: "streamable HTTP",
			newHandler: func(cfg server.ProtectedResourceMetadataConfig) http.Handler {
				return server.NewStreamableHTTPServer(server.NewMCPServer("test", "1.0.0"),
					server.WithProtectedResourceMetadata(cfg), server.WithDisableStreaming(true))
			},
			missStatus: http.StatusMethodNotAllowed,
		},
		{
			name: "SSE",
			newHandler: func(cfg server.ProtectedResourceMetadataConfig) http.Handler {
				return server.NewSSEServer(server.NewMCPServer("test", "1.0.0"),
					server.WithSSEProtectedResourceMetadata(cfg))
			},
			missStatus: http.StatusNotFound,
		},
		{
			name: "custom ServeMux",
			newHandler: func(cfg server.ProtectedResourceMetadataConfig) http.Handler {
				mux := http.NewServeMux()
				mux.Handle(server.ProtectedResourceMetadataPath(cfg.Resource),
					server.NewProtectedResourceMetadataHandler(cfg))
				return mux
			},
			missStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := server.ProtectedResourceMetadataConfig{Resource: "https://mcp.example.com" + tt.resourcePath}
			assert.Equal(t, prefix+tt.resourcePath, server.ProtectedResourceMetadataPath(cfg.Resource))
			for _, transport := range transports {
				t.Run(transport.name, func(t *testing.T) {
					var handler http.Handler
					if !assert.NotPanics(t, func() { handler = transport.newHandler(cfg) }) {
						return
					}
					ts := httptest.NewServer(handler)
					defer ts.Close()

					resp, err := ts.Client().Get(ts.URL + prefix + tt.requestPath)
					require.NoError(t, err)
					defer resp.Body.Close()
					if !tt.wantMetadata {
						assert.Equal(t, transport.missStatus, resp.StatusCode)
						return
					}
					require.Equal(t, http.StatusOK, resp.StatusCode)
					assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
					var got server.ProtectedResourceMetadataConfig
					require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
					assert.Equal(t, cfg.Resource, got.Resource)
				})
			}
		})
	}
}
