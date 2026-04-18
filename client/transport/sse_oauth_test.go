package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSSE_WithOAuth(t *testing.T) {
	ctx := context.Background()
	// Track request count to simulate 401 on first request, then success
	requestCount := 0
	authHeaderReceived := ""
	sseEndpointSent := false

	// Create a test server that requires OAuth
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if this is an SSE connection request
		if r.Header.Get("Accept") == "text/event-stream" {
			// Capture the Authorization header
			authHeaderReceived = r.Header.Get("Authorization")

			// Check for Authorization header
			if requestCount == 0 {
				// First request - simulate 401 to test error handling
				requestCount++
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			// Create a valid endpoint URL
			endpointURL := "http://" + r.Host + "/endpoint"

			// Send the SSE endpoint event
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte("event: endpoint\ndata: " + endpointURL + "\n\n"))
			if err != nil {
				t.Errorf("Failed to write SSE endpoint event: %v", err)
			}
			sseEndpointSent = true
			return
		}

		// This is a regular HTTP request to the endpoint
		if r.URL.Path == "/endpoint" {
			// Capture the Authorization header
			authHeaderReceived = r.Header.Get("Authorization")

			// Verify the Authorization header
			if authHeaderReceived != "Bearer test-token" {
				t.Errorf("Expected Authorization header 'Bearer test-token', got '%s'", authHeaderReceived)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			// Return a successful response
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if err := json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  "success",
			}); err != nil {
				t.Errorf("Failed to encode JSON response: %v", err)
			}
		}
	}))
	defer server.Close()

	// Create a token store with a valid token
	tokenStore := NewMemoryTokenStore()
	validToken := &Token{
		AccessToken:  "test-token",
		TokenType:    "Bearer",
		RefreshToken: "refresh-token",
		ExpiresIn:    3600,
		ExpiresAt:    time.Now().Add(1 * time.Hour), // Valid for 1 hour
	}
	if err := tokenStore.SaveToken(ctx, validToken); err != nil {
		t.Fatalf("Failed to save token: %v", err)
	}

	// Create OAuth config
	oauthConfig := OAuthConfig{
		ClientID:    "test-client",
		RedirectURI: "http://localhost:8085/callback",
		Scopes:      []string{"mcp.read", "mcp.write"},
		TokenStore:  tokenStore,
		PKCEEnabled: true,
	}

	// Create SSE with OAuth
	transport, err := NewSSE(server.URL, WithOAuth(oauthConfig))
	if err != nil {
		t.Fatalf("Failed to create SSE: %v", err)
	}

	// Verify that OAuth is enabled
	if !transport.IsOAuthEnabled() {
		t.Errorf("Expected IsOAuthEnabled() to return true")
	}

	// Verify the OAuth handler is set
	if transport.GetOAuthHandler() == nil {
		t.Errorf("Expected GetOAuthHandler() to return a handler")
	}

	// First start attempt should fail with OAuthAuthorizationRequiredError
	// Use a context with a short timeout to avoid hanging
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = transport.Start(ctx)

	// Verify the error is an OAuthAuthorizationRequiredError
	if err == nil {
		t.Fatalf("Expected error on first start attempt, got nil")
	}

	var oauthErr *OAuthAuthorizationRequiredError
	if !errors.As(err, &oauthErr) {
		t.Fatalf("Expected OAuthAuthorizationRequiredError, got %T: %v", err, err)
	}

	// Verify the error has the handler
	if oauthErr.Handler == nil {
		t.Errorf("Expected OAuthAuthorizationRequiredError to have a handler")
	}

	// Verify the server received the first request
	if requestCount != 1 {
		t.Errorf("Expected server to receive 1 request, got %d", requestCount)
	}

	// Second start attempt should succeed
	// Use a context with a short timeout to avoid hanging
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	err = transport.Start(ctx2)
	if err != nil {
		t.Fatalf("Failed to start SSE: %v", err)
	}

	// Verify the SSE endpoint was sent
	if !sseEndpointSent {
		t.Errorf("Expected SSE endpoint to be sent")
	}

	// Skip the actual request/response test since it's difficult to mock properly in this context
	// The important part is that we've verified the OAuth functionality works during connection
	// and that the endpoint is properly received

	// For a real test, we would need to mock the SSE message handling more thoroughly
	// which is beyond the scope of this test

	// Verify the server received the Authorization header during the SSE connection
	if authHeaderReceived != "Bearer test-token" {
		t.Errorf("Expected server to receive Authorization header 'Bearer test-token', got '%s'", authHeaderReceived)
	}

	// Clean up
	transport.Close()
}

