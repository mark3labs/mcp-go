package transport

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// Regression test for https://github.com/mark3labs/mcp-go/issues/903
//
// When metadata discovery hits a non-2xx response, fetchMetadataFromURL returns
// (nil, nil), which previously left getServerMetadata returning (nil, nil) — no
// metadata and no error. Callers (RegisterClient, token/auth URL builders) then
// dereferenced the nil *AuthServerMetadata and panicked, crashing the process.
// getServerMetadata must return a non-nil error instead of (nil, nil).
func newOAuthHandlerWithUnavailableMetadata(t *testing.T) *OAuthHandler {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	return NewOAuthHandler(OAuthConfig{
		ClientID:              "test-client",
		RedirectURI:           "http://localhost/callback",
		Scopes:                []string{"mcp.read"},
		TokenStore:            NewMemoryTokenStore(),
		AuthServerMetadataURL: server.URL + "/.well-known/oauth-authorization-server",
	})
}

func TestOAuthHandler_GetServerMetadata_UnavailableReturnsError(t *testing.T) {
	handler := newOAuthHandlerWithUnavailableMetadata(t)

	metadata, err := handler.GetServerMetadata(t.Context())

	require.Error(t, err, "expected an error instead of (nil, nil) when discovery returns no metadata")
	require.Nil(t, metadata)
}

func TestOAuthHandler_RegisterClient_UnavailableMetadataDoesNotPanic(t *testing.T) {
	handler := newOAuthHandlerWithUnavailableMetadata(t)

	var err error
	require.NotPanics(t, func() {
		err = handler.RegisterClient(t.Context(), "test-client")
	})
	require.Error(t, err)
}
