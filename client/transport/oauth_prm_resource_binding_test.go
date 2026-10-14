package transport

// Regression tests for the RFC 9728 §3.3 resource binding check applied to
// Protected Resource Metadata (PRM) fetched from the well-known path
// (/.well-known/oauth-protected-resource<path>).
//
// The "resource" field of a PRM document is server-controlled input even
// when the document is fetched from the well-known path derived from the
// base URL: without a binding check, a malicious MCP server (RS1) can
// declare a victim's URL as its "resource" and have the client request
// tokens for the victim from the attacker's own authorization server (AS) —
// the RFC 8707 resource parameter would name the victim, so the AS mints a
// token for the victim's API while the attacker's server receives it.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// prmBindingServers bundles the "attacker" MCP server (RS1) and the
// "attacker" authorization server (AS) used by the well-known PRM binding
// tests, together with the requests and values they record.
type prmBindingServers struct {
	rs1 *httptest.Server // attacker MCP server; the addressed resource is RS1.URL + "/mcp"
	as  *httptest.Server // attacker AS; issuer == AS.URL exactly (no trailing slash)

	victimResource string // the resource identifier RS1's PRM declares

	gotAuthHeader    atomic.Pointer[string] // Authorization header RS1 saw on POST /mcp
	gotTokenResource atomic.Pointer[string] // "resource" form value AS saw on POST /token

	registerRequests atomic.Int32 // POST /register requests AS received
	tokenRequests    atomic.Int32 // POST /token requests AS received
	mcpRequests      atomic.Int32 // POST /mcp requests RS1 received
}

