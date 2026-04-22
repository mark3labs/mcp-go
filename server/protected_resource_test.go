package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProtectedResourceMetadataHandler(t *testing.T) {
	metadata := &ProtectedResourceMetadata{
		Resource:             "https://mcp.example.com",
		AuthorizationServers: []string{"https://auth.example.com"},
		ScopesSupported:      []string{"read", "write"},
		ResourceName:         "Example MCP Server",
	}

	handler := ProtectedResourceMetadataHandler(metadata)

	t.Run("GET returns metadata as JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
		assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))

		var got ProtectedResourceMetadata
		err := json.NewDecoder(w.Body).Decode(&got)
		require.NoError(t, err)
		assert.Equal(t, metadata.Resource, got.Resource)
		assert.Equal(t, metadata.AuthorizationServers, got.AuthorizationServers)
		assert.Equal(t, metadata.ScopesSupported, got.ScopesSupported)
		assert.Equal(t, metadata.ResourceName, got.ResourceName)
	})

	t.Run("OPTIONS returns no content with CORS headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/.well-known/oauth-protected-resource", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
		assert.Equal(t, "GET, OPTIONS", w.Header().Get("Access-Control-Allow-Methods"))
	})

	t.Run("POST returns method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/.well-known/oauth-protected-resource", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

func TestProtectedResourceMetadataPath(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		want     string
	}{
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
			name:     "path-qualified resource",
			resource: "https://mcp.example.com/mcp",
			want:     "/.well-known/oauth-protected-resource/mcp",
		},
		{
			name:     "deep path-qualified resource",
			resource: "https://mcp.example.com/api/v1/mcp",
			want:     "/.well-known/oauth-protected-resource/api/v1/mcp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ProtectedResourceMetadataPath(tt.resource))
		})
	}
}

func TestStreamableHTTPServer_ProtectedResourceMetadata(t *testing.T) {
	metadata := &ProtectedResourceMetadata{
		Resource:             "https://mcp.example.com",
		AuthorizationServers: []string{"https://auth.example.com"},
		ScopesSupported:      []string{"mcp:read"},
	}

	t.Run("ProtectedResourceMetadataEndpoint returns handler when configured", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		httpServer := NewStreamableHTTPServer(mcpServer,
			WithProtectedResourceMetadata(metadata),
		)

		handler := httpServer.ProtectedResourceMetadataEndpoint()
		require.NotNil(t, handler)

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var got ProtectedResourceMetadata
		err := json.NewDecoder(w.Body).Decode(&got)
		require.NoError(t, err)
		assert.Equal(t, metadata.Resource, got.Resource)
		assert.Equal(t, metadata.AuthorizationServers, got.AuthorizationServers)
	})

	t.Run("ProtectedResourceMetadataEndpoint returns nil when not configured", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		httpServer := NewStreamableHTTPServer(mcpServer)

		handler := httpServer.ProtectedResourceMetadataEndpoint()
		assert.Nil(t, handler)
	})

	t.Run("Start registers well-known endpoint", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		httpServer := NewStreamableHTTPServer(mcpServer,
			WithProtectedResourceMetadata(metadata),
		)

		// Use the mux that Start would create by manually constructing it
		mux := http.NewServeMux()
		mux.Handle("/mcp", httpServer)
		mux.Handle(ProtectedResourceMetadataPath(metadata.Resource), ProtectedResourceMetadataHandler(metadata))

		ts := httptest.NewServer(mux)
		defer ts.Close()

		resp, err := http.Get(ts.URL + "/.well-known/oauth-protected-resource")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var got ProtectedResourceMetadata
		err = json.NewDecoder(resp.Body).Decode(&got)
		require.NoError(t, err)
		assert.Equal(t, "https://mcp.example.com", got.Resource)
		assert.Equal(t, []string{"https://auth.example.com"}, got.AuthorizationServers)
		assert.Equal(t, []string{"mcp:read"}, got.ScopesSupported)
	})

	t.Run("Start registers path-qualified well-known endpoint", func(t *testing.T) {
		pathMetadata := &ProtectedResourceMetadata{
			Resource:             "https://mcp.example.com/mcp",
			AuthorizationServers: []string{"https://auth.example.com"},
		}

		mcpServer := NewMCPServer("test", "1.0.0")
		httpServer := NewStreamableHTTPServer(mcpServer,
			WithProtectedResourceMetadata(pathMetadata),
		)

		mux := http.NewServeMux()
		mux.Handle("/mcp", httpServer)
		mux.Handle(ProtectedResourceMetadataPath(pathMetadata.Resource), ProtectedResourceMetadataHandler(pathMetadata))

		ts := httptest.NewServer(mux)
		defer ts.Close()

		resp, err := http.Get(ts.URL + "/.well-known/oauth-protected-resource/mcp")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var got ProtectedResourceMetadata
		err = json.NewDecoder(resp.Body).Decode(&got)
		require.NoError(t, err)
		assert.Equal(t, "https://mcp.example.com/mcp", got.Resource)
	})

	t.Run("ServeHTTP dispatches well-known endpoint", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		httpServer := NewStreamableHTTPServer(mcpServer,
			WithProtectedResourceMetadata(metadata),
		)

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
		w := httptest.NewRecorder()
		httpServer.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var got ProtectedResourceMetadata
		err := json.NewDecoder(w.Body).Decode(&got)
		require.NoError(t, err)
		assert.Equal(t, metadata.Resource, got.Resource)
	})

	t.Run("ServeHTTP dispatches path-qualified well-known endpoint", func(t *testing.T) {
		pathMetadata := &ProtectedResourceMetadata{
			Resource:             "https://mcp.example.com/mcp",
			AuthorizationServers: []string{"https://auth.example.com"},
		}

		mcpServer := NewMCPServer("test", "1.0.0")
		httpServer := NewStreamableHTTPServer(mcpServer,
			WithProtectedResourceMetadata(pathMetadata),
		)

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp", nil)
		w := httptest.NewRecorder()
		httpServer.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var got ProtectedResourceMetadata
		err := json.NewDecoder(w.Body).Decode(&got)
		require.NoError(t, err)
		assert.Equal(t, "https://mcp.example.com/mcp", got.Resource)
	})
}

