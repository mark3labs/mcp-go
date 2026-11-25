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

// RFC 6749 section 3.1: the authorization endpoint may carry a query
// component, which must be retained when adding the request parameters.
func TestGetAuthorizationURL_EndpointWithQuery(t *testing.T) {
	var serverURL string
	as := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 serverURL,
			"authorization_endpoint": serverURL + "/authorize?tenant=acme",
			"token_endpoint":         serverURL + "/token",
		})
	}))
	serverURL = as.URL
	t.Cleanup(as.Close)

	h := NewOAuthHandler(OAuthConfig{
		ClientID:              "public-client",
		RedirectURI:           "http://127.0.0.1:33418/callback",
		TokenStore:            NewMemoryTokenStore(),
		AuthServerMetadataURL: as.URL + "/.well-known/oauth-authorization-server",
	})

	authURL, err := h.GetAuthorizationURL(t.Context(), "st", "")
	require.NoError(t, err)

	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	query := parsed.Query()
	assert.Equal(t, "/authorize", parsed.Path)
	assert.Equal(t, "acme", query.Get("tenant"))
	assert.Equal(t, "code", query.Get("response_type"))
	assert.Equal(t, "public-client", query.Get("client_id"))
	assert.Equal(t, "st", query.Get("state"))
}
