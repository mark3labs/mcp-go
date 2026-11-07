package transport

import (
	"context"
	"io"
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

// newRedirectAttacker returns a server that counts the requests it receives,
// and a URL for it whose host differs from the 127.0.0.1 httptest uses.
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

// newRedirectingServer returns a server that answers every request with a
// redirect to location using the given status.
func newRedirectingServer(t *testing.T, location string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, location, status)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// redirectTestRequest returns the JSON-RPC request the redirect tests send.
func redirectTestRequest() JSONRPCRequest {
	return JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(1)),
		Method:  "tools/call",
		Params:  map[string]any{"name": "x"},
	}
}

// redirectStatuses lists the redirect statuses net/http follows.
var redirectStatuses = []struct {
	name   string
	status int
}{
	{name: "301 Moved Permanently", status: http.StatusMovedPermanently},
	{name: "302 Found", status: http.StatusFound},
	{name: "303 See Other", status: http.StatusSeeOther},
	{name: "307 Temporary Redirect", status: http.StatusTemporaryRedirect},
	{name: "308 Permanent Redirect", status: http.StatusPermanentRedirect},
}

// TestSameOriginRedirectPolicy checks which redirects the default transport clients follow.
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
		{name: "empty port is the default port", from: "https://a.example/mcp", to: "https://a.example:/mcp"},
		{name: "leading zeros in port", from: "https://a.example/mcp", to: "https://a.example:0443/mcp"},
		{name: "leading zeros in non-default port", from: "http://a.example:8080/mcp", to: "http://a.example:08080/mcp"},
		{name: "host case differs", from: "https://A.example/mcp", to: "https://a.EXAMPLE/mcp"},
		{name: "scheme case differs", from: "HTTPS://a.example/mcp", to: "https://a.example/mcp"},
		{name: "IPv6 same origin", from: "http://[::1]:8080/mcp", to: "http://[::1]:8080/v2"},
		{name: "IPv6 equivalent literals", from: "http://[::1]:8080/mcp", to: "http://[0:0::1]:8080/v2"},
		{name: "IPv6 full and compressed form", from: "http://[2001:db8::1]/mcp", to: "http://[2001:0db8:0000:0000:0000:0000:0000:0001]/v2"},
		{name: "IPv6 hex case", from: "http://[2001:DB8::A]/mcp", to: "http://[2001:db8::a]/v2"},
		{name: "IPv4-mapped IPv6 is not IPv4", from: "http://127.0.0.1/mcp", to: "http://[::ffff:127.0.0.1]/mcp", wantErr: ErrCrossOriginRedirect},
		{name: "IPv6 other address", from: "http://[::1]/mcp", to: "http://[::2]/mcp", wantErr: ErrCrossOriginRedirect},
		{name: "IPv6 other port", from: "http://[::1]:8080/mcp", to: "http://[::1]:9090/mcp", wantErr: ErrCrossOriginRedirect},
		{name: "dotted capital I is not folded onto i", from: "https://i.example/mcp", to: "https://İ.example/mcp", wantErr: ErrCrossOriginRedirect},
		{name: "i is not folded onto dotted capital I", from: "https://İ.example/mcp", to: "https://i.example/mcp", wantErr: ErrCrossOriginRedirect},
		{name: "Kelvin sign is not folded onto k", from: "https://k.example/mcp", to: "https://K.example/mcp", wantErr: ErrCrossOriginRedirect},
		{name: "other host", from: "https://a.example/mcp", to: "https://b.example/mcp", wantErr: ErrCrossOriginRedirect},
		{name: "subdomain", from: "https://a.example/mcp", to: "https://x.a.example/mcp", wantErr: ErrCrossOriginRedirect},
		{name: "parent domain", from: "https://x.a.example/mcp", to: "https://a.example/mcp", wantErr: ErrCrossOriginRedirect},
		{name: "other port", from: "http://a.example:8080/mcp", to: "http://a.example:9090/mcp", wantErr: ErrCrossOriginRedirect},
		{name: "scheme downgrade", from: "https://a.example/mcp", to: "http://a.example/mcp", wantErr: ErrCrossOriginRedirect},
		{name: "scheme upgrade", from: "http://a.example/mcp", to: "https://a.example/mcp", wantErr: ErrCrossOriginRedirect},
		{name: "cloud metadata address", from: "https://a.example/mcp", to: "http://169.254.169.254/latest/meta-data/", wantErr: ErrCrossOriginRedirect},
		{name: "loopback", from: "https://a.example/mcp", to: "http://127.0.0.1:6379/", wantErr: ErrCrossOriginRedirect},
		{name: "userinfo trick", from: "https://a.example/mcp", to: "https://a.example@b.example/mcp", wantErr: ErrCrossOriginRedirect},
		{name: "too many redirects", from: "https://a.example/mcp", to: "https://a.example/mcp", via: maxRedirects, wantErr: ErrTooManyRedirects},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sameOriginRedirectPolicy(redirectHop(t, http.MethodPost, tt.from, tt.to, tt.via))
			if tt.wantErr == nil {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, tt.wantErr)
			if tt.wantErr == ErrTooManyRedirects {
				assert.NotErrorIs(t, err, ErrCrossOriginRedirect)
			}
		})
	}
}

