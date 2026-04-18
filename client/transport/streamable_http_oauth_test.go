package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestStreamableHTTP_WithOAuth(t *testing.T) {
	ctx := context.Background()
	// Track request count to simulate 401 on first request, then success
	var requestCount atomic.Int32
	authHeaderReceived := ""

	// Create a test server that requires OAuth
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the Authorization header
		authHeaderReceived = r.Header.Get("Authorization")

		// Check for Authorization header
		if requestCount.Load() == 0 {
			// First request - simulate 401 to test error handling
			requestCount.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Subsequent requests - verify the Authorization header
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

	// Create StreamableHTTP with OAuth
	transport, err := NewStreamableHTTP(server.URL, WithHTTPOAuth(oauthConfig))
	if err != nil {
		t.Fatalf("Failed to create StreamableHTTP: %v", err)
	}

	// Verify that OAuth is enabled
	if !transport.IsOAuthEnabled() {
		t.Errorf("Expected IsOAuthEnabled() to return true")
	}

	// Verify the OAuth handler is set
	if transport.GetOAuthHandler() == nil {
		t.Errorf("Expected GetOAuthHandler() to return a handler")
	}

	// First request should fail with OAuthAuthorizationRequiredError
	_, err = transport.SendRequest(context.Background(), JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(1),
		Method:  "test",
	})

	// Verify the error is an OAuthAuthorizationRequiredError
	if err == nil {
		t.Fatalf("Expected error on first request, got nil")
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
	if got := requestCount.Load(); got != 1 {
		t.Errorf("Expected server to receive 1 request, got %d", got)
	}

	// Second request should succeed
	response, err := transport.SendRequest(context.Background(), JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(2),
		Method:  "test",
	})

	if err != nil {
		t.Fatalf("Failed to send second request: %v", err)
	}

	// Verify the response
	var resultStr string
	if err := json.Unmarshal(response.Result, &resultStr); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if resultStr != "success" {
		t.Errorf("Expected result to be 'success', got %v", resultStr)
	}

	// Verify the server received the Authorization header
	if authHeaderReceived != "Bearer test-token" {
		t.Errorf("Expected server to receive Authorization header 'Bearer test-token', got '%s'", authHeaderReceived)
	}
}

func TestStreamableHTTP_WithOAuth_Unauthorized(t *testing.T) {
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

	// Create StreamableHTTP with OAuth
	transport, err := NewStreamableHTTP(server.URL, WithHTTPOAuth(oauthConfig))
	if err != nil {
		t.Fatalf("Failed to create StreamableHTTP: %v", err)
	}

	// Send a request
	_, err = transport.SendRequest(context.Background(), JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(1),
		Method:  "test",
	})

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

func TestStreamableHTTP_IsOAuthEnabled(t *testing.T) {
	// Create StreamableHTTP without OAuth
	transport1, err := NewStreamableHTTP("http://example.com")
	if err != nil {
		t.Fatalf("Failed to create StreamableHTTP: %v", err)
	}

	// Verify OAuth is not enabled
	if transport1.IsOAuthEnabled() {
		t.Errorf("Expected IsOAuthEnabled() to return false")
	}

	// Create StreamableHTTP with OAuth
	transport2, err := NewStreamableHTTP("http://example.com", WithHTTPOAuth(OAuthConfig{
		ClientID: "test-client",
	}))
	if err != nil {
		t.Fatalf("Failed to create StreamableHTTP: %v", err)
	}

	// Verify OAuth is enabled
	if !transport2.IsOAuthEnabled() {
		t.Errorf("Expected IsOAuthEnabled() to return true")
	}
}

func TestStreamableHTTP_WithOAuth_PreservesPathInBaseURL(t *testing.T) {
	transport, err := NewStreamableHTTP("https://example.com/googledrive?foo=bar#frag", WithHTTPOAuth(OAuthConfig{
		ClientID: "test-client",
	}))
	if err != nil {
		t.Fatalf("Failed to create StreamableHTTP: %v", err)
	}

	if transport.GetOAuthHandler() == nil {
		t.Fatalf("Expected GetOAuthHandler() to return a handler")
	}

	if transport.GetOAuthHandler().baseURL != "https://example.com/googledrive" {
		t.Errorf("Expected OAuth base URL to preserve path, got %q", transport.GetOAuthHandler().baseURL)
	}

	if transport.serverURL.String() != "https://example.com/googledrive?foo=bar#frag" {
		t.Errorf("Expected transport server URL to retain query and fragment, got %q", transport.serverURL.String())
	}
}

// TestStreamableHTTP_WithOAuth_ExtractsPRMFromWWWAuthenticate verifies that a
// 401 response carrying a WWW-Authenticate header with a resource_metadata
// parameter (RFC 9728 §5.1) stores the advertised URL on the OAuth handler
// so subsequent metadata discovery can use it.
func TestStreamableHTTP_WithOAuth_ExtractsPRMFromWWWAuthenticate(t *testing.T) {
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

	transport, err := NewStreamableHTTP(server.URL, WithHTTPOAuth(OAuthConfig{
		ClientID:    "test-client",
		RedirectURI: "http://localhost/callback",
		TokenStore:  tokenStore,
	}))
	if err != nil {
		t.Fatalf("Failed to create StreamableHTTP: %v", err)
	}

	_, err = transport.SendRequest(context.Background(), JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(1),
		Method:  "test",
	})

	var oauthErr *OAuthAuthorizationRequiredError
	if !errors.As(err, &oauthErr) {
		t.Fatalf("Expected OAuthAuthorizationRequiredError, got %T: %v", err, err)
	}
	if got := transport.GetOAuthHandler().ProtectedResourceMetadataURL(); got != advertisedPRM {
		t.Errorf("Expected PRM URL %q, got %q", advertisedPRM, got)
	}
}

// TestStreamableHTTP_WithOAuth_NoWWWAuthenticate_LeavesPRMUnset verifies that
// a 401 without a WWW-Authenticate header preserves pre-RFC-9728 behaviour:
// no PRM URL is captured and discovery falls back to origin-based paths.
func TestStreamableHTTP_WithOAuth_NoWWWAuthenticate_LeavesPRMUnset(t *testing.T) {
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

	transport, err := NewStreamableHTTP(server.URL, WithHTTPOAuth(OAuthConfig{
		ClientID:    "test-client",
		RedirectURI: "http://localhost/callback",
		TokenStore:  tokenStore,
	}))
	if err != nil {
		t.Fatalf("Failed to create StreamableHTTP: %v", err)
	}

	_, _ = transport.SendRequest(context.Background(), JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(1),
		Method:  "test",
	})

	if got := transport.GetOAuthHandler().ProtectedResourceMetadataURL(); got != "" {
		t.Errorf("Expected PRM URL to remain empty, got %q", got)
	}
}
