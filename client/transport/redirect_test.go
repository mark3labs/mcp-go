package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestSameOriginRedirectPolicy(t *testing.T) {
	mustReq := func(raw string) *http.Request {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		return &http.Request{URL: u}
	}

	tests := []struct {
		name    string
		origin  string
		target  string
		wantErr bool
	}{
		{name: "same origin path change", origin: "https://mcp.example.com/mcp", target: "https://mcp.example.com/mcp/"},
		{name: "explicit default port", origin: "https://mcp.example.com/mcp", target: "https://mcp.example.com:443/v2"},
		{name: "host case differs", origin: "https://mcp.example.com/mcp", target: "https://MCP.example.com/v2"},
		{name: "other host", origin: "https://mcp.example.com/mcp", target: "https://evil.example.net/", wantErr: true},
		{name: "subdomain", origin: "https://example.com/mcp", target: "https://internal.example.com/", wantErr: true},
		{name: "internal address", origin: "https://mcp.example.com/mcp", target: "http://169.254.169.254/latest/meta-data/", wantErr: true},
		{name: "scheme downgrade", origin: "https://mcp.example.com/mcp", target: "http://mcp.example.com/mcp", wantErr: true},
		{name: "other port", origin: "http://localhost:8080/mcp", target: "http://localhost:9090/mcp", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sameOriginRedirectPolicy(mustReq(tt.target), []*http.Request{mustReq(tt.origin)})
			if tt.wantErr {
				assert.ErrorIs(t, err, ErrCrossOriginRedirect)
			} else {
				assert.NoError(t, err)
			}
		})
	}

	t.Run("redirect limit", func(t *testing.T) {
		via := make([]*http.Request, maxRedirects)
		for i := range via {
			via[i] = mustReq("https://mcp.example.com/mcp")
		}
		assert.Error(t, sameOriginRedirectPolicy(mustReq("https://mcp.example.com/mcp"), via))
	})
}

// redirectTarget counts the requests that reach it.
type redirectTarget struct {
	srv  *httptest.Server
	hits atomic.Int32
}

func newRedirectTarget(t *testing.T) *redirectTarget {
	rt := &redirectTarget{}
	rt.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rt.hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(rt.srv.Close)
	return rt
}

func newRedirectingServer(t *testing.T, location string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, location, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestStreamableHTTP_RefusesCrossOriginRedirect(t *testing.T) {
	target := newRedirectTarget(t)
	redirector := newRedirectingServer(t, target.srv.URL+"/steal")

	trans, err := NewStreamableHTTP(redirector.URL,
		WithHTTPHeaders(map[string]string{
			"X-Api-Key":     "secret-key",
			"Authorization": "Bearer secret-token",
		}))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	_, err = trans.SendRequest(ctx, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(int64(1)),
		Method:  "initialize",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCrossOriginRedirect)
	assert.Zero(t, target.hits.Load(), "redirect target must not be contacted")
}

func TestSSE_RefusesCrossOriginRedirect(t *testing.T) {
	target := newRedirectTarget(t)
	redirector := newRedirectingServer(t, target.srv.URL+"/steal")

	trans, err := NewSSE(redirector.URL,
		WithHeaders(map[string]string{"X-Api-Key": "secret-key"}))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	err = trans.Start(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCrossOriginRedirect)
	assert.Zero(t, target.hits.Load(), "redirect target must not be contacted")
}

func TestStreamableHTTP_FollowsSameOriginRedirect(t *testing.T) {
	var gotKey atomic.Value
	mux := http.NewServeMux()
	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/mcp", http.StatusPermanentRedirect)
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		gotKey.Store(r.Header.Get("X-Api-Key"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	trans, err := NewStreamableHTTP(srv.URL+"/old",
		WithHTTPHeaders(map[string]string{"X-Api-Key": "secret-key"}))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	resp, err := trans.SendRequest(ctx, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(int64(1)),
		Method:  "initialize",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "secret-key", gotKey.Load())
}
