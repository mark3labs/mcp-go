package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/server"
)

func TestProtectedResourceMetadataPath(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		want     string
	}{
		{
			name:     "empty resource",
			resource: "",
			want:     "/.well-known/oauth-protected-resource",
		},
		{
			name:     "host only",
			resource: "https://mcp.example.com",
			want:     "/.well-known/oauth-protected-resource",
		},
		{
			name:     "host with trailing slash",
			resource: "https://mcp.example.com/",
			want:     "/.well-known/oauth-protected-resource",
		},
		{
			name:     "single path segment",
			resource: "https://mcp.example.com/mcp",
			want:     "/.well-known/oauth-protected-resource/mcp",
		},
		{
			name:     "multi-segment path",
			resource: "https://mcp.example.com/api/v1/mcp",
			want:     "/.well-known/oauth-protected-resource/api/v1/mcp",
		},
		{
			name:     "trailing slash on path",
			resource: "https://mcp.example.com/mcp/",
			want:     "/.well-known/oauth-protected-resource/mcp",
		},
		{
			name:     "unparseable resource falls back to base path",
			resource: "://broken",
			want:     "/.well-known/oauth-protected-resource",
		},
		{
			name:     "bare host without scheme falls back to base path",
			resource: "mcp.example.com",
			want:     "/.well-known/oauth-protected-resource",
		},
		{
			name:     "bare host with path but no scheme falls back to base path",
			resource: "mcp.example.com/mcp",
			want:     "/.well-known/oauth-protected-resource",
		},
		{
			name:     "path-only resource falls back to base path",
			resource: "/mcp",
			want:     "/.well-known/oauth-protected-resource",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, server.ProtectedResourceMetadataPath(tt.resource))
		})
	}
}

func TestNewProtectedResourceMetadataHandler_GET(t *testing.T) {
	cfg := server.ProtectedResourceMetadataConfig{
		Resource:               "https://my-server.example.com",
		AuthorizationServers:   []string{"https://auth.example.com"},
		ScopesSupported:        []string{"mcp:read", "mcp:write"},
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "My MCP Server",
		ResourceDocumentation:  "https://docs.example.com",
		JWKSURI:                "https://my-server.example.com/.well-known/jwks.json",
	}

	h := server.NewProtectedResourceMetadataHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET, HEAD, OPTIONS", rec.Header().Get("Access-Control-Allow-Methods"))

	var got server.ProtectedResourceMetadataConfig
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, cfg.Resource, got.Resource)
	assert.Equal(t, cfg.AuthorizationServers, got.AuthorizationServers)
	assert.Equal(t, cfg.ScopesSupported, got.ScopesSupported)
	assert.Equal(t, cfg.BearerMethodsSupported, got.BearerMethodsSupported)
	assert.Equal(t, cfg.ResourceName, got.ResourceName)
	assert.Equal(t, cfg.ResourceDocumentation, got.ResourceDocumentation)
	assert.Equal(t, cfg.JWKSURI, got.JWKSURI)
}

func TestNewProtectedResourceMetadataHandler_HEAD(t *testing.T) {
	h := server.NewProtectedResourceMetadataHandler(server.ProtectedResourceMetadataConfig{
		Resource: "https://my-server.example.com",
	})

	req := httptest.NewRequest(http.MethodHead, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	body, _ := io.ReadAll(rec.Body)
	assert.Empty(t, body, "HEAD response must have no body")
}

func TestNewProtectedResourceMetadataHandler_OPTIONS(t *testing.T) {
	h := server.NewProtectedResourceMetadataHandler(server.ProtectedResourceMetadataConfig{
		Resource: "https://my-server.example.com",
	})

	req := httptest.NewRequest(http.MethodOptions, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET, HEAD, OPTIONS", rec.Header().Get("Access-Control-Allow-Methods"))
}

func TestNewProtectedResourceMetadataHandler_MethodNotAllowed(t *testing.T) {
	h := server.NewProtectedResourceMetadataHandler(server.ProtectedResourceMetadataConfig{
		Resource: "https://my-server.example.com",
	})

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/.well-known/oauth-protected-resource", nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
			assert.NotEmpty(t, rec.Header().Get("Allow"), "Allow header must be set on 405")
		})
	}
}

func TestNewProtectedResourceMetadataHandler_OmitsEmptyFields(t *testing.T) {
	h := server.NewProtectedResourceMetadataHandler(server.ProtectedResourceMetadataConfig{
		Resource: "https://minimal.example.com",
	})

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var raw map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&raw))

	// resource is required and always present, even when emitted alone.
	assert.Equal(t, "https://minimal.example.com", raw["resource"])

	// All other fields use omitempty and must be absent.
	for _, key := range []string{
		"authorization_servers",
		"scopes_supported",
		"bearer_methods_supported",
		"resource_name",
		"resource_documentation",
		"resource_policy_uri",
		"resource_tos_uri",
		"jwks_uri",
		"resource_signing_alg_values_supported",
		"tls_client_certificate_bound_access_tokens",
		"authorization_details_types_supported",
		"dpop_signing_alg_values_supported",
		"dpop_bound_access_tokens_required",
	} {
		_, present := raw[key]
		assert.False(t, present, "expected field %q to be omitted", key)
	}
}

