package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// A pre-existing session must be usable without Initialize, as it is with
// NewStreamableHttpClient.
func TestNewOAuthStreamableHttpClient_WithSession(t *testing.T) {
	ctx := t.Context()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "existing-session", r.Header.Get(transport.HeaderKeySessionID))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]any{"tools": []any{}},
		})
	}))
	defer server.Close()

	tokenStore := NewMemoryTokenStore()
	require.NoError(t, tokenStore.SaveToken(ctx, &Token{
		AccessToken: "test-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}))

	client, err := NewOAuthStreamableHttpClient(
		server.URL,
		OAuthConfig{ClientID: "test-client", TokenStore: tokenStore},
		transport.WithSession("existing-session"),
	)
	require.NoError(t, err)
	defer client.Close()
	require.NoError(t, client.Start(ctx))

	assert.True(t, client.IsInitialized())
	_, err = client.ListTools(ctx, mcp.ListToolsRequest{})
	require.NoError(t, err)
}
