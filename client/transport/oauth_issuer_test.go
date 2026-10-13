package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// issuerTestServers starts an authorization server whose metadata, served for
// the issuer path tenant, declares issuer(asURL), and a protected resource
// whose metadata lists listed(asURL) as its authorization server. It returns
// the resource's URL and a counter of registration requests.
func issuerTestServers(t *testing.T, tenant string, listed, issuer func(asURL string) string) (string, *atomic.Int32) {
	t.Helper()
	var registrations atomic.Int32
	var as *httptest.Server
	as = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server" + tenant:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(AuthServerMetadata{
				Issuer:                issuer(as.URL + tenant),
				AuthorizationEndpoint: as.URL + "/authorize",
				TokenEndpoint:         as.URL + "/token",
				RegistrationEndpoint:  as.URL + "/register",
			})
		case "/register":
			registrations.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "registered-client"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(as.Close)

	var resourceURL string
	resource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/oauth-protected-resource" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              resourceURL,
			"authorization_servers": []string{listed(as.URL + tenant)},
		})
	}))
	t.Cleanup(resource.Close)
	resourceURL = resource.URL
	return resource.URL, &registrations
}

func newIssuerTestHandler(resourceURL string, skipIssuerValidation bool) *OAuthHandler {
	handler := NewOAuthHandler(OAuthConfig{
		RedirectURI:                  "http://localhost:8085/callback",
		TokenStore:                   NewMemoryTokenStore(),
		PKCEEnabled:                  true,
		SkipIssuerMetadataValidation: skipIssuerValidation,
	})
	handler.SetBaseURL(resourceURL)
	return handler
}

// Metadata fetched for one issuer that declares another must not be used
// (RFC 8414 §3.3): the client would otherwise register with, and send the
// user to, endpoints the named authorization server never published.
func TestOAuthHandler_RejectsMetadataForAnotherIssuer(t *testing.T) {
	same := func(asURL string) string { return asURL }
	attacker := func(string) string { return "https://attacker.example.com" }
	resourceURL, registrations := issuerTestServers(t, "", same, attacker)
	handler := newIssuerTestHandler(resourceURL, false)

	_, err := handler.GetServerMetadata(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `declares issuer "https://attacker.example.com"`)

	require.Error(t, handler.RegisterClient(t.Context(), "test-client"))
	assert.Zero(t, registrations.Load(), "the client must not use the rejected metadata")
}

func TestOAuthHandler_AcceptsMetadataForTheListedIssuer(t *testing.T) {
	same := func(asURL string) string { return asURL }
	withSlash := func(asURL string) string { return asURL + "/" }
	tests := []struct {
		name           string
		tenant         string
		listed, issuer func(string) string
	}{
		{name: "identical", listed: same, issuer: same},
		{name: "issuer with a path", tenant: "/tenant1", listed: same, issuer: same},
		{name: "listed with a trailing slash", listed: withSlash, issuer: same},
		{name: "issuer with a trailing slash", listed: same, issuer: withSlash},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resourceURL, registrations := issuerTestServers(t, tt.tenant, tt.listed, tt.issuer)
			handler := newIssuerTestHandler(resourceURL, false)

			_, err := handler.GetServerMetadata(t.Context())
			require.NoError(t, err)

			require.NoError(t, handler.RegisterClient(t.Context(), "test-client"))
			assert.Equal(t, int32(1), registrations.Load())
		})
	}
}

// SkipIssuerMetadataValidation lets a client work with an authorization
// server known to publish a mismatched issuer.
func TestOAuthHandler_SkipIssuerMetadataValidation(t *testing.T) {
	same := func(asURL string) string { return asURL }
	other := func(string) string { return "https://login.example.com/{tenantid}/v2.0" }
	resourceURL, registrations := issuerTestServers(t, "", same, other)
	handler := newIssuerTestHandler(resourceURL, true)

	_, err := handler.GetServerMetadata(t.Context())
	require.NoError(t, err)
	require.NoError(t, handler.RegisterClient(t.Context(), "test-client"))
	assert.Equal(t, int32(1), registrations.Load())
}