func TestSSEServer_ProtectedResourceMetadata(t *testing.T) {
	metadata := &ProtectedResourceMetadata{
		Resource:             "https://mcp.example.com",
		AuthorizationServers: []string{"https://auth.example.com"},
		ScopesSupported:      []string{"mcp:read"},
	}

	t.Run("ServeHTTP routes well-known endpoint", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		sseServer := NewSSEServer(mcpServer,
			WithSSEProtectedResourceMetadata(metadata),
			WithBaseURL("http://localhost:8080"),
		)

		ts := httptest.NewServer(sseServer)
		defer ts.Close()

		resp, err := http.Get(ts.URL + "/.well-known/oauth-protected-resource")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var got ProtectedResourceMetadata
		err = json.NewDecoder(resp.Body).Decode(&got)
		require.NoError(t, err)
		assert.Equal(t, "https://mcp.example.com", got.Resource)
	})

	t.Run("ServeHTTP routes path-qualified well-known endpoint", func(t *testing.T) {
		pathMetadata := &ProtectedResourceMetadata{
			Resource:             "https://mcp.example.com/mcp",
			AuthorizationServers: []string{"https://auth.example.com"},
		}

		mcpServer := NewMCPServer("test", "1.0.0")
		sseServer := NewSSEServer(mcpServer,
			WithSSEProtectedResourceMetadata(pathMetadata),
			WithBaseURL("http://localhost:8080"),
		)

		ts := httptest.NewServer(sseServer)
		defer ts.Close()

		resp, err := http.Get(ts.URL + "/.well-known/oauth-protected-resource/mcp")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var got ProtectedResourceMetadata
		err = json.NewDecoder(resp.Body).Decode(&got)
		require.NoError(t, err)
		assert.Equal(t, "https://mcp.example.com/mcp", got.Resource)
	})

	t.Run("ServeHTTP does not route well-known when not configured", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		sseServer := NewSSEServer(mcpServer,
			WithBaseURL("http://localhost:8080"),
		)

		ts := httptest.NewServer(sseServer)
		defer ts.Close()

		resp, err := http.Get(ts.URL + "/.well-known/oauth-protected-resource")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("ProtectedResourceMetadataEndpoint returns handler when configured", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		sseServer := NewSSEServer(mcpServer,
			WithSSEProtectedResourceMetadata(metadata),
		)

		handler := sseServer.ProtectedResourceMetadataEndpoint()
		require.NotNil(t, handler)
	})

	t.Run("ProtectedResourceMetadataEndpoint returns nil when not configured", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		sseServer := NewSSEServer(mcpServer)

		handler := sseServer.ProtectedResourceMetadataEndpoint()
		assert.Nil(t, handler)
	})
}

func TestProtectedResourceMetadata_JSONSerialization(t *testing.T) {
	metadata := &ProtectedResourceMetadata{
		Resource:               "https://mcp.example.com",
		AuthorizationServers:   []string{"https://auth.example.com"},
		ScopesSupported:        []string{"read", "write"},
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "Test Server",
		ResourceDocumentation:  "https://docs.example.com",
		JWKSURI:                "https://mcp.example.com/.well-known/jwks.json",
	}

	data, err := json.Marshal(metadata)
	require.NoError(t, err)

	var got ProtectedResourceMetadata
	err = json.Unmarshal(data, &got)
	require.NoError(t, err)

	assert.Equal(t, metadata.Resource, got.Resource)
	assert.Equal(t, metadata.AuthorizationServers, got.AuthorizationServers)
	assert.Equal(t, metadata.ScopesSupported, got.ScopesSupported)
	assert.Equal(t, metadata.BearerMethodsSupported, got.BearerMethodsSupported)
	assert.Equal(t, metadata.ResourceName, got.ResourceName)
	assert.Equal(t, metadata.ResourceDocumentation, got.ResourceDocumentation)
	assert.Equal(t, metadata.JWKSURI, got.JWKSURI)
}

func TestProtectedResourceMetadata_OmitEmpty(t *testing.T) {
	metadata := &ProtectedResourceMetadata{
		Resource: "https://mcp.example.com",
	}

	data, err := json.Marshal(metadata)
	require.NoError(t, err)

	var raw map[string]any
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	assert.Contains(t, raw, "resource")
	assert.NotContains(t, raw, "authorization_servers")
	assert.NotContains(t, raw, "scopes_supported")
	assert.NotContains(t, raw, "bearer_methods_supported")
	assert.NotContains(t, raw, "resource_name")
	assert.NotContains(t, raw, "resource_documentation")
	assert.NotContains(t, raw, "jwks_uri")
}
