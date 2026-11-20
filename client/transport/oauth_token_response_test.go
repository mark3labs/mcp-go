package transport

import (
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Some authorization servers add a non-standard expires_at to the token
// response, as Unix seconds. RFC 6749 §5.1 defines only expires_in, so the
// handler must not fail the exchange over it (issue #1213).
const numericExpiresAtBody = `{"access_token":"new-access","token_type":"Bearer","expires_in":3600,"refresh_token":"next","expires_at":1794307530}`

func answer(body string) func(http.ResponseWriter, string) {
	return func(w http.ResponseWriter, _ string) { _, _ = io.WriteString(w, body) }
}

func TestOAuthHandler_RefreshAcceptsNumericExpiresAt(t *testing.T) {
	server := newRefreshServer(t, answer(numericExpiresAtBody))
	handler, store := newRefreshTestHandler(t, server)

	header, err := handler.GetAuthorizationHeader(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "Bearer new-access", header)

	saved, err := store.GetToken(t.Context())
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(time.Hour), saved.ExpiresAt, time.Minute,
		"expires_in wins over expires_at")
}

func TestOAuthHandler_CodeExchangeAcceptsNumericExpiresAt(t *testing.T) {
	server := newRefreshServer(t, answer(numericExpiresAtBody))
	store := NewMemoryTokenStore()
	handler := NewOAuthHandler(OAuthConfig{
		ClientID:              "client",
		RedirectURI:           "http://127.0.0.1:33418/callback",
		TokenStore:            store,
		AuthServerMetadataURL: server.URL + "/.well-known/oauth-authorization-server",
	})

	_, err := handler.GetAuthorizationURL(t.Context(), "st", "")
	require.NoError(t, err)
	require.NoError(t, handler.ProcessAuthorizationResponse(t.Context(), "CODE", "st", ""))

	saved, err := store.GetToken(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "new-access", saved.AccessToken)
	assert.WithinDuration(t, time.Now().Add(time.Hour), saved.ExpiresAt, time.Minute)
}

// Without expires_in, a readable expires_at is the only expiry the server
// gave; one that can't be read is ignored rather than failing the refresh.
func TestOAuthHandler_RefreshUsesExpiresAtWithoutExpiresIn(t *testing.T) {
	at := time.Unix(1794307530, 0)
	tests := []struct {
		name      string
		expiresAt string
		want      time.Time
	}{
		{"unix seconds", `1794307530`, at},
		{"RFC 3339 string", `"` + at.UTC().Format(time.RFC3339) + `"`, at},
		{"unreadable", `"soon"`, time.Time{}},
		{"null", `null`, time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newRefreshServer(t, answer(
				`{"access_token":"new-access","token_type":"Bearer","refresh_token":"next","expires_at":`+tt.expiresAt+`}`))
			handler, store := newRefreshTestHandler(t, server)

			_, err := handler.GetAuthorizationHeader(t.Context())
			require.NoError(t, err)

			saved, err := store.GetToken(t.Context())
			require.NoError(t, err)
			assert.True(t, tt.want.Equal(saved.ExpiresAt), "ExpiresAt = %v, want %v", saved.ExpiresAt, tt.want)
		})
	}
}
