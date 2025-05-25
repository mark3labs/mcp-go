package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestStreamableHTTP_WithOAuth(t *testing.T) {
	// Track request count to simulate 401 on first request, then success
	requestCount := 0
	authHeaderReceived := ""

	// Create a test server that requires OAuth
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the Authorization header
		authHeaderReceived = r.Header.Get("Authorization")

		// Check for Authorization header
		if requestCount == 0 {
			// First request - simulate 401 to test error handling
			requestCount++
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
	if err := tokenStore.SaveToken(validToken); err != nil {
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
	transport, err := NewStreamableHTTP(server.URL, WithOAuth(oauthConfig))
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
	if requestCount != 1 {
		t.Errorf("Expected server to receive 1 request, got %d", requestCount)
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
	transport, err := NewStreamableHTTP(server.URL, WithOAuth(oauthConfig))
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
	transport2, err := NewStreamableHTTP("http://example.com", WithOAuth(OAuthConfig{
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
