package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCORSConfig_DisabledByDefault(t *testing.T) {
	t.Parallel()

	var c *CORSConfig
	assert.False(t, c.enabled(), "nil config must be disabled")

	c = &CORSConfig{}
	assert.False(t, c.enabled(), "config without origins must be disabled")

	c = &CORSConfig{AllowedOrigins: []string{"https://example.com"}}
	assert.True(t, c.enabled(), "config with origins must be enabled")
}

func TestCORSConfig_ResolveOrigin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cfg         CORSConfig
		origin      string
		wantAllowed string
	}{
		{
			name:        "exact match",
			cfg:         CORSConfig{AllowedOrigins: []string{"https://example.com"}},
			origin:      "https://example.com",
			wantAllowed: "https://example.com",
		},
		{
			name:        "no match returns empty",
			cfg:         CORSConfig{AllowedOrigins: []string{"https://example.com"}},
			origin:      "https://attacker.com",
			wantAllowed: "",
		},
		{
			name:        "wildcard without credentials returns *",
			cfg:         CORSConfig{AllowedOrigins: []string{"*"}},
			origin:      "https://anywhere.com",
			wantAllowed: "*",
		},
		{
			name:        "wildcard with credentials echoes origin",
			cfg:         CORSConfig{AllowedOrigins: []string{"*"}, AllowCredentials: true},
			origin:      "https://anywhere.com",
			wantAllowed: "https://anywhere.com",
		},
		{
			name:        "wildcard with credentials but no origin returns empty",
			cfg:         CORSConfig{AllowedOrigins: []string{"*"}, AllowCredentials: true},
			origin:      "",
			wantAllowed: "",
		},
		{
			name:        "multiple origins picks the matching one",
			cfg:         CORSConfig{AllowedOrigins: []string{"https://a.com", "https://b.com"}},
			origin:      "https://b.com",
			wantAllowed: "https://b.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.cfg.resolveOrigin(tt.origin)
			assert.Equal(t, tt.wantAllowed, got)
		})
	}
}

func TestCORSOptionHelpers(t *testing.T) {
	t.Parallel()

	cfg := &CORSConfig{}
	for _, opt := range []CORSOption{
		WithCORSAllowedOrigins("https://example.com", "https://other.com"),
		WithCORSAllowedMethods(http.MethodGet, http.MethodPost),
		WithCORSAllowedHeaders("X-Custom", "Authorization"),
		WithCORSExposedHeaders("X-Trace"),
		WithCORSAllowCredentials(),
		WithCORSMaxAge(600),
	} {
		opt(cfg)
	}

	assert.Equal(t, []string{"https://example.com", "https://other.com"}, cfg.AllowedOrigins)
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, cfg.AllowedMethods)
	assert.Equal(t, []string{"X-Custom", "Authorization"}, cfg.AllowedHeaders)
	assert.Equal(t, []string{"X-Trace"}, cfg.ExposedHeaders)
	assert.True(t, cfg.AllowCredentials)
	assert.Equal(t, 600, cfg.MaxAge)
}

func TestCORSOptionHelpers_ReplaceOnRepeatedCall(t *testing.T) {
	t.Parallel()

	cfg := &CORSConfig{AllowedOrigins: []string{"https://stale.com"}}
	WithCORSAllowedOrigins("https://fresh.com")(cfg)
	assert.Equal(t, []string{"https://fresh.com"}, cfg.AllowedOrigins)
}

func TestStreamableHTTP_CORS_Preflight(t *testing.T) {
	t.Parallel()

	mcp := NewMCPServer("test", "1.0.0")
	srv := NewStreamableHTTPServer(mcp,
		WithStreamableHTTPCORS(
			WithCORSAllowedOrigins("https://example.com"),
			WithCORSAllowedMethods(http.MethodPost, http.MethodGet, http.MethodDelete, http.MethodOptions),
			WithCORSAllowedHeaders("Content-Type", "Mcp-Session-Id"),
			WithCORSAllowCredentials(),
			WithCORSMaxAge(120),
		),
	)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodOptions, ts.URL, nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "Content-Type")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, "https://example.com", resp.Header.Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", resp.Header.Get("Access-Control-Allow-Credentials"))
	assert.Equal(t, "120", resp.Header.Get("Access-Control-Max-Age"))
	assert.Contains(t, resp.Header.Get("Access-Control-Allow-Methods"), http.MethodPost)
	assert.Contains(t, resp.Header.Get("Access-Control-Allow-Headers"), "Mcp-Session-Id")
	assert.Equal(t, "Origin", resp.Header.Get("Vary"))
}

