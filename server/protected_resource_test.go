package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

func resourceName(s string) *string { return &s }

// --- NewProtectedResourceMetadataHandler (Option A) ---

func TestNewProtectedResourceMetadataHandler_GET(t *testing.T) {
	meta := server.OAuthProtectedResourceMetadata{
		Resource:             "https://my-server.example.com",
		AuthorizationServers: []string{"https://auth.example.com"},
		ScopesSupported:      []string{"mcp:read", "mcp:write"},
		ResourceName:         resourceName("My MCP Server"),
	}

	h := server.NewProtectedResourceMetadataHandler(meta)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var got server.OAuthProtectedResourceMetadata
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if got.Resource != meta.Resource {
		t.Errorf("Resource = %q, want %q", got.Resource, meta.Resource)
	}
	if len(got.AuthorizationServers) != 1 || got.AuthorizationServers[0] != "https://auth.example.com" {
		t.Errorf("AuthorizationServers = %v, want [https://auth.example.com]", got.AuthorizationServers)
	}
	if len(got.ScopesSupported) != 2 {
		t.Errorf("ScopesSupported = %v, want 2 entries", got.ScopesSupported)
	}
	if got.ResourceName == nil || *got.ResourceName != "My MCP Server" {
		t.Errorf("ResourceName = %v, want 'My MCP Server'", got.ResourceName)
	}
}

func TestNewProtectedResourceMetadataHandler_MethodNotAllowed(t *testing.T) {
	h := server.NewProtectedResourceMetadataHandler(server.OAuthProtectedResourceMetadata{
		Resource: "https://my-server.example.com",
	})

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/.well-known/oauth-protected-resource", nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s: expected 405, got %d", method, rec.Code)
			}
			if allow := rec.Header().Get("Allow"); allow == "" {
				t.Errorf("%s: expected Allow header to be set", method)
			}
		})
	}
}

func TestNewProtectedResourceMetadataHandler_OPTIONS(t *testing.T) {
	h := server.NewProtectedResourceMetadataHandler(server.OAuthProtectedResourceMetadata{
		Resource: "https://my-server.example.com",
	})

	req := httptest.NewRequest(http.MethodOptions, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("OPTIONS: expected 204, got %d", rec.Code)
	}
	if acao := rec.Header().Get("Access-Control-Allow-Origin"); acao != "*" {
		t.Errorf("CORS origin header = %q, want *", acao)
	}
}

func TestNewProtectedResourceMetadataHandler_CORSHeaders(t *testing.T) {
	h := server.NewProtectedResourceMetadataHandler(server.OAuthProtectedResourceMetadata{
		Resource: "https://my-server.example.com",
	})

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if acao := rec.Header().Get("Access-Control-Allow-Origin"); acao != "*" {
		t.Errorf("CORS origin header = %q, want *", acao)
	}
}

func TestNewProtectedResourceMetadataHandler_CacheControl(t *testing.T) {
	h := server.NewProtectedResourceMetadataHandler(server.OAuthProtectedResourceMetadata{
		Resource: "https://my-server.example.com",
	})

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

func TestNewProtectedResourceMetadataHandler_MinimalConfig(t *testing.T) {
	h := server.NewProtectedResourceMetadataHandler(server.OAuthProtectedResourceMetadata{
		Resource: "https://minimal.example.com",
	})

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var got server.OAuthProtectedResourceMetadata
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if got.Resource != "https://minimal.example.com" {
		t.Errorf("Resource = %q, want https://minimal.example.com", got.Resource)
	}
}

func TestNewProtectedResourceMetadataHandler_EmptyResource(t *testing.T) {
	h := server.NewProtectedResourceMetadataHandler(server.OAuthProtectedResourceMetadata{})

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("empty Resource: expected 500, got %d", rec.Code)
	}
}

// --- WithProtectedResourceMetadata (StreamableHTTP / Option B) ---

func TestStreamableHTTPServer_WithProtectedResourceMetadata(t *testing.T) {
	mcpSrv := server.NewMCPServer("test", "1.0.0")
	meta := server.OAuthProtectedResourceMetadata{
		Resource:             "https://my-mcp-server.example.com",
		AuthorizationServers: []string{"https://auth.example.com"},
		ScopesSupported:      []string{"mcp:read"},
	}

	httpSrv := server.NewStreamableHTTPServer(mcpSrv,
		server.WithProtectedResourceMetadata(meta),
	)
	ts := httptest.NewServer(httpSrv.HTTPHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/oauth-protected-resource")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var got server.OAuthProtectedResourceMetadata
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if got.Resource != meta.Resource {
		t.Errorf("Resource = %q, want %q", got.Resource, meta.Resource)
	}
	if len(got.AuthorizationServers) != 1 || got.AuthorizationServers[0] != meta.AuthorizationServers[0] {
		t.Errorf("AuthorizationServers = %v, want %v", got.AuthorizationServers, meta.AuthorizationServers)
	}
}

// --- Integration: verify /.well-known path doesn't interfere with /mcp ---

func TestStreamableHTTPServer_WellKnownDoesNotBreakMCP(t *testing.T) {
	mcpSrv := server.NewMCPServer("test", "1.0.0")
	meta := server.OAuthProtectedResourceMetadata{
		Resource: "https://my-server.example.com",
	}

	httpSrv := server.NewStreamableHTTPServer(mcpSrv,
		server.WithProtectedResourceMetadata(meta),
	)
	ts := httptest.NewServer(httpSrv.HTTPHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/oauth-protected-resource")
	if err != nil {
		t.Fatalf("GET well-known failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("well-known: expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("well-known Content-Type = %q, want application/json", ct)
	}
}

// --- WithSSEProtectedResourceMetadata (SSE / Option B) ---

func TestSSEServer_WithSSEProtectedResourceMetadata(t *testing.T) {
	mcpSrv := server.NewMCPServer("test", "1.0.0")
	meta := server.OAuthProtectedResourceMetadata{
		Resource:             "https://my-sse-server.example.com",
		AuthorizationServers: []string{"https://auth.example.com"},
	}

	ts := server.NewTestServer(mcpSrv,
		server.WithSSEProtectedResourceMetadata(meta),
	)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/oauth-protected-resource")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var got server.OAuthProtectedResourceMetadata
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Resource != meta.Resource {
		t.Errorf("Resource = %q, want %q", got.Resource, meta.Resource)
	}
	if len(got.AuthorizationServers) != 1 || got.AuthorizationServers[0] != meta.AuthorizationServers[0] {
		t.Errorf("AuthorizationServers = %v, want %v", got.AuthorizationServers, meta.AuthorizationServers)
	}
}
