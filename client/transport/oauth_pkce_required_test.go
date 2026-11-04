package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPKCETestHandler(t *testing.T, pkceEnabled bool) (*OAuthHandler, *atomic.Int64) {
	t.Helper()
	var tokenRequests atomic.Int64
	var serverURL string
	as := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                 serverURL,
				"authorization_endpoint": serverURL + "/authorize",
				"token_endpoint":         serverURL + "/token",
			})
		case "/token":
			tokenRequests.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "AT", "token_type": "Bearer", "expires_in": 3600})
		}
	}))
	serverURL = as.URL
	t.Cleanup(as.Close)

	return NewOAuthHandler(OAuthConfig{
		ClientID:              "public-client",
		RedirectURI:           "http://127.0.0.1:33418/callback",
		TokenStore:            NewMemoryTokenStore(),
		AuthServerMetadataURL: as.URL + "/.well-known/oauth-authorization-server",
		PKCEEnabled:           pkceEnabled,
	}), &tokenRequests
}

func TestGetAuthorizationURL_PKCERequiresChallenge(t *testing.T) {
	h, _ := newPKCETestHandler(t, true)

	authURL, err := h.GetAuthorizationURL(t.Context(), "st", "")
	require.ErrorIs(t, err, ErrPKCERequired)
	assert.Empty(t, authURL)

	_, ok := h.GetAuthorizationTransaction()
	assert.False(t, ok, "a refused flow must not leave a pending transaction")
}

func TestAuthorizationCodeExchange_PKCERequiresVerifier(t *testing.T) {
	challenge := GenerateCodeChallenge("verifier")
	query := url.Values{"code": {"CODE"}, "state": {"st"}}

	tests := []struct {
		name     string
		exchange func(t *testing.T, h *OAuthHandler) error
	}{
		{
			name: "ProcessAuthorizationResponse",
			exchange: func(t *testing.T, h *OAuthHandler) error {
				_, err := h.GetAuthorizationURL(t.Context(), "st", challenge)
				require.NoError(t, err)
				return h.ProcessAuthorizationResponse(t.Context(), "CODE", "st", "")
			},
		},
		{
			name: "ProcessAuthorizationCallback",
			exchange: func(t *testing.T, h *OAuthHandler) error {
				_, err := h.GetAuthorizationURL(t.Context(), "st", challenge)
				require.NoError(t, err)
				return h.ProcessAuthorizationCallback(t.Context(), query, "")
			},
		},
		{
			name: "state restored without a transaction",
			exchange: func(t *testing.T, h *OAuthHandler) error {
				h.SetExpectedState("st")
				return h.ProcessAuthorizationResponse(t.Context(), "CODE", "st", "")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, tokenRequests := newPKCETestHandler(t, true)
			require.ErrorIs(t, tt.exchange(t, h), ErrPKCERequired)
			assert.Zero(t, tokenRequests.Load(), "the code must not be sent without a verifier")
		})
	}
}

func TestAuthorizationFlow_PKCEDisabledAllowsEmptyValues(t *testing.T) {
	h, tokenRequests := newPKCETestHandler(t, false)

	authURL, err := h.GetAuthorizationURL(t.Context(), "st", "")
	require.NoError(t, err)
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	assert.False(t, parsed.Query().Has("code_challenge"))

	require.NoError(t, h.ProcessAuthorizationResponse(t.Context(), "CODE", "st", ""))
	assert.EqualValues(t, 1, tokenRequests.Load())
}

func TestAuthorizationFlow_PKCESendsChallengeAndVerifier(t *testing.T) {
	h, tokenRequests := newPKCETestHandler(t, true)

	verifier, err := GenerateCodeVerifier()
	require.NoError(t, err)
	authURL, err := h.GetAuthorizationURL(t.Context(), "st", GenerateCodeChallenge(verifier))
	require.NoError(t, err)
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	assert.Equal(t, GenerateCodeChallenge(verifier), parsed.Query().Get("code_challenge"))
	assert.Equal(t, "S256", parsed.Query().Get("code_challenge_method"))

	require.NoError(t, h.ProcessAuthorizationResponse(t.Context(), "CODE", "st", verifier))
	assert.EqualValues(t, 1, tokenRequests.Load())
}
