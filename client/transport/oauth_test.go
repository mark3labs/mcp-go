package transport

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestToken_IsExpired(t *testing.T) {
	// Test cases
	testCases := []struct {
		name     string
		token    Token
		expected bool
	}{
		{
			name: "Valid token",
			token: Token{
				AccessToken: "valid-token",
				ExpiresAt:   time.Now().Add(1 * time.Hour),
			},
			expected: false,
		},
		{
			name: "Expired token",
			token: Token{
				AccessToken: "expired-token",
				ExpiresAt:   time.Now().Add(-1 * time.Hour),
			},
			expected: true,
		},
		{
			name: "Token with no expiration",
			token: Token{
				AccessToken: "no-expiration-token",
			},
			expected: false,
		},
	}

	// Run test cases
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.token.IsExpired()
			if result != tc.expected {
				t.Errorf("Expected IsExpired() to return %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestMemoryTokenStore(t *testing.T) {
	// Create a token store
	store := NewMemoryTokenStore()

	// Test getting token from empty store
	_, err := store.GetToken()
	if err == nil {
		t.Errorf("Expected error when getting token from empty store")
	}

	// Create a test token
	token := &Token{
		AccessToken:  "test-token",
		TokenType:    "Bearer",
		RefreshToken: "refresh-token",
		ExpiresIn:    3600,
		ExpiresAt:    time.Now().Add(1 * time.Hour),
	}

	// Save the token
	err = store.SaveToken(token)
	if err != nil {
		t.Fatalf("Failed to save token: %v", err)
	}

	// Get the token
	retrievedToken, err := store.GetToken()
	if err != nil {
		t.Fatalf("Failed to get token: %v", err)
	}

	// Verify the token
	if retrievedToken.AccessToken != token.AccessToken {
		t.Errorf("Expected access token to be %s, got %s", token.AccessToken, retrievedToken.AccessToken)
	}
	if retrievedToken.TokenType != token.TokenType {
		t.Errorf("Expected token type to be %s, got %s", token.TokenType, retrievedToken.TokenType)
	}
	if retrievedToken.RefreshToken != token.RefreshToken {
		t.Errorf("Expected refresh token to be %s, got %s", token.RefreshToken, retrievedToken.RefreshToken)
	}
}

func TestValidateRedirectURI(t *testing.T) {
	// Test cases
	testCases := []struct {
		name        string
		redirectURI string
		expectError bool
	}{
		{
			name:        "Valid HTTPS URI",
			redirectURI: "https://example.com/callback",
			expectError: false,
		},
		{
			name:        "Valid localhost URI",
			redirectURI: "http://localhost:8085/callback",
			expectError: false,
		},
		{
			name:        "Valid localhost URI with 127.0.0.1",
			redirectURI: "http://127.0.0.1:8085/callback",
			expectError: false,
		},
		{
			name:        "Invalid HTTP URI (non-localhost)",
			redirectURI: "http://example.com/callback",
			expectError: true,
		},
		{
			name:        "Invalid HTTP URI with 'local' in domain",
			redirectURI: "http://localdomain.com/callback",
			expectError: true,
		},
		{
			name:        "Empty URI",
			redirectURI: "",
			expectError: true,
		},
		{
			name:        "Invalid scheme",
			redirectURI: "ftp://example.com/callback",
			expectError: true,
		},
		{
			name:        "IPv6 localhost",
			redirectURI: "http://[::1]:8080/callback",
			expectError: false, // IPv6 localhost is valid
		},
	}

	// Run test cases
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRedirectURI(tc.redirectURI)
			if tc.expectError && err == nil {
				t.Errorf("Expected error for redirect URI %s, got nil", tc.redirectURI)
			} else if !tc.expectError && err != nil {
				t.Errorf("Expected no error for redirect URI %s, got %v", tc.redirectURI, err)
			}
		})
	}
}

func TestOAuthHandler_GetAuthorizationHeader_EmptyAccessToken(t *testing.T) {
	// Create a token store with a token that has an empty access token
	tokenStore := NewMemoryTokenStore()
	invalidToken := &Token{
		AccessToken:  "", // Empty access token
		TokenType:    "Bearer",
		RefreshToken: "refresh-token",
		ExpiresIn:    3600,
		ExpiresAt:    time.Now().Add(1 * time.Hour), // Valid for 1 hour
	}
	if err := tokenStore.SaveToken(invalidToken); err != nil {
		t.Fatalf("Failed to save token: %v", err)
	}

	// Create an OAuth handler
	config := OAuthConfig{
		ClientID:     "test-client",
		RedirectURI:  "http://localhost:8085/callback",
		Scopes:       []string{"mcp.read", "mcp.write"},
		TokenStore:   tokenStore,
		PKCEEnabled:  true,
	}

	handler := NewOAuthHandler(config)

	// Test getting authorization header with empty access token
	_, err := handler.GetAuthorizationHeader(context.Background())
	if err == nil {
		t.Fatalf("Expected error when getting authorization header with empty access token")
	}

	// Verify the error message
	if !errors.Is(err, ErrOAuthAuthorizationRequired) {
		t.Errorf("Expected error to be ErrOAuthAuthorizationRequired, got %v", err)
	}
}

func TestOAuthHandler_GetServerMetadata_EmptyURL(t *testing.T) {
	// Create an OAuth handler with an empty AuthServerMetadataURL
	config := OAuthConfig{
		ClientID:              "test-client",
		RedirectURI:           "http://localhost:8085/callback",
		Scopes:                []string{"mcp.read"},
		TokenStore:            NewMemoryTokenStore(),
		AuthServerMetadataURL: "", // Empty URL
		PKCEEnabled:           true,
	}

	handler := NewOAuthHandler(config)

	// Test getting server metadata with empty URL
	_, err := handler.GetServerMetadata(context.Background())
	if err == nil {
		t.Fatalf("Expected error when getting server metadata with empty URL")
	}

	// Verify the error message
	if !strings.Contains(err.Error(), "AuthServerMetadataURL is required") {
		t.Errorf("Expected error message to contain 'AuthServerMetadataURL is required', got %s", err.Error())
	}
}