// TestOAuthRedirectPolicy checks which redirects the OAuth handler's default client follows.
func TestOAuthRedirectPolicy(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		from    string
		to      string
		via     int
		wantErr error
	}{
		{name: "discovery to another origin", method: http.MethodGet, from: "https://mcp.example/.well-known/oauth-protected-resource", to: "https://auth.example/.well-known/oauth-protected-resource"},
		{name: "discovery same origin", method: http.MethodGet, from: "https://auth.example/.well-known/openid-configuration", to: "https://auth.example/realms/x/.well-known/openid-configuration"},
		{name: "discovery over plain http", method: http.MethodGet, from: "http://localhost:8080/.well-known/oauth-authorization-server", to: "http://localhost:9090/.well-known/oauth-authorization-server"},
		{name: "discovery downgrade to http", method: http.MethodGet, from: "https://auth.example/.well-known/oauth-authorization-server", to: "http://auth.example/.well-known/oauth-authorization-server", wantErr: ErrInsecureRedirect},
		{name: "discovery too many redirects", method: http.MethodGet, from: "https://auth.example/a", to: "https://auth.example/b", via: maxRedirects, wantErr: ErrTooManyRedirects},
		{name: "token request same origin", method: http.MethodPost, from: "https://auth.example/token", to: "https://auth.example/oauth/token"},
		{name: "token request to another origin", method: http.MethodPost, from: "https://auth.example/token", to: "https://evil.example/token", wantErr: ErrCrossOriginRedirect},
		{name: "token request to subdomain", method: http.MethodPost, from: "https://auth.example/token", to: "https://x.auth.example/token", wantErr: ErrCrossOriginRedirect},
		{name: "token request downgrade", method: http.MethodPost, from: "https://auth.example/token", to: "http://auth.example/token", wantErr: ErrCrossOriginRedirect},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := oauthRedirectPolicy(redirectHop(t, tt.method, tt.from, tt.to, tt.via))
			if tt.wantErr == nil {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// redirectHop builds the arguments net/http passes to CheckRedirect for a
// redirect from "from" to "to", with "hops" earlier requests (at least one).
func redirectHop(t *testing.T, method, from, to string, hops int) (*http.Request, []*http.Request) {
	t.Helper()
	first, err := http.NewRequest(method, from, nil)
	require.NoError(t, err)
	// net/http builds the next request from the Location with URL.Parse,
	// without NewRequest's empty-port cleanup.
	nextURL, err := url.Parse(to)
	require.NoError(t, err)
	next := &http.Request{Method: method, URL: nextURL}
	via := []*http.Request{first}
	for len(via) < hops {
		via = append(via, first)
	}
	return next, via
}

// TestStreamableHTTP_RefusesCrossOriginRedirect checks that a request is not sent to another origin, whatever the redirect status.
func TestStreamableHTTP_RefusesCrossOriginRedirect(t *testing.T) {
	for _, tt := range redirectStatuses {
		t.Run(tt.name, func(t *testing.T) {
			hits, attackerURL := newRedirectAttacker(t)
			srv := newRedirectingServer(t, attackerURL+"/steal", tt.status)

			tr, err := NewStreamableHTTP(srv.URL,
				WithHTTPHeaders(map[string]string{"X-Api-Key": redirectTestSecret}),
				WithHTTPHeaderFunc(func(_ context.Context) map[string]string {
					return map[string]string{"Authorization": "Bearer " + redirectTestSecret}
				}))
			require.NoError(t, err)
			t.Cleanup(func() { _ = tr.Close() })

			_, err = tr.SendRequest(t.Context(), redirectTestRequest())
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrCrossOriginRedirect)
			assert.Zero(t, hits.Load(), "the redirect target must not be contacted")
		})
	}
}

// TestStreamableHTTP_SendNotificationRefusesCrossOriginRedirect checks that notifications get the same redirect policy as requests.
func TestStreamableHTTP_SendNotificationRefusesCrossOriginRedirect(t *testing.T) {
	hits, attackerURL := newRedirectAttacker(t)
	srv := newRedirectingServer(t, attackerURL+"/steal", http.StatusTemporaryRedirect)

	tr, err := NewStreamableHTTP(srv.URL,
		WithHTTPHeaders(map[string]string{"X-Api-Key": redirectTestSecret}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })

	err = tr.SendNotification(t.Context(), mcp.JSONRPCNotification{
		JSONRPC:      mcp.JSONRPC_VERSION,
		Notification: mcp.Notification{Method: "notifications/initialized"},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCrossOriginRedirect)
	assert.Zero(t, hits.Load(), "the redirect target must not be contacted")
}

// TestStreamableHTTP_FollowsSameOriginRedirect checks that a same-origin 308 is followed with its headers and body.
func TestStreamableHTTP_FollowsSameOriginRedirect(t *testing.T) {
	var gotKey atomic.Value
	var gotBody atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/mcp", http.StatusPermanentRedirect)
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		gotKey.Store(r.Header.Get("X-Api-Key"))
		var buf strings.Builder
		_, _ = io.Copy(&buf, r.Body)
		gotBody.Store(strings.Contains(buf.String(), `"tools/call"`))
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
	assert.True(t, gotBody.Load(), "a same-origin 308 must resend the request body")
}

// TestStreamableHTTP_CustomClientKeepsItsRedirectPolicy checks that a client from WithHTTPBasicClient is used unchanged.
func TestStreamableHTTP_CustomClientKeepsItsRedirectPolicy(t *testing.T) {
	hits, attackerURL := newRedirectAttacker(t)
	srv := newRedirectingServer(t, attackerURL+"/elsewhere", http.StatusFound)

	tr, err := NewStreamableHTTP(srv.URL, WithHTTPBasicClient(&http.Client{}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })

	_, _ = tr.SendRequest(t.Context(), redirectTestRequest())
	assert.Equal(t, int64(1), hits.Load(), "a caller-supplied client must keep its own redirect policy")
}

// TestStreamableHTTP_TimeoutOptionKeepsRedirectPolicy checks that WithHTTPTimeout does not replace the default client's redirect policy.
func TestStreamableHTTP_TimeoutOptionKeepsRedirectPolicy(t *testing.T) {
	hits, attackerURL := newRedirectAttacker(t)
	srv := newRedirectingServer(t, attackerURL+"/steal", http.StatusTemporaryRedirect)

	tr, err := NewStreamableHTTP(srv.URL, WithHTTPTimeout(5*time.Second))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })

	_, err = tr.SendRequest(t.Context(), redirectTestRequest())
	assert.ErrorIs(t, err, ErrCrossOriginRedirect)
	assert.Zero(t, hits.Load())
}

