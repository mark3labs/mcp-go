package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flakyMetadataServer serves authorization server metadata, failing the first
// failures requests with 503, and counts the requests it gets.
func flakyMetadataServer(t *testing.T, failures int32) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) <= failures {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 server.URL,
			"authorization_endpoint": server.URL + "/authorize",
			"token_endpoint":         server.URL + "/token",
		})
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func newDiscoveryTestHandler(server *httptest.Server) *OAuthHandler {
	return NewOAuthHandler(OAuthConfig{
		ClientID:              "client",
		RedirectURI:           "http://localhost/callback",
		AuthServerMetadataURL: server.URL + "/.well-known/oauth-authorization-server",
	})
}

// Metadata discovery that failed is tried again by the next call, rather
// than the failure being returned for the rest of the handler's life.
func TestOAuthHandler_DiscoveryRetriesAfterFailure(t *testing.T) {
	t.Run("cancelled context", func(t *testing.T) {
		server, _ := flakyMetadataServer(t, 0)
		handler := newDiscoveryTestHandler(server)

		cancelled, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := handler.GetServerMetadata(cancelled)
		require.ErrorIs(t, err, context.Canceled)

		metadata, err := handler.GetServerMetadata(t.Context())
		require.NoError(t, err)
		assert.Equal(t, server.URL+"/token", metadata.TokenEndpoint)
	})
	t.Run("cancelled context, then no metadata", func(t *testing.T) {
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(server.Close)
		handler := newDiscoveryTestHandler(server)

		cancelled, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := handler.GetServerMetadata(cancelled)
		require.ErrorIs(t, err, context.Canceled)

		// The next discovery fails for a reason of its own.
		_, err = handler.GetServerMetadata(t.Context())
		require.Error(t, err)
		assert.NotErrorIs(t, err, context.Canceled)
		assert.Contains(t, err.Error(), "returned no metadata")
		assert.Equal(t, int32(1), requests.Load())
	})
	t.Run("server error", func(t *testing.T) {
		server, requests := flakyMetadataServer(t, 1)
		handler := newDiscoveryTestHandler(server)

		_, err := handler.GetServerMetadata(t.Context())
		require.Error(t, err)

		metadata, err := handler.GetServerMetadata(t.Context())
		require.NoError(t, err)
		assert.Equal(t, server.URL+"/token", metadata.TokenEndpoint)
		assert.Equal(t, int32(2), requests.Load())
	})
}

// Metadata that was discovered is kept.
func TestOAuthHandler_DiscoveryKeepsMetadata(t *testing.T) {
	server, requests := flakyMetadataServer(t, 0)
	handler := newDiscoveryTestHandler(server)

	for range 3 {
		_, err := handler.GetServerMetadata(t.Context())
		require.NoError(t, err)
	}
	assert.Equal(t, int32(1), requests.Load())
}
