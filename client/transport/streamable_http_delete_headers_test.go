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

func deleteTestOAuth(t *testing.T, withToken bool) StreamableHTTPCOption {
	t.Helper()
	tokenStore := NewMemoryTokenStore()
	if withToken {
		require.NoError(t, tokenStore.SaveToken(t.Context(), &Token{
			AccessToken: "oauth-token",
			TokenType:   "Bearer",
			ExpiresAt:   time.Now().Add(time.Hour),
		}))
	}
	return WithHTTPOAuth(OAuthConfig{
		ClientID:    "test-client",
		RedirectURI: "http://localhost:8085/callback",
		TokenStore:  tokenStore,
		PKCEEnabled: true,
	})
}

func TestStreamableHTTP_CloseSendsHeadersOnDelete(t *testing.T) {
	staticAuth := WithHTTPHeaders(map[string]string{"Authorization": "Bearer static"})
	tenantFunc := WithHTTPHeaderFunc(func(context.Context) map[string]string {
		return map[string]string{"X-Tenant": "acme"}
	})
	authFunc := WithHTTPHeaderFunc(func(context.Context) map[string]string {
		return map[string]string{"Authorization": "Bearer dynamic"}
	})

	tests := []struct {
		name       string
		opts       func(t *testing.T) []StreamableHTTPCOption
		wantAuth   string
		wantTenant string
	}{
		{
			name: "static headers and header func",
			opts: func(*testing.T) []StreamableHTTPCOption {
				return []StreamableHTTPCOption{staticAuth, tenantFunc}
			},
			wantAuth:   "Bearer static",
			wantTenant: "acme",
		},
		{
			name: "oauth token",
			opts: func(t *testing.T) []StreamableHTTPCOption {
				return []StreamableHTTPCOption{deleteTestOAuth(t, true)}
			},
			wantAuth: "Bearer oauth-token",
		},
		{
			name: "oauth token replaces static header",
			opts: func(t *testing.T) []StreamableHTTPCOption {
				return []StreamableHTTPCOption{staticAuth, deleteTestOAuth(t, true)}
			},
			wantAuth: "Bearer oauth-token",
		},
		{
			name: "header func replaces oauth token",
			opts: func(t *testing.T) []StreamableHTTPCOption {
				return []StreamableHTTPCOption{deleteTestOAuth(t, true), authFunc}
			},
			wantAuth: "Bearer dynamic",
		},
		{
			name: "oauth lookup fails and the delete is still sent",
			opts: func(t *testing.T) []StreamableHTTPCOption {
				return []StreamableHTTPCOption{deleteTestOAuth(t, false)}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, deletes := deleteHeaderServer(t)

			opts := append([]StreamableHTTPCOption{WithSession("session-1")}, tt.opts(t)...)
			trans, err := NewStreamableHTTP(srv.URL, opts...)
			require.NoError(t, err)
			trans.SetProtocolVersion(mcp.LATEST_LEGACY_PROTOCOL_VERSION)

			require.NoError(t, trans.Close())

			h := receiveDelete(t, deletes)
			assert.Equal(t, "session-1", h.Get(HeaderKeySessionID))
			assert.Equal(t, tt.wantAuth, h.Get("Authorization"))
			assert.Equal(t, tt.wantTenant, h.Get("X-Tenant"))
		})
	}
}