func TestSSE_WithOAuth_Unauthorized(t *testing.T) {
	// Create a test server that requires OAuth
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always return unauthorized
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	// Create an empty token store
	tokenStore := NewMemoryTokenStore()

	// Create OAuth config
	oauthConfig := OAuthConfig{
		ClientID:    "test-client",
		RedirectURI: "http://localhost:8085/callback",
		Scopes:      []string{"mcp.read", "mcp.write"},
		TokenStore:  tokenStore,
		PKCEEnabled: true,
	}

	// Create SSE with OAuth
	transport, err := NewSSE(server.URL, WithOAuth(oauthConfig))
	if err != nil {
		t.Fatalf("Failed to create SSE: %v", err)
	}

	// Start should fail with OAuthAuthorizationRequiredError
	// Use a context with a short timeout to avoid hanging
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = transport.Start(ctx)

	// Verify the error is an OAuthAuthorizationRequiredError
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	var oauthErr *OAuthAuthorizationRequiredError
	if !errors.As(err, &oauthErr) {
		t.Fatalf("Expected OAuthAuthorizationRequiredError, got %T: %v", err, err)
	}

	// Verify the error has the handler
	if oauthErr.Handler == nil {
		t.Errorf("Expected OAuthAuthorizationRequiredError to have a handler")
	}
}

func TestSSE_IsOAuthEnabled(t *testing.T) {
	// Create SSE without OAuth
	transport1, err := NewSSE("http://example.com")
	if err != nil {
		t.Fatalf("Failed to create SSE: %v", err)
	}

	// Verify OAuth is not enabled
	if transport1.IsOAuthEnabled() {
		t.Errorf("Expected IsOAuthEnabled() to return false")
	}

	// Create SSE with OAuth
	transport2, err := NewSSE("http://example.com", WithOAuth(OAuthConfig{
		ClientID: "test-client",
	}))
	if err != nil {
		t.Fatalf("Failed to create SSE: %v", err)
	}

	// Verify OAuth is enabled
	if !transport2.IsOAuthEnabled() {
		t.Errorf("Expected IsOAuthEnabled() to return true")
	}
}

func TestSSE_WithOAuth_PreservesPathInBaseURL(t *testing.T) {
	transport, err := NewSSE("https://example.com/googledrive?foo=bar#frag", WithOAuth(OAuthConfig{
		ClientID: "test-client",
	}))
	if err != nil {
		t.Fatalf("Failed to create SSE: %v", err)
	}

	if transport.GetOAuthHandler() == nil {
		t.Fatalf("Expected GetOAuthHandler() to return a handler")
	}

	if transport.GetOAuthHandler().baseURL != "https://example.com/googledrive" {
		t.Errorf("Expected OAuth base URL to preserve path, got %q", transport.GetOAuthHandler().baseURL)
	}

	if transport.baseURL.String() != "https://example.com/googledrive?foo=bar#frag" {
		t.Errorf("Expected transport base URL to retain query and fragment, got %q", transport.baseURL.String())
	}
}

// TestSSE_WithOAuth_ExtractsPRMFromWWWAuthenticate verifies that a 401
// response carrying a WWW-Authenticate header with a resource_metadata
// parameter (RFC 9728 §5.1) stores the advertised URL on the OAuth handler
// so subsequent metadata discovery can use it.
func TestSSE_WithOAuth_ExtractsPRMFromWWWAuthenticate(t *testing.T) {
	const advertisedPRM = "https://example.com/tenant-a/.well-known/oauth-protected-resource"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="mcp", error="invalid_token", resource_metadata="`+advertisedPRM+`"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	// Pre-populate a token so the transport reaches the server and the 401
	// WWW-Authenticate path runs — with no token the transport short-circuits
	// with OAuthAuthorizationRequiredError before any HTTP request is sent.
	tokenStore := NewMemoryTokenStore()
	_ = tokenStore.SaveToken(context.Background(), &Token{
		AccessToken: "rejected-by-server",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	})

	transport, err := NewSSE(server.URL, WithOAuth(OAuthConfig{
		ClientID:    "test-client",
		RedirectURI: "http://localhost/callback",
		TokenStore:  tokenStore,
	}))
	if err != nil {
		t.Fatalf("Failed to create SSE: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = transport.Start(ctx)

	var oauthErr *OAuthAuthorizationRequiredError
	if !errors.As(err, &oauthErr) {
		t.Fatalf("Expected OAuthAuthorizationRequiredError, got %T: %v", err, err)
	}
	if got := transport.GetOAuthHandler().ProtectedResourceMetadataURL(); got != advertisedPRM {
		t.Errorf("Expected PRM URL %q, got %q", advertisedPRM, got)
	}
}

// TestSSE_WithOAuth_NoWWWAuthenticate_LeavesPRMUnset verifies that a 401
// without a WWW-Authenticate header preserves the pre-RFC-9728 behaviour:
// no PRM URL is captured and discovery falls back to origin-based paths.
func TestSSE_WithOAuth_NoWWWAuthenticate_LeavesPRMUnset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	tokenStore := NewMemoryTokenStore()
	_ = tokenStore.SaveToken(context.Background(), &Token{
		AccessToken: "rejected-by-server",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	})

	transport, err := NewSSE(server.URL, WithOAuth(OAuthConfig{
		ClientID:    "test-client",
		RedirectURI: "http://localhost/callback",
		TokenStore:  tokenStore,
	}))
	if err != nil {
		t.Fatalf("Failed to create SSE: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = transport.Start(ctx)

	if got := transport.GetOAuthHandler().ProtectedResourceMetadataURL(); got != "" {
		t.Errorf("Expected PRM URL to remain empty, got %q", got)
	}
}