// TestSSE_RefusesCrossOriginRedirect checks that the SSE stream request is not sent to another origin.
func TestSSE_RefusesCrossOriginRedirect(t *testing.T) {
	for _, tt := range redirectStatuses {
		t.Run(tt.name, func(t *testing.T) {
			hits, attackerURL := newRedirectAttacker(t)
			srv := newRedirectingServer(t, attackerURL+"/steal", tt.status)

			tr, err := NewSSE(srv.URL, WithHeaders(map[string]string{"X-Api-Key": redirectTestSecret}))
			require.NoError(t, err)
			t.Cleanup(func() { _ = tr.Close() })

			err = tr.Start(t.Context())
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrCrossOriginRedirect)
			assert.Zero(t, hits.Load(), "the redirect target must not be contacted")
		})
	}
}

// TestSSE_RejectsEndpointOnOtherOrigin checks that an endpoint event naming another scheme, host or port is ignored.
func TestSSE_RejectsEndpointOnOtherOrigin(t *testing.T) {
	tests := []struct {
		name     string
		endpoint func(host string) string
	}{
		{name: "scheme downgrade", endpoint: func(host string) string { return "http://" + host + "/message" }},
		{name: "other host", endpoint: func(string) string { return "https://localhost:1/message" }},
		{name: "other port", endpoint: func(host string) string {
			h, _, _ := strings.Cut(host, ":")
			return "https://" + h + ":1/message"
		}},
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

// TestSSE_AcceptsEndpointOnSameOrigin checks that a relative endpoint on the connection's origin is accepted.
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

// TestSSE_CustomClientKeepsItsRedirectPolicy checks that a client from WithHTTPClient is used unchanged.
func TestSSE_CustomClientKeepsItsRedirectPolicy(t *testing.T) {
	hits, attackerURL := newRedirectAttacker(t)
	srv := newRedirectingServer(t, attackerURL+"/elsewhere", http.StatusFound)

	tr, err := NewSSE(srv.URL, WithHTTPClient(&http.Client{}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })

	_ = tr.Start(t.Context())
	assert.Equal(t, int64(1), hits.Load(), "a caller-supplied client must keep its own redirect policy")
}

// TestOAuthHandler_DefaultClientRefusesCrossOriginTokenRedirect checks that a token request body never reaches another origin.
func TestOAuthHandler_DefaultClientRefusesCrossOriginTokenRedirect(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
	}{
		{name: "307 Temporary Redirect", status: http.StatusTemporaryRedirect},
		{name: "308 Permanent Redirect", status: http.StatusPermanentRedirect},
	} {
		t.Run(tt.name, func(t *testing.T) {
			hits, attackerURL := newRedirectAttacker(t)
			srv := newRedirectingServer(t, attackerURL+"/token", tt.status)

			h := NewOAuthHandler(OAuthConfig{})
			form := url.Values{"grant_type": {"refresh_token"}, "client_secret": {redirectTestSecret}}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/token",
				strings.NewReader(form.Encode()))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			resp, err := h.httpClient.Do(req)
			if resp != nil {
				_ = resp.Body.Close()
			}
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrCrossOriginRedirect)
			assert.Zero(t, hits.Load(), "the token request body must not reach another origin")
		})
	}
}

// TestOAuthHandler_DefaultClientFollowsDiscoveryRedirect checks that metadata discovery may still be redirected to another origin.
func TestOAuthHandler_DefaultClientFollowsDiscoveryRedirect(t *testing.T) {
	var hits atomic.Int64
	metadata := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(metadata.Close)
	otherOrigin := strings.Replace(metadata.URL, "127.0.0.1", "localhost", 1)
	srv := newRedirectingServer(t, otherOrigin+"/.well-known/oauth-authorization-server", http.StatusFound)

	h := NewOAuthHandler(OAuthConfig{})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		srv.URL+"/.well-known/oauth-authorization-server", nil)
	require.NoError(t, err)

	resp, err := h.httpClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int64(1), hits.Load())
}
