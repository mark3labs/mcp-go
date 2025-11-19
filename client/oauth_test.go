package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
)

func TestNewOAuthStreamableHttpClient(t *testing.T) {
	ctx := context.Background()
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Return a successful response
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]any{
					"name":    "test-server",
					"version": "1.0.0",
				},
				"capabilities": map[string]any{},
			},
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

	// Create client with OAuth
	client, err := NewOAuthStreamableHttpClient(server.URL, oauthConfig)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Start the client
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}
	defer client.Close()

	// Verify that the client was created successfully
	trans := client.GetTransport()
	streamableHTTP, ok := trans.(*transport.StreamableHTTP)
	if !ok {
		t.Fatalf("Expected transport to be *transport.StreamableHTTP, got %T", trans)
	}

	// Verify OAuth is enabled
	if !streamableHTTP.IsOAuthEnabled() {
		t.Errorf("Expected IsOAuthEnabled() to return true")
	}

	// Verify the OAuth handler is set
	if streamableHTTP.GetOAuthHandler() == nil {
		t.Errorf("Expected GetOAuthHandler() to return a handler")
	}
}

func TestIsOAuthAuthorizationRequiredError(t *testing.T) {
	// Create a test error
	err := &transport.OAuthAuthorizationRequiredError{
		Handler: transport.NewOAuthHandler(transport.OAuthConfig{}),
	}

	// Verify IsOAuthAuthorizationRequiredError returns true
	if !IsOAuthAuthorizationRequiredError(err) {
		t.Errorf("Expected IsOAuthAuthorizationRequiredError to return true")
	}

	// Verify GetOAuthHandler returns the handler
	handler := GetOAuthHandler(err)
	if handler == nil {
		t.Errorf("Expected GetOAuthHandler to return a handler")
	}

	// Test with a different error
	err2 := fmt.Errorf("some other error")

	// Verify IsOAuthAuthorizationRequiredError returns false
	if IsOAuthAuthorizationRequiredError(err2) {
		t.Errorf("Expected IsOAuthAuthorizationRequiredError to return false")
	}
	// Verify GetOAuthHandler returns nil
	handler = GetOAuthHandler(err2)
	if handler != nil {
		t.Errorf("Expected GetOAuthHandler to return nil")
	}
}

func TestGetResourceMetadataURL(t *testing.T) {
	// Test with error containing metadata URL
	metadataURL := "https://auth.example.com/.well-known/oauth-protected-resource"
	err := &transport.OAuthAuthorizationRequiredError{
		Handler: transport.NewOAuthHandler(transport.OAuthConfig{}),
		AuthorizationRequiredError: transport.AuthorizationRequiredError{
			ResourceMetadataURL: metadataURL,
		},
	}

	// Verify GetResourceMetadataURL returns the correct URL
	result := GetResourceMetadataURL(err)
	if result != metadataURL {
		t.Errorf("Expected GetResourceMetadataURL to return %q, got %q", metadataURL, result)
	}

	// Test with error containing no metadata URL
	err2 := &transport.OAuthAuthorizationRequiredError{
		Handler: transport.NewOAuthHandler(transport.OAuthConfig{}),
		AuthorizationRequiredError: transport.AuthorizationRequiredError{
			ResourceMetadataURL: "",
		},
	}

	result2 := GetResourceMetadataURL(err2)
	if result2 != "" {
		t.Errorf("Expected GetResourceMetadataURL to return empty string, got %q", result2)
	}

	// Test with non-OAuth error
	err3 := fmt.Errorf("some other error")

	result3 := GetResourceMetadataURL(err3)
	if result3 != "" {
		t.Errorf("Expected GetResourceMetadataURL to return empty string for non-OAuth error, got %q", result3)
	}
}

func TestIsAuthorizationRequiredError(t *testing.T) {
	// Test with base AuthorizationRequiredError (401 without OAuth handler)
	metadataURL := "https://auth.example.com/.well-known/oauth-protected-resource"
	err := &transport.AuthorizationRequiredError{
		ResourceMetadataURL: metadataURL,
	}

	// Verify IsAuthorizationRequiredError returns true
	if !IsAuthorizationRequiredError(err) {
		t.Errorf("Expected IsAuthorizationRequiredError to return true for AuthorizationRequiredError")
	}

	// Verify GetResourceMetadataURL returns the correct URL
	result := GetResourceMetadataURL(err)
	if result != metadataURL {
		t.Errorf("Expected GetResourceMetadataURL to return %q, got %q", metadataURL, result)
	}

	// Test with OAuthAuthorizationRequiredError (different type)
	oauthErr := &transport.OAuthAuthorizationRequiredError{
		Handler: transport.NewOAuthHandler(transport.OAuthConfig{}),
		AuthorizationRequiredError: transport.AuthorizationRequiredError{
			ResourceMetadataURL: metadataURL,
		},
	}

	// Verify IsOAuthAuthorizationRequiredError returns true
	if !IsOAuthAuthorizationRequiredError(oauthErr) {
		t.Errorf("Expected IsOAuthAuthorizationRequiredError to return true for OAuthAuthorizationRequiredError")
	}

	// Verify GetResourceMetadataURL works with OAuth error too
	result2 := GetResourceMetadataURL(oauthErr)
	if result2 != metadataURL {
		t.Errorf("Expected GetResourceMetadataURL to return %q, got %q", metadataURL, result2)
	}

	// Test with non-authorization error
	err3 := fmt.Errorf("some other error")

	// Verify IsAuthorizationRequiredError returns false
	if IsAuthorizationRequiredError(err3) {
		t.Errorf("Expected IsAuthorizationRequiredError to return false for non-authorization error")
	}
}