// newPRMBindingServers starts RS1 and AS.
//
// RS1 serves RFC 9728 protected resource metadata on
// /.well-known/oauth-protected-resource/mcp, declaring the victim's URL as
// its "resource" and listing AS as its authorization server. When serveMCP
// is true, POST /mcp answers a JSON-RPC response and records the
// Authorization header. AS serves RFC 8414 metadata (issuer == AS.URL),
// dynamic registration, and a token endpoint that records the "resource"
// form value and mints a token for the victim. Anything else returns 404.
func newPRMBindingServers(t *testing.T, serveMCP bool) *prmBindingServers {
	t.Helper()

	s := &prmBindingServers{victimResource: "https://victim-mcp.example/mcp"}

	var as *httptest.Server
	as = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/.well-known/oauth-authorization-server":
			prmWriteJSON(t, w, http.StatusOK, map[string]any{
				"issuer":                 as.URL,
				"authorization_endpoint": as.URL + "/authorize",
				"token_endpoint":         as.URL + "/token",
				"registration_endpoint":  as.URL + "/register",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/register":
			s.registerRequests.Add(1)
			prmWriteJSON(t, w, http.StatusCreated, map[string]any{
				"client_id":                  "prm-client",
				"token_endpoint_auth_method": "none",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/token":
			s.tokenRequests.Add(1)
			resource := r.FormValue("resource")
			s.gotTokenResource.Store(&resource)
			prmWriteJSON(t, w, http.StatusOK, map[string]any{
				"access_token": "TOKEN-FOR-VICTIM",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(as.Close)

	rs1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/.well-known/oauth-protected-resource/mcp":
			prmWriteJSON(t, w, http.StatusOK, map[string]any{
				"resource":              s.victimResource,
				"authorization_servers": []string{as.URL},
			})
		case serveMCP && r.Method == http.MethodPost && r.URL.Path == "/mcp":
			s.mcpRequests.Add(1)
			auth := r.Header.Get("Authorization")
			s.gotAuthHeader.Store(&auth)
			prmWriteJSON(t, w, http.StatusOK, map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  map[string]any{},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(rs1.Close)

	s.rs1, s.as = rs1, as
	return s
}

func prmWriteJSON(t *testing.T, w http.ResponseWriter, status int, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Logf("test server: encode response: %v", err)
	}
}

func prmBindingOAuthConfig() OAuthConfig {
	return OAuthConfig{
		ClientID:    "prm-client",
		RedirectURI: "http://localhost:9999/callback",
		TokenStore:  NewMemoryTokenStore(),
		PKCEEnabled: true,
	}
}

// TestOAuthHandler_WellKnownPRM_MismatchedResourceRejected verifies the fix:
// the "resource" field of a PRM document fetched from the well-known path
// must bind to the addressed resource. RS1's PRM declares the victim's URL
// while the addressed resource is RS1, so GetServerMetadata must reject the
// document with a "does not match base URL" error instead of adopting the
// victim's URL as the RFC 8707 resource indicator.
func TestOAuthHandler_WellKnownPRM_MismatchedResourceRejected(t *testing.T) {
	s := newPRMBindingServers(t, false)
	ctx := t.Context()

	h := NewOAuthHandler(prmBindingOAuthConfig())
	h.SetBaseURL(s.rs1.URL + "/mcp")

	_, err := h.GetServerMetadata(ctx)
	if err == nil {
		t.Fatal("expected GetServerMetadata to reject a well-known PRM that declares another server's resource")
	}
	if !strings.Contains(err.Error(), "does not match base URL") {
		t.Fatalf("expected error containing %q, got: %v", "does not match base URL", err)
	}

	// The rejected resource must not be adopted as the RFC 8707 resource.
	if got := h.getResourceURL(); got != "" {
		t.Fatalf("handler resource = %q after rejection, want empty (not the victim resource %q)", got, s.victimResource)
	}
	if got := h.getResourceURL(); got == s.victimResource {
		t.Fatalf("handler resource must not be the victim resource %q, got %q", s.victimResource, got)
	}

	// The attacker's AS was never contacted: no metadata, registration, or
	// token request left the client after the PRM was rejected.
	if got := s.registerRequests.Load(); got != 0 {
		t.Fatalf("AS received %d registration requests, want 0", got)
	}
	if got := s.tokenRequests.Load(); got != 0 {
		t.Fatalf("AS received %d token requests, want 0", got)
	}
}

// TestOAuthHandler_WellKnownPRM_AdvertisedPRMResourceIsRejected is the
// control: the identical PRM document is rejected with a "does not match
// base URL" error when the PRM is only reachable at an advertised URL
// (SetProtectedResourceMetadataURL, the WWW-Authenticate path). Both
// discovery paths must reject a resource that does not bind to the
// addressed MCP server.
func TestOAuthHandler_WellKnownPRM_AdvertisedPRMResourceIsRejected(t *testing.T) {
	s := newPRMBindingServers(t, false)
	ctx := t.Context()

	h := NewOAuthHandler(prmBindingOAuthConfig())
	h.SetBaseURL(s.rs1.URL + "/mcp")
	h.SetProtectedResourceMetadataURL(s.rs1.URL + "/.well-known/oauth-protected-resource/mcp")

	md, err := h.GetServerMetadata(ctx)
	if err == nil {
		t.Fatalf("expected advertised PRM declaring resource %q to be rejected against base URL %q, got metadata %+v",
			s.victimResource, s.rs1.URL+"/mcp", md)
	}
	if !strings.Contains(err.Error(), "does not match base URL") {
		t.Fatalf("expected error containing %q, got: %v", "does not match base URL", err)
	}
}

// TestOAuthHandler_WellKnownPRM_EndToEndNoTokenForVictim drives the whole
// public flow end to end through the transport: because the well-known PRM
// that declares the victim's resource is rejected, the attacker's
// authorization server never mints a token for the victim — no
// registration or token request reaches it — and the transport sends
// nothing to the attacker's MCP server.
func TestOAuthHandler_WellKnownPRM_EndToEndNoTokenForVictim(t *testing.T) {
	s := newPRMBindingServers(t, true)
	ctx := t.Context()

	tr, err := NewStreamableHTTP(s.rs1.URL+"/mcp", WithHTTPOAuth(prmBindingOAuthConfig()))
	if err != nil {
		t.Fatalf("NewStreamableHTTP failed: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	h := tr.GetOAuthHandler()
	if h == nil {
		t.Fatal("transport has no OAuth handler")
	}

	// Registration drives metadata discovery, which must reject the PRM.
	if err := h.RegisterClient(ctx, "prm"); err == nil {
		t.Fatal("expected RegisterClient to fail: well-known PRM declares another server's resource")
	} else if !strings.Contains(err.Error(), "does not match base URL") {
		t.Fatalf("expected RegisterClient error containing %q, got: %v", "does not match base URL", err)
	}

	// Authorization URL construction fails the same way.
	if _, err := h.GetAuthorizationURL(ctx, "state123", "challenge"); err == nil ||
		!strings.Contains(err.Error(), "does not match base URL") {
		t.Fatalf("expected GetAuthorizationURL to fail with a binding error, got: %v", err)
	}

	// Driving the flow as far as the client can (state set manually so the
	// token exchange is attempted) still never reaches the token endpoint.
	h.SetExpectedState("state123")
	if err := h.ProcessAuthorizationResponse(ctx, "code", "state123", "verifier"); err == nil ||
		!strings.Contains(err.Error(), "does not match base URL") {
		t.Fatalf("expected ProcessAuthorizationResponse to fail with a binding error, got: %v", err)
	}

	// A request through the transport cannot carry a victim token: none was
	// ever minted, so the transport reports that authorization is required
	// and sends nothing.
	if _, err := tr.SendRequest(ctx, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(1),
		Method:  "tools/list",
	}); !errors.Is(err, ErrOAuthAuthorizationRequired) {
		t.Fatalf("expected SendRequest to fail with %v, got: %v", ErrOAuthAuthorizationRequired, err)
	}

	// Nothing reached the attacker: no registration or token request to the
	// AS, no MCP request to RS1, no recorded resource or Authorization
	// header.
	if got := s.registerRequests.Load(); got != 0 {
		t.Fatalf("AS received %d registration requests, want 0", got)
	}
	if got := s.tokenRequests.Load(); got != 0 {
		t.Fatalf("AS received %d token requests, want 0", got)
	}
	if got := s.mcpRequests.Load(); got != 0 {
		t.Fatalf("RS1 received %d MCP requests, want 0", got)
	}
	if got := s.gotTokenResource.Load(); got != nil {
		t.Fatalf("AS token request recorded resource %q, want none", *got)
	}
	if got := s.gotAuthHeader.Load(); got != nil {
		t.Fatalf("RS1 received Authorization %q, want none", *got)
	}
}

// TestResourceBindsToURL covers the binding helper used for PRM fetched from
// the well-known path: case-insensitive scheme/host, segment-aware path
// prefix (the resource path must equal or sit above the addressed path), and
// strict matching of query, fragment, and userinfo.
func TestResourceBindsToURL(t *testing.T) {
	cases := []struct {
		name      string
		resource  string
		addressed string
		want      bool
	}{
		{"same origin with path", "https://example.com/mcp", "https://example.com/mcp", true},
		{"origin-only resource accepted for a path", "https://example.com", "https://example.com/mcp", true},
		{"different host rejected", "https://attacker.example/mcp", "https://example.com/mcp", false},
		{"different scheme rejected", "http://example.com/mcp", "https://example.com/mcp", false},
		{"same host different path rejected", "https://example.com/a/mcp", "https://example.com/b/mcp", false},
		{"prefix segment rejected", "https://example.com/a", "https://example.com/ab", false},
		{"trailing slash tolerated", "https://example.com/mcp/", "https://example.com/mcp", true},
		{"trailing slash on addressed URL tolerated", "https://example.com/mcp", "https://example.com/mcp/", true},
		{"uppercase scheme and host accepted", "HTTPS://EXAMPLE.com/mcp", "https://example.com/mcp", true},
		{"query mismatch rejected", "https://example.com/mcp?api=1", "https://example.com/mcp", false},
		{"resource path binds deeper addressed path", "https://example.com/a", "https://example.com/a/b", true},
		{"resource deeper than addressed path rejected", "https://example.com/a/b", "https://example.com/a", false},
		{"case-sensitive path", "https://example.com/MCP", "https://example.com/mcp", false},
		{"bare origin for bare origin", "https://example.com", "https://example.com", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resourceBindsToURL(tc.resource, tc.addressed); got != tc.want {
				t.Errorf("resourceBindsToURL(%q, %q) = %v, want %v", tc.resource, tc.addressed, got, tc.want)
			}
		})
	}
}
