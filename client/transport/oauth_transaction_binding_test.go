package transport

import (
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOAuthTransactionBindingRejectsStaleCallbackToken(t *testing.T) {
	a, b := newBindingServer(t, "a", "secret"), newBindingServer(t, "b", "")
	prm, migrate := bindingResource(t, a, b)
	h := NewOAuthHandler(OAuthConfig{RedirectURI: prm + "/callback", ProtectedResourceMetadataURL: prm})
	require.NoError(t, h.RegisterClient(t.Context(), "name"))
	_, err := h.GetAuthorizationURL(t.Context(), "state", "")
	require.NoError(t, err)
	gate := make(chan struct{})
	defer close(gate)
	a.tokenGate = gate
	a.entered = make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- h.ProcessAuthorizationCallback(t.Context(), url.Values{
			"code": {"old-code"}, "state": {"state"}, "iss": {a.URL},
		}, "")
	}()
	select {
	case <-a.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("callback code exchange was not sent")
	}
	migrate()
	h.SetProtectedResourceMetadataURL(prm)
	_, err = h.GetServerMetadata(t.Context())
	require.NoError(t, err)
	gate <- struct{}{}
	require.ErrorIs(t, <-done, ErrAuthorizationServerChanged)
	_, err = h.config.TokenStore.GetToken(t.Context())
	require.ErrorIs(t, err, ErrNoToken)
	assert.Zero(t, b.tokens.Load())
}

func TestOAuthTransactionBindingRejectsRestoredCredentialMismatch(t *testing.T) {
	for _, field := range []string{"client ID", "client secret", "issuer"} {
		t.Run(field, func(t *testing.T) {
			a := newBindingServer(t, "a", "secret")
			config := OAuthConfig{
				ClientID: "a", ClientSecret: "secret", CredentialsIssuer: a.URL,
				AuthServerMetadataURL: a.URL + "/.well-known/oauth-authorization-server",
			}
			h := NewOAuthHandler(config)
			_, err := h.GetAuthorizationURL(t.Context(), "state", "")
			require.NoError(t, err)
			transaction, ok := h.GetAuthorizationTransaction()
			require.True(t, ok)
			switch field {
			case "client ID":
				config.ClientID = "replacement"
			case "client secret":
				config.ClientSecret = "replacement"
			case "issuer":
				config.CredentialsIssuer = a.URL + "/different"
			}
			callback := NewOAuthHandler(config)
			require.NoError(t, callback.SetAuthorizationTransaction(transaction))
			err = callback.ProcessAuthorizationCallback(t.Context(), url.Values{
				"code": {"code"}, "state": {"state"}, "iss": {a.URL},
			}, "")
			require.ErrorIs(t, err, ErrAuthorizationServerChanged)
			assert.Zero(t, a.tokens.Load())
		})
	}
}
