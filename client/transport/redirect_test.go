package transport

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

const redirectTestSecret = "secret-key"

// newRedirectAttacker returns a server that records every request it receives,
// and a URL for it whose host differs from the 127.0.0.1 used by httptest.
func newRedirectAttacker(t *testing.T) (*atomic.Int64, string) {
	t.Helper()
	var hits atomic.Int64
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(attacker.Close)
	return &hits, strings.Replace(attacker.URL, "127.0.0.1", "localhost", 1)
}

func newRedirectingServer(t *testing.T, location string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, location, status)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func redirectTestRequest() JSONRPCRequest {
	return JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(1),
		Method:  "tools/call",
		Params:  map[string]any{"name": "x"},
	}
}

func TestStreamableHTTP_RefusesCrossOriginRedirect(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{name: "302 Found", status: http.StatusFound},
		{name: "307 Temporary Redirect", status: http.StatusTemporaryRedirect},
		{name: "308 Permanent Redirect", status: http.StatusPermanentRedirect},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits, attackerURL := newRedirectAttacker(t)
			srv := newRedirectingServer(t, attackerURL+"/steal", tt.status)

			tr, err := NewStreamableHTTP(srv.URL,
				WithHTTPHeaders(map[string]string{"X-Api-Key": redirectTestSecret}))
			require.NoError(t, err)
			t.Cleanup(func() { _ = tr.Close() })

			_, err = tr.SendRequest(t.Context(), redirectTestRequest())
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrCrossOriginRedirect)
			assert.Zero(t, hits.Load(), "the redirect target must not be contacted")
		})
	}
}

func TestStreamableHTTP_FollowsSameOriginRedirect(t *testing.T) {
	var gotKey atomic.Value
	mux := http.NewServeMux()
	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/mcp", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		gotKey.Store(r.Header.Get("X-Api-Key"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tr, err := NewStreamableHTTP(srv.URL+"/old",
		WithHTTPHeaders(map[string]string{"X-Api-Key": redirectTestSecret}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })

	resp, err := tr.SendRequest(t.Context(), redirectTestRequest())
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, redirectTestSecret, gotKey.Load())
}

func TestStreamableHTTP_CustomClientKeepsItsRedirectPolicy(t *testing.T) {
	hits, attackerURL := newRedirectAttacker(t)
	srv := newRedirectingServer(t, attackerURL+"/elsewhere", http.StatusFound)

	tr, err := NewStreamableHTTP(srv.URL, WithHTTPBasicClient(&http.Client{}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })

	_, _ = tr.SendRequest(t.Context(), redirectTestRequest())
	assert.Equal(t, int64(1), hits.Load(), "a caller-supplied client must keep its own redirect policy")
}

func TestSSE_RefusesCrossOriginRedirect(t *testing.T) {
	hits, attackerURL := newRedirectAttacker(t)
	srv := newRedirectingServer(t, attackerURL+"/steal", http.StatusFound)

	tr, err := NewSSE(srv.URL, WithHeaders(map[string]string{"X-Api-Key": redirectTestSecret}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })

	err = tr.Start(t.Context())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCrossOriginRedirect)
	assert.Zero(t, hits.Load(), "the redirect target must not be contacted")
}

func TestSSE_RejectsEndpointOnOtherOrigin(t *testing.T) {
	tests := []struct {
		name     string
		endpoint func(host string) string
	}{
		{name: "scheme downgrade", endpoint: func(host string) string { return "http://" + host + "/message" }},
		{name: "other host", endpoint: func(string) string { return "https://localhost:1/message" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var posts atomic.Int64
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					posts.Add(1)
					w.WriteHeader(http.StatusAccepted)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("event: endpoint\ndata: " + tt.endpoint(r.Host) + "\n\n"))
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			t.Cleanup(srv.Close)

			tr, err := NewSSE(srv.URL, WithHTTPClient(srv.Client()), WithEndpointTimeout(200*time.Millisecond))
			require.NoError(t, err)
			t.Cleanup(func() { _ = tr.Close() })

			err = tr.Start(t.Context())
			require.Error(t, err, "an endpoint on another origin must not be accepted")
			assert.Contains(t, err.Error(), "timeout waiting for endpoint")
			assert.Zero(t, posts.Load())
		})
	}
}

