package transport

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// issHandler returns an OAuth handler for an authorization server that
// advertises the RFC 9207 iss parameter when issSupported is set, with its
// flow started under the state "state", along with the server's issuer and a
// counter of token requests.
func issHandler(t *testing.T, issSupported bool) (*OAuthHandler, string, *atomic.Int32) {
	t.Helper()
	var tokenRequests atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(AuthServerMetadata{
				Issuer:                server.URL,
				AuthorizationEndpoint: server.URL + "/authorize",
				TokenEndpoint:         server.URL + "/token",
				AuthorizationResponseIssParameterSupported: issSupported,
			})
		case "/token":
			tokenRequests.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "token", "token_type": "Bearer"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	handler := NewOAuthHandler(OAuthConfig{
		ClientID:              "client",
		RedirectURI:           "http://localhost:8085/callback",
		TokenStore:            NewMemoryTokenStore(),
		AuthServerMetadataURL: server.URL + "/.well-known/oauth-authorization-server",
		PKCEEnabled:           true,
	})
	handler.SetExpectedState("state")
	return handler, server.URL, &tokenRequests
}

// The iss parameter of an authorization response must identify the
// authorization server the client is using (RFC 9207 §2.4). Otherwise the
// response may come from another server in a mix-up attack, and the code
// must not be exchanged.
func TestOAuthHandler_ProcessAuthorizationCallbackChecksIssuer(t *testing.T) {
	same := func(issuer string) string { return issuer }
	tests := []struct {
		name         string
		issSupported bool
		iss          func(issuer string) string // nil: no iss parameter
		wantErr      bool
	}{
		{name: "advertised and matching", issSupported: true, iss: same},
		{name: "advertised and missing", issSupported: true, wantErr: true},
		{name: "advertised and different", issSupported: true, iss: func(string) string { return "https://attacker.example.com" }, wantErr: true},
		{name: "advertised with a trailing slash added", issSupported: true, iss: func(issuer string) string { return issuer + "/" }, wantErr: true},
		{name: "advertised and empty", issSupported: true, iss: func(string) string { return "" }, wantErr: true},
		{name: "not advertised and missing"},
		{name: "not advertised and empty", iss: func(string) string { return "" }},
		{name: "not advertised and matching", iss: same},
		{name: "not advertised and different", iss: func(string) string { return "https://attacker.example.com" }, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, issuer, tokenRequests := issHandler(t, tt.issSupported)
			query := url.Values{"code": {"code"}, "state": {"state"}}
			if tt.iss != nil {
				query.Set("iss", tt.iss(issuer))
			}

			err := handler.ProcessAuthorizationCallback(t.Context(), query, "verifier")
			if !tt.wantErr {
				require.NoError(t, err)
				assert.Equal(t, int32(1), tokenRequests.Load())
				return
			}
			require.ErrorIs(t, err, ErrIssuerMismatch)
			assert.Zero(t, tokenRequests.Load(), "the code must not be exchanged")

			// The state is spent, so the same response can't be replayed.
			err = handler.ProcessAuthorizationCallback(t.Context(), query, "verifier")
			require.Error(t, err)
			assert.NotErrorIs(t, err, ErrIssuerMismatch)
		})
	}
}

// An error in place of a code is reported, but only once the response has
// passed the state and iss checks: the spec forbids acting on an error from a
// response whose iss doesn't match.
func TestOAuthHandler_ProcessAuthorizationCallbackReportsErrors(t *testing.T) {
	t.Run("matching iss", func(t *testing.T) {
		handler, issuer, tokenRequests := issHandler(t, true)
		err := handler.ProcessAuthorizationCallback(t.Context(), url.Values{
			"error":             {"access_denied"},
			"error_description": {"The user denied the request"},
			"state":             {"state"},
			"iss":               {issuer},
		}, "verifier")

		var oauthErr OAuthError
		require.True(t, errors.As(err, &oauthErr), "got %v", err)
		assert.Equal(t, "access_denied", oauthErr.ErrorCode)
		assert.Equal(t, "The user denied the request", oauthErr.ErrorDescription)
		assert.Zero(t, tokenRequests.Load())
	})

	t.Run("different iss", func(t *testing.T) {
		handler, _, _ := issHandler(t, true)
		err := handler.ProcessAuthorizationCallback(t.Context(), url.Values{
			"error": {"access_denied"},
			"state": {"state"},
			"iss":   {"https://attacker.example.com"},
		}, "verifier")

		require.ErrorIs(t, err, ErrIssuerMismatch)
		var oauthErr OAuthError
		assert.False(t, errors.As(err, &oauthErr), "the error from the wrong issuer must not be reported")
	})

	t.Run("neither code nor error", func(t *testing.T) {
		handler, issuer, tokenRequests := issHandler(t, true)
		err := handler.ProcessAuthorizationCallback(t.Context(), url.Values{
			"state": {"state"},
			"iss":   {issuer},
		}, "verifier")

		require.ErrorContains(t, err, "no code")
		assert.Zero(t, tokenRequests.Load())
	})

	t.Run("wrong state", func(t *testing.T) {
		handler, issuer, tokenRequests := issHandler(t, true)
		err := handler.ProcessAuthorizationCallback(t.Context(), url.Values{
			"code":  {"code"},
			"state": {"forged"},
			"iss":   {issuer},
		}, "verifier")

		require.ErrorIs(t, err, ErrInvalidState)
		assert.Zero(t, tokenRequests.Load())
	})
}

// RFC 6749 §3.1 forbids repeating a response parameter. A second iss or code
// could otherwise pass the checks while another one is used.
func TestOAuthHandler_ProcessAuthorizationCallbackRejectsRepeatedParameters(t *testing.T) {
	for _, name := range []string{"iss", "code", "state", "error"} {
		t.Run(name, func(t *testing.T) {
			handler, issuer, tokenRequests := issHandler(t, true)
			query := url.Values{"code": {"code"}, "state": {"state"}, "iss": {issuer}}
			if !query.Has(name) {
				query.Add(name, "access_denied")
			}
			query.Add(name, "https://attacker.example.com")

			err := handler.ProcessAuthorizationCallback(t.Context(), query, "verifier")
			require.ErrorContains(t, err, "repeats the "+name+" parameter")
			assert.Zero(t, tokenRequests.Load())
		})
	}
}

// Without metadata from the authorization server, the client only guesses
// its endpoints and has no confirmed issuer to compare iss with, so the
// response goes through as before.
func TestOAuthHandler_ProcessAuthorizationCallbackWithoutMetadata(t *testing.T) {
	var tokenRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		tokenRequests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "token", "token_type": "Bearer"})
	}))
	t.Cleanup(server.Close)
	handler := NewOAuthHandler(OAuthConfig{
		ClientID:    "client",
		RedirectURI: "http://localhost:8085/callback",
		TokenStore:  NewMemoryTokenStore(),
		PKCEEnabled: true,
	})
	handler.SetBaseURL(server.URL)
	handler.SetExpectedState("state")

	err := handler.ProcessAuthorizationCallback(t.Context(), url.Values{
		"code":  {"code"},
		"state": {"state"},
		"iss":   {server.URL + "/tenant"},
	}, "verifier")
	require.NoError(t, err)
	assert.Equal(t, int32(1), tokenRequests.Load())
}
