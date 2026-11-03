package transport

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func refreshFailsWith(status int, body string) func(http.ResponseWriter, string) {
	return func(w http.ResponseWriter, _ string) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func TestOAuthHandler_RefreshFailureKeepsCause(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		check  func(t *testing.T, err error)
	}{
		{
			name:   "grant rejected",
			status: http.StatusBadRequest,
			body:   `{"error":"invalid_grant"}`,
			check: func(t *testing.T, err error) {
				var oauthErr OAuthError
				require.ErrorAs(t, err, &oauthErr)
				assert.Equal(t, "invalid_grant", oauthErr.ErrorCode)
			},
		},
		{
			name:   "token endpoint unavailable",
			status: http.StatusServiceUnavailable,
			body:   "upstream down",
			check: func(t *testing.T, err error) {
				assert.Contains(t, err.Error(), "status 503")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newRefreshServer(t, refreshFailsWith(tt.status, tt.body))
			handler, _ := newRefreshTestHandler(t, server)

			_, err := handler.GetAuthorizationHeader(t.Context())

			require.ErrorIs(t, err, ErrOAuthAuthorizationRequired)
			tt.check(t, err)
		})
	}
}

// oauthCauseSetup returns an OAuth config whose stored token can't be used or
// refreshed, the URL of the MCP server, and the error that says why.
type oauthCauseSetup func(t *testing.T) (OAuthConfig, string, error)

func issuerMigrated(t *testing.T) (OAuthConfig, string, error) {
	a, b := newBindingServer(t, "a", "secret"), newBindingServer(t, "b", "")
	prm, migrate := bindingResource(t, a, b)
	store := NewMemoryTokenStore()
	require.NoError(t, store.SaveToken(t.Context(), &Token{
		Issuer:       a.URL,
		AccessToken:  "expired",
		RefreshToken: "refresh",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}))
	migrate()
	return OAuthConfig{
		ClientID:                     "configured",
		ClientSecret:                 "secret",
		CredentialsIssuer:            a.URL,
		RedirectURI:                  prm + "/callback",
		ProtectedResourceMetadataURL: prm,
		TokenStore:                   store,
	}, strings.TrimSuffix(prm, "/.well-known/oauth-protected-resource"), ErrAuthorizationServerChanged
}

func refreshUnavailable(t *testing.T) (OAuthConfig, string, error) {
	server := newRefreshServer(t, refreshFailsWith(http.StatusServiceUnavailable, "upstream down"))
	handler, _ := newRefreshTestHandler(t, server)
	return handler.config, server.URL, nil
}

func TestOAuthAuthorizationRequiredError_KeepsCause(t *testing.T) {
	transports := []struct {
		name string
		call func(ctx context.Context, url string, cfg OAuthConfig) error
	}{
		{
			name: "streamable http",
			call: func(ctx context.Context, url string, cfg OAuthConfig) error {
				c, err := NewStreamableHTTP(url, WithHTTPOAuth(cfg))
				if err != nil {
					return err
				}
				_, err = c.SendRequest(ctx, JSONRPCRequest{JSONRPC: mcp.JSONRPC_VERSION, Method: "ping"})
				return err
			},
		},
		{
			name: "sse",
			call: func(ctx context.Context, url string, cfg OAuthConfig) error {
				c, err := NewSSE(url, WithOAuth(cfg))
				if err != nil {
					return err
				}
				return c.Start(ctx)
			},
		},
	}
	causes := []struct {
		name  string
		setup oauthCauseSetup
		check func(t *testing.T, err error)
	}{
		{
			name:  "authorization server changed",
			setup: issuerMigrated,
		},
		{
			name:  "refresh failed",
			setup: refreshUnavailable,
			check: func(t *testing.T, err error) {
				assert.Contains(t, errors.Unwrap(err).Error(), "status 503")
			},
		},
	}

	for _, tr := range transports {
		for _, cause := range causes {
			t.Run(tr.name+"/"+cause.name, func(t *testing.T) {
				cfg, url, want := cause.setup(t)

				err := tr.call(t.Context(), url, cfg)

				var authErr *OAuthAuthorizationRequiredError
				require.ErrorAs(t, err, &authErr)
				assert.NotNil(t, authErr.Handler)
				assert.ErrorIs(t, err, ErrOAuthAuthorizationRequired)
				assert.Equal(t, ErrOAuthAuthorizationRequired.Error(), authErr.Error())
				if want != nil {
					assert.ErrorIs(t, err, want)
				}
				if cause.check != nil {
					cause.check(t, authErr)
				}
			})
		}
	}
}