func TestStreamableHTTP_CORS_PreflightDisallowedOrigin(t *testing.T) {
	t.Parallel()

	mcp := NewMCPServer("test", "1.0.0")
	srv := NewStreamableHTTPServer(mcp,
		WithStreamableHTTPCORS(WithCORSAllowedOrigins("https://example.com")),
	)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodOptions, ts.URL, nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "https://attacker.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	// Preflight is still answered (browser will block), but no Allow-Origin
	// header is emitted because the origin is not in the allow list.
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestStreamableHTTP_CORS_SimpleRequest(t *testing.T) {
	t.Parallel()

	mcp := NewMCPServer("test", "1.0.0")
	srv := NewStreamableHTTPServer(mcp,
		WithStreamableHTTPCORS(
			WithCORSAllowedOrigins("https://example.com"),
			WithCORSExposedHeaders("Mcp-Session-Id", "X-Trace"),
		),
	)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL, body)
	require.NoError(t, err)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	assert.Equal(t, "https://example.com", resp.Header.Get("Access-Control-Allow-Origin"))
	exposed := resp.Header.Get("Access-Control-Expose-Headers")
	assert.Contains(t, exposed, "Mcp-Session-Id")
	assert.Contains(t, exposed, "X-Trace")
	assert.Equal(t, "Origin", resp.Header.Get("Vary"))
}

func TestStreamableHTTP_CORS_NoConfigPassesPreflightThrough(t *testing.T) {
	t.Parallel()

	mcp := NewMCPServer("test", "1.0.0")
	srv := NewStreamableHTTPServer(mcp)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodOptions, ts.URL, nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	// With CORS disabled, OPTIONS is not handled specially and falls
	// through to ServeHTTP's default branch (404).
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestStreamableHTTP_CORS_WildcardWithCredentialsEchoesOrigin(t *testing.T) {
	t.Parallel()

	mcp := NewMCPServer("test", "1.0.0")
	srv := NewStreamableHTTPServer(mcp,
		WithStreamableHTTPCORS(
			WithCORSAllowedOrigins("*"),
			WithCORSAllowCredentials(),
		),
	)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodOptions, ts.URL, nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "https://anywhere.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	assert.Equal(t, "https://anywhere.com", resp.Header.Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", resp.Header.Get("Access-Control-Allow-Credentials"))
}

func TestSSE_CORS_Preflight(t *testing.T) {
	t.Parallel()

	mcp := NewMCPServer("test", "1.0.0")
	srv := NewSSEServer(mcp,
		WithSSECORS(
			WithCORSAllowedOrigins("https://example.com"),
		),
	)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodOptions, ts.URL+"/sse", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, "https://example.com", resp.Header.Get("Access-Control-Allow-Origin"))
	assert.Contains(t, resp.Header.Get("Access-Control-Allow-Methods"), http.MethodGet)
}

func TestSSE_CORS_DefaultWildcardPreservedWhenDisabled(t *testing.T) {
	t.Parallel()

	mcp := NewMCPServer("test", "1.0.0")
	ts := NewTestServer(mcp)
	t.Cleanup(ts.Close)

	// Issue a GET to the SSE endpoint and look for the default behavior.
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/sse", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	// Without an explicit CORS config, handleSSE preserves the historical
	// "*" default to avoid breaking existing browser-based clients.
	assert.Equal(t, "*", resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestSSE_CORS_OverridesDefaultWildcard(t *testing.T) {
	t.Parallel()

	mcp := NewMCPServer("test", "1.0.0")
	srv := NewSSEServer(mcp,
		WithSSECORS(WithCORSAllowedOrigins("https://example.com")),
	)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/sse", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Origin", "https://example.com")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	assert.Equal(t, "https://example.com", resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestSSE_CORS_HandlersHonorConfig(t *testing.T) {
	t.Parallel()

	mcp := NewMCPServer("test", "1.0.0")
	srv := NewSSEServer(mcp,
		WithSSECORS(WithCORSAllowedOrigins("https://example.com")),
	)

	mux := http.NewServeMux()
	mux.Handle("/custom/sse", srv.SSEHandler())
	mux.Handle("/custom/message", srv.MessageHandler())
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodOptions, ts.URL+"/custom/message", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, "https://example.com", resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestCORSConfig_Clone(t *testing.T) {
	t.Parallel()

	orig := &CORSConfig{
		AllowedOrigins:   []string{"a"},
		AllowedMethods:   []string{"GET"},
		AllowedHeaders:   []string{"X-A"},
		ExposedHeaders:   []string{"X-B"},
		AllowCredentials: true,
		MaxAge:           5,
	}
	cp := orig.clone()
	cp.AllowedOrigins[0] = "mutated"
	assert.Equal(t, "a", orig.AllowedOrigins[0], "clone must not share slice with original")

	var nilCfg *CORSConfig
	assert.Nil(t, nilCfg.clone())
}