func TestStreamableHTTPServer_WithProtectedResourceMetadata_ServeHTTP(t *testing.T) {
	mcpSrv := server.NewMCPServer("test", "1.0.0")
	cfg := server.ProtectedResourceMetadataConfig{
		Resource:             "https://mcp.example.com",
		AuthorizationServers: []string{"https://auth.example.com"},
		ScopesSupported:      []string{"mcp:read"},
	}

	httpSrv := server.NewStreamableHTTPServer(mcpSrv,
		server.WithProtectedResourceMetadata(cfg),
	)

	ts := httptest.NewServer(httpSrv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/oauth-protected-resource")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var got server.ProtectedResourceMetadataConfig
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, cfg.Resource, got.Resource)
	assert.Equal(t, cfg.AuthorizationServers, got.AuthorizationServers)
	assert.Equal(t, cfg.ScopesSupported, got.ScopesSupported)
}

func TestStreamableHTTPServer_WithProtectedResourceMetadata_PathQualifiedResource(t *testing.T) {
	mcpSrv := server.NewMCPServer("test", "1.0.0")
	cfg := server.ProtectedResourceMetadataConfig{
		Resource:             "https://mcp.example.com/mcp",
		AuthorizationServers: []string{"https://auth.example.com"},
	}

	httpSrv := server.NewStreamableHTTPServer(mcpSrv,
		server.WithProtectedResourceMetadata(cfg),
	)
	ts := httptest.NewServer(httpSrv)
	defer ts.Close()

	// Path-qualified well-known path per RFC 9728 §3.1.
	resp, err := http.Get(ts.URL + "/.well-known/oauth-protected-resource/mcp")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got server.ProtectedResourceMetadataConfig
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, "https://mcp.example.com/mcp", got.Resource)

	// Bare well-known path must NOT serve PRM JSON for path-qualified resources.
	resp2, err := http.Get(ts.URL + "/.well-known/oauth-protected-resource")
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.NotEqual(t, "application/json", resp2.Header.Get("Content-Type"),
		"bare well-known path must not serve PRM when resource is path-qualified")
}

func TestStreamableHTTPServer_WithProtectedResourceMetadata_DoesNotInterfereWithMCP(t *testing.T) {
	mcpSrv := server.NewMCPServer("test", "1.0.0")
	httpSrv := server.NewStreamableHTTPServer(mcpSrv,
		server.WithProtectedResourceMetadata(server.ProtectedResourceMetadataConfig{
			Resource: "https://mcp.example.com",
		}),
	)
	ts := httptest.NewServer(httpSrv)
	defer ts.Close()

	// Initialize MCP request must still be served at the default /mcp endpoint.
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL+"/mcp", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.NotEqual(t, http.StatusNotFound, resp.StatusCode, "MCP endpoint should still be reachable")
}

func TestStreamableHTTPServer_WithoutProtectedResourceMetadata_NoMetadataServed(t *testing.T) {
	mcpSrv := server.NewMCPServer("test", "1.0.0")
	httpSrv := server.NewStreamableHTTPServer(mcpSrv)
	ts := httptest.NewServer(httpSrv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/oauth-protected-resource")
	require.NoError(t, err)
	defer resp.Body.Close()

	// We just need to ensure we are NOT serving PRM JSON when not configured.
	assert.NotEqual(t, "application/json", resp.Header.Get("Content-Type"),
		"well-known endpoint must not return PRM JSON when not configured")
}

func TestSSEServer_WithSSEProtectedResourceMetadata(t *testing.T) {
	mcpSrv := server.NewMCPServer("test", "1.0.0")
	cfg := server.ProtectedResourceMetadataConfig{
		Resource:             "https://sse.example.com",
		AuthorizationServers: []string{"https://auth.example.com"},
	}

	ts := server.NewTestServer(mcpSrv,
		server.WithSSEProtectedResourceMetadata(cfg),
	)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/oauth-protected-resource")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got server.ProtectedResourceMetadataConfig
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, cfg.Resource, got.Resource)
	assert.Equal(t, cfg.AuthorizationServers, got.AuthorizationServers)
}

func TestSSEServer_WithSSEProtectedResourceMetadata_PathQualifiedResource(t *testing.T) {
	mcpSrv := server.NewMCPServer("test", "1.0.0")
	cfg := server.ProtectedResourceMetadataConfig{
		Resource: "https://sse.example.com/mcp",
	}

	ts := server.NewTestServer(mcpSrv,
		server.WithSSEProtectedResourceMetadata(cfg),
	)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/oauth-protected-resource/mcp")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSSEServer_WithoutProtectedResourceMetadata_WellKnown404(t *testing.T) {
	mcpSrv := server.NewMCPServer("test", "1.0.0")
	ts := server.NewTestServer(mcpSrv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/oauth-protected-resource")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
