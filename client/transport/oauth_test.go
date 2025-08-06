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
		ClientID:    "test-client",
		RedirectURI: "http://localhost:8085/callback",
		Scopes:      []string{"mcp.read", "mcp.write"},
		TokenStore:  tokenStore,
		PKCEEnabled: true,
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

	// Verify the error message contains something about a connection error
	// since we're now trying to connect to the well-known endpoint
	if !strings.Contains(err.Error(), "connection refused") &&
		!strings.Contains(err.Error(), "failed to send protected resource request") {
		t.Errorf("Expected error message to contain connection error, got %s", err.Error())
	}
}

func TestOAuthError(t *testing.T) {
	testCases := []struct {
		name        string
		errorCode   string
		description string
		uri         string
		expected    string
	}{
		{
			name:        "Error with description",
			errorCode:   "invalid_request",
			description: "The request is missing a required parameter",
			uri:         "https://example.com/errors/invalid_request",
			expected:    "OAuth error: invalid_request - The request is missing a required parameter",
		},
		{
			name:        "Error without description",
			errorCode:   "unauthorized_client",
			description: "",
			uri:         "",
			expected:    "OAuth error: unauthorized_client",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			oauthErr := OAuthError{
				ErrorCode:        tc.errorCode,
				ErrorDescription: tc.description,
				ErrorURI:         tc.uri,
			}

			if oauthErr.Error() != tc.expected {
				t.Errorf("Expected error message %q, got %q", tc.expected, oauthErr.Error())
			}
		})
	}
}

func TestOAuthHandler_ProcessAuthorizationResponse_StateValidation(t *testing.T) {
	// Create an OAuth handler
	config := OAuthConfig{
		ClientID:              "test-client",
		RedirectURI:           "http://localhost:8085/callback",
		Scopes:                []string{"mcp.read", "mcp.write"},
		TokenStore:            NewMemoryTokenStore(),
		AuthServerMetadataURL: "http://example.com/.well-known/oauth-authorization-server",
		PKCEEnabled:           true,
	}

	handler := NewOAuthHandler(config)

	// Mock the server metadata to avoid nil pointer dereference
	handler.serverMetadata = &AuthServerMetadata{
		Issuer:                "http://example.com",
		AuthorizationEndpoint: "http://example.com/authorize",
		TokenEndpoint:         "http://example.com/token",
	}

	// Set the expected state
	expectedState := "test-state-123"
	handler.expectedState = expectedState

	// Test with non-matching state - this should fail immediately with ErrInvalidState
	// before trying to connect to any server
	err := handler.ProcessAuthorizationResponse(context.Background(), "test-code", "wrong-state", "test-code-verifier")
	if !errors.Is(err, ErrInvalidState) {
		t.Errorf("Expected ErrInvalidState, got %v", err)
	}

	// Test with empty expected state
	handler.expectedState = ""
	err = handler.ProcessAuthorizationResponse(context.Background(), "test-code", expectedState, "test-code-verifier")
	if err == nil {
		t.Errorf("Expected error with empty expected state, got nil")
	}
	if errors.Is(err, ErrInvalidState) {
		t.Errorf("Got ErrInvalidState when expected a different error for empty expected state")
	}
}

func TestOAuthHandler_SetExpectedState_CrossRequestScenario(t *testing.T) {
	// Simulate the scenario where different OAuthHandler instances are used
	// for initialization and callback steps (different HTTP request handlers)

	config := OAuthConfig{
		ClientID:              "test-client",
		RedirectURI:           "http://localhost:8085/callback",
		Scopes:                []string{"mcp.read", "mcp.write"},
		TokenStore:            NewMemoryTokenStore(),
		AuthServerMetadataURL: "http://example.com/.well-known/oauth-authorization-server",
		PKCEEnabled:           true,
	}

	// Step 1: First handler instance (initialization request)
	// This simulates the handler that generates the authorization URL
	handler1 := NewOAuthHandler(config)

	// Mock the server metadata for the first handler
	handler1.serverMetadata = &AuthServerMetadata{
		Issuer:                "http://example.com",
		AuthorizationEndpoint: "http://example.com/authorize",
		TokenEndpoint:         "http://example.com/token",
	}

	// Generate state and get authorization URL (this would typically be done in the init handler)
	testState := "generated-state-value-123"
	_, err := handler1.GetAuthorizationURL(context.Background(), testState, "test-code-challenge")
	if err != nil {
		// We expect this to fail since we're not actually connecting to a server,
		// but it should still store the expected state
		if !strings.Contains(err.Error(), "connection") && !strings.Contains(err.Error(), "dial") {
			t.Errorf("Expected connection error, got: %v", err)
		}
	}

	// Verify the state was stored in the first handler
	if handler1.GetExpectedState() != testState {
		t.Errorf("Expected state %s to be stored in first handler, got %s", testState, handler1.GetExpectedState())
	}

	// Step 2: Second handler instance (callback request)
	// This simulates a completely separate handler instance that would be created
	// in a different HTTP request handler for processing the OAuth callback
	handler2 := NewOAuthHandler(config)

	// Mock the server metadata for the second handler
	handler2.serverMetadata = &AuthServerMetadata{
		Issuer:                "http://example.com",
		AuthorizationEndpoint: "http://example.com/authorize",
		TokenEndpoint:         "http://example.com/token",
	}

	// Initially, the second handler has no expected state
	if handler2.GetExpectedState() != "" {
		t.Errorf("Expected second handler to have empty state initially, got %s", handler2.GetExpectedState())
	}

	// Step 3: Transfer the state from the first handler to the second
	// This is the key functionality being tested - setting the expected state
	// in a different handler instance
	handler2.SetExpectedState(testState)

	// Verify the state was transferred correctly
	if handler2.GetExpectedState() != testState {
		t.Errorf("Expected state %s to be set in second handler, got %s", testState, handler2.GetExpectedState())
	}

	// Step 4: Test that state validation works correctly in the second handler

	// Test with correct state - should pass validation but fail at token exchange
	// (since we're not actually running a real OAuth server)
	err = handler2.ProcessAuthorizationResponse(context.Background(), "test-code", testState, "test-code-verifier")
	if err == nil {
		t.Errorf("Expected error due to token exchange failure, got nil")
	}
	// Should NOT be ErrInvalidState since the state matches
	if errors.Is(err, ErrInvalidState) {
		t.Errorf("Got ErrInvalidState with matching state, should have failed at token exchange instead")
	}

	// Verify state was cleared after processing (even though token exchange failed)
	if handler2.GetExpectedState() != "" {
		t.Errorf("Expected state to be cleared after processing, got %s", handler2.GetExpectedState())
	}

	// Step 5: Test with wrong state after resetting
	handler2.SetExpectedState("different-state-value")
	err = handler2.ProcessAuthorizationResponse(context.Background(), "test-code", testState, "test-code-verifier")
	if !errors.Is(err, ErrInvalidState) {
		t.Errorf("Expected ErrInvalidState with wrong state, got %v", err)
	}
}
