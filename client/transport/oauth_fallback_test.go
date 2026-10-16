package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOAuthHandler_DefaultEndpointsDiscardURLComponents(t *testing.T) {
	tests := []struct {
		name   string
		suffix string
	}{
		{name: "origin"},
		{name: "path", suffix: "/mcp"},
		{name: "query", suffix: "/mcp?tenant=acme"},
		{name: "empty query", suffix: "/mcp?"},
		{name: "fragment", suffix: "/mcp#section"},
		{name: "query and fragment", suffix: "/mcp?tenant=acme#section"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/token" {
					http.NotFound(w, r)
					return
				}
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Empty(t, r.URL.RawQuery)
				assert.Equal(t, "authorization_code", r.FormValue("grant_type"))
				assert.Equal(t, "test-code", r.FormValue("code"))
				assert.Equal(t, "test-verifier", r.FormValue("code_verifier"))
				w.Header().Set("Content-Type", "application/json")
				assert.NoError(t, json.NewEncoder(w).Encode(Token{
					AccessToken: "test-token",
					TokenType:   "Bearer",
					ExpiresIn:   3600,
				}))
			}))
			defer server.Close()

			store := NewMemoryTokenStore()
			handler := NewOAuthHandler(OAuthConfig{
				ClientID:    "test-client",
				RedirectURI: "http://localhost/callback",
				TokenStore:  store,
				PKCEEnabled: true,
			})
			handler.SetBaseURL(server.URL + tt.suffix)

			metadata, err := handler.GetServerMetadata(t.Context())
			require.NoError(t, err)
			assert.Equal(t, server.URL, metadata.Issuer)
			assert.Equal(t, server.URL+"/authorize", metadata.AuthorizationEndpoint)
			assert.Equal(t, server.URL+"/token", metadata.TokenEndpoint)
			assert.Equal(t, server.URL+"/register", metadata.RegistrationEndpoint)

			authorizationURL, err := handler.GetAuthorizationURL(t.Context(), "test-state", "test-challenge")
			require.NoError(t, err)
			parsedURL, err := url.Parse(authorizationURL)
			require.NoError(t, err)
			assert.Equal(t, "/authorize", parsedURL.Path)
			assert.Empty(t, parsedURL.Fragment)
			assert.Equal(t, "test-client", parsedURL.Query().Get("client_id"))
			assert.Equal(t, "test-state", parsedURL.Query().Get("state"))

			require.NoError(t, handler.ProcessAuthorizationResponse(t.Context(), "test-code", "test-state", "test-verifier"))
			token, err := store.GetToken(t.Context())
			require.NoError(t, err)
			assert.Equal(t, "test-token", token.AccessToken)
		})
	}
}
