package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func deleteHeaderServer(t *testing.T) (*httptest.Server, <-chan http.Header) {
	t.Helper()
	deletes := make(chan http.Header, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes <- r.Header.Clone()
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, deletes
}

func receiveDelete(t *testing.T, deletes <-chan http.Header) http.Header {
	t.Helper()
	select {
	case h := <-deletes:
		return h
	default:
		t.Fatal("Close did not send a DELETE")
		return nil
	}
}

func TestStreamableHTTP_CloseSendsCustomHeadersOnDelete(t *testing.T) {
	srv, deletes := deleteHeaderServer(t)

	trans, err := NewStreamableHTTP(srv.URL,
		WithSession("session-1"),
		WithHTTPHeaders(map[string]string{"Authorization": "Bearer static"}),
		WithHTTPHeaderFunc(func(context.Context) map[string]string {
			return map[string]string{"X-Tenant": "acme"}
		}),
	)
	require.NoError(t, err)
	trans.SetProtocolVersion(mcp.LATEST_LEGACY_PROTOCOL_VERSION)

	require.NoError(t, trans.Close())

	h := receiveDelete(t, deletes)
	assert.Equal(t, "session-1", h.Get(HeaderKeySessionID))
	assert.Equal(t, "Bearer static", h.Get("Authorization"))
	assert.Equal(t, "acme", h.Get("X-Tenant"))
}

func TestStreamableHTTP_CloseSendsOAuthHeaderOnDelete(t *testing.T) {
	srv, deletes := deleteHeaderServer(t)

	tokenStore := NewMemoryTokenStore()
	require.NoError(t, tokenStore.SaveToken(t.Context(), &Token{
		AccessToken: "oauth-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}))

	trans, err := NewStreamableHTTP(srv.URL,
		WithSession("session-1"),
		WithHTTPOAuth(OAuthConfig{
			ClientID:    "test-client",
			RedirectURI: "http://localhost:8085/callback",
			TokenStore:  tokenStore,
			PKCEEnabled: true,
		}),
	)
	require.NoError(t, err)
	trans.SetProtocolVersion(mcp.LATEST_LEGACY_PROTOCOL_VERSION)

	require.NoError(t, trans.Close())

	h := receiveDelete(t, deletes)
	assert.Equal(t, "Bearer oauth-token", h.Get("Authorization"))
}