func TestSSE_AcceptsEndpointOnSameOrigin(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: endpoint\ndata: /message\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	tr, err := NewSSE(srv.URL, WithHTTPClient(srv.Client()), WithEndpointTimeout(2*time.Second))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })

	require.NoError(t, tr.Start(t.Context()))
}

func TestSSE_CustomClientKeepsItsRedirectPolicy(t *testing.T) {
	hits, attackerURL := newRedirectAttacker(t)
	srv := newRedirectingServer(t, attackerURL+"/elsewhere", http.StatusFound)

	tr, err := NewSSE(srv.URL, WithHTTPClient(&http.Client{}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })

	_ = tr.Start(t.Context())
	assert.Equal(t, int64(1), hits.Load(), "a caller-supplied client must keep its own redirect policy")
}

func TestSameOriginRedirectPolicy(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		to      string
		via     int
		wantErr error
	}{
		{name: "same origin, other path", from: "https://a.example/mcp", to: "https://a.example/v2/mcp"},
		{name: "explicit default https port", from: "https://a.example/mcp", to: "https://a.example:443/mcp"},
		{name: "explicit default http port", from: "http://a.example:80/mcp", to: "http://a.example/mcp"},
		{name: "host case differs", from: "https://A.example/mcp", to: "https://a.EXAMPLE/mcp"},
		{name: "empty port is the default port", from: "https://a.example/mcp", to: "https://a.example:/mcp"},
		{name: "non-ASCII fold onto ASCII host", from: "https://i.example/mcp", to: "https://İ.example/mcp", wantErr: ErrCrossOriginRedirect},
		{name: "ASCII host to non-ASCII fold", from: "https://İ.example/mcp", to: "https://i.example/mcp", wantErr: ErrCrossOriginRedirect},
		{name: "non-ASCII is never folded", from: "https://k.example/mcp", to: "https://K.example/mcp", wantErr: ErrCrossOriginRedirect},
		{name: "other host", from: "https://a.example/mcp", to: "https://b.example/mcp", wantErr: ErrCrossOriginRedirect},
		{name: "subdomain", from: "https://a.example/mcp", to: "https://x.a.example/mcp", wantErr: ErrCrossOriginRedirect},
		{name: "other port", from: "http://a.example:8080/mcp", to: "http://a.example:9090/mcp", wantErr: ErrCrossOriginRedirect},
		{name: "scheme downgrade", from: "https://a.example/mcp", to: "http://a.example/mcp", wantErr: ErrCrossOriginRedirect},
		{name: "scheme upgrade", from: "http://a.example/mcp", to: "https://a.example/mcp", wantErr: ErrCrossOriginRedirect},
		{name: "internal address", from: "https://a.example/mcp", to: "http://169.254.169.254/latest", wantErr: ErrCrossOriginRedirect},
		{name: "too many redirects", from: "https://a.example/mcp", to: "https://a.example/mcp", via: maxRedirects, wantErr: ErrTooManyRedirects},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first, err := http.NewRequest(http.MethodPost, tt.from, nil)
			require.NoError(t, err)
			// net/http builds the next request from the Location with
			// URL.Parse, without NewRequest's empty-port cleanup.
			nextURL, err := url.Parse(tt.to)
			require.NoError(t, err)
			next := &http.Request{Method: http.MethodPost, URL: nextURL}

			via := []*http.Request{first}
			for len(via) < max(tt.via, 1) {
				via = append(via, first)
			}

			err = sameOriginRedirectPolicy(next, via)
			if tt.wantErr == nil {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}
