package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
	ctx := context.Background()

	// Test getting token from empty store
	_, err := store.GetToken(ctx)
	if !errors.Is(err, ErrNoToken) {
		t.Errorf("Expected ErrNoToken when getting token from empty store, got %v", err)
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
	err = store.SaveToken(ctx, token)
	if err != nil {
		t.Fatalf("Failed to save token: %v", err)
	}

	// Get the token
	retrievedToken, err := store.GetToken(ctx)
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
	ctx := context.Background()
	tokenStore := NewMemoryTokenStore()
	invalidToken := &Token{
		AccessToken:  "", // Empty access token
		TokenType:    "Bearer",
		RefreshToken: "refresh-token",
		ExpiresIn:    3600,
		ExpiresAt:    time.Now().Add(1 * time.Hour), // Valid for 1 hour
	}
	if err := tokenStore.SaveToken(ctx, invalidToken); err != nil {
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

func TestMemoryTokenStore_ContextCancellation(t *testing.T) {
	store := NewMemoryTokenStore()

	t.Run("GetToken with canceled context", func(t *testing.T) {
		// Create a canceled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// Attempt to get token with canceled context
		_, err := store.GetToken(ctx)

		// Should return context.Canceled error
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled error, got %v", err)
		}
	})

	t.Run("SaveToken with canceled context", func(t *testing.T) {
		// Create a canceled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		token := &Token{
			AccessToken: "test-token",
			TokenType:   "Bearer",
		}

		// Attempt to save token with canceled context
		err := store.SaveToken(ctx, token)

		// Should return context.Canceled error
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled error, got %v", err)
		}
	})

	t.Run("GetToken with deadline exceeded", func(t *testing.T) {
		// Create a context with past deadline
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
		defer cancel()

		// Attempt to get token with expired context
		_, err := store.GetToken(ctx)

		// Should return context.DeadlineExceeded error
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Expected context.DeadlineExceeded error, got %v", err)
		}
	})

	t.Run("SaveToken with deadline exceeded", func(t *testing.T) {
		// Create a context with past deadline
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
		defer cancel()

		token := &Token{
			AccessToken: "test-token",
			TokenType:   "Bearer",
		}

		// Attempt to save token with expired context
		err := store.SaveToken(ctx, token)

		// Should return context.DeadlineExceeded error
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Expected context.DeadlineExceeded error, got %v", err)
		}
	})
}

func TestOAuthHandler_GetAuthorizationHeader_ContextCancellation(t *testing.T) {
	// Create a token store with a valid token
	tokenStore := NewMemoryTokenStore()
	validToken := &Token{
		AccessToken:  "test-token",
		TokenType:    "Bearer",
		RefreshToken: "refresh-token",
		ExpiresIn:    3600,
		ExpiresAt:    time.Now().Add(1 * time.Hour),
	}

	// Save the token with a valid context
	ctx := context.Background()
	if err := tokenStore.SaveToken(ctx, validToken); err != nil {
		t.Fatalf("Failed to save token: %v", err)
	}

	config := OAuthConfig{
		ClientID:    "test-client",
		RedirectURI: "http://localhost:8085/callback",
		Scopes:      []string{"mcp.read", "mcp.write"},
		TokenStore:  tokenStore,
		PKCEEnabled: true,
	}

	handler := NewOAuthHandler(config)

	t.Run("GetAuthorizationHeader with canceled context", func(t *testing.T) {
		// Create a canceled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// Attempt to get authorization header with canceled context
		_, err := handler.GetAuthorizationHeader(ctx)

		// Should return context.Canceled error (propagated from TokenStore.GetToken)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled error, got %v", err)
		}
	})

	t.Run("GetAuthorizationHeader with deadline exceeded", func(t *testing.T) {
		// Create a context with past deadline
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
		defer cancel()

		// Attempt to get authorization header with expired context
		_, err := handler.GetAuthorizationHeader(ctx)

		// Should return context.DeadlineExceeded error (propagated from TokenStore.GetToken)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Expected context.DeadlineExceeded error, got %v", err)
		}
	})
}

func TestOAuthHandler_getValidToken_ContextCancellation(t *testing.T) {
	// Use regular MemoryTokenStore for testing
	tokenStore := NewMemoryTokenStore()

	config := OAuthConfig{
		ClientID:    "test-client",
		RedirectURI: "http://localhost:8085/callback",
		Scopes:      []string{"mcp.read", "mcp.write"},
		TokenStore:  tokenStore,
		PKCEEnabled: true,
	}

	handler := NewOAuthHandler(config)

	t.Run("Context canceled during initial token retrieval", func(t *testing.T) {
		// Create a canceled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// This will call getValidToken internally
		_, err := handler.GetAuthorizationHeader(ctx)

		// Should return context.Canceled error
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled error, got %v", err)
		}
	})

	t.Run("Context deadline exceeded during token retrieval", func(t *testing.T) {
		// Create a context with past deadline
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
		defer cancel()

		// This will call getValidToken internally
		_, err := handler.GetAuthorizationHeader(ctx)

		// Should return context.DeadlineExceeded error
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Expected context.DeadlineExceeded error, got %v", err)
		}
	})

	t.Run("Context canceled with existing expired token", func(t *testing.T) {
		// First save an expired token
		expiredToken := &Token{
			AccessToken:  "expired-token",
			TokenType:    "Bearer",
			RefreshToken: "refresh-token",
			ExpiresAt:    time.Now().Add(-1 * time.Hour), // Expired
		}

		validCtx := context.Background()
		if err := tokenStore.SaveToken(validCtx, expiredToken); err != nil {
			t.Fatalf("Failed to save expired token: %v", err)
		}

		// Now try to get authorization header with canceled context
		// This should detect the canceled context during the refresh attempt
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := handler.GetAuthorizationHeader(ctx)

		// Should return context.Canceled error or authorization required
		// (depending on where exactly the cancellation is detected)
		if err == nil {
			t.Errorf("Expected an error due to context cancellation, got nil")
		}

		// The error could be context.Canceled or authorization required
		// Both are valid depending on timing
		if !errors.Is(err, context.Canceled) && !errors.Is(err, ErrOAuthAuthorizationRequired) {
			t.Errorf("Expected context.Canceled or ErrOAuthAuthorizationRequired, got %v", err)
		}
	})
}

func TestOAuthHandler_RefreshToken_ContextCancellation(t *testing.T) {
	// Create a token store with a valid refresh token
	tokenStore := NewMemoryTokenStore()
	tokenWithRefresh := &Token{
		AccessToken:  "expired-access-token",
		TokenType:    "Bearer",
		RefreshToken: "valid-refresh-token",
		ExpiresAt:    time.Now().Add(-1 * time.Hour), // Expired access token
	}

	ctx := context.Background()
	if err := tokenStore.SaveToken(ctx, tokenWithRefresh); err != nil {
		t.Fatalf("Failed to save token with refresh: %v", err)
	}

	config := OAuthConfig{
		ClientID:              "test-client",
		ClientSecret:          "test-secret",
		RedirectURI:           "http://localhost:8085/callback",
		Scopes:                []string{"mcp.read", "mcp.write"},
		TokenStore:            tokenStore,
		AuthServerMetadataURL: "https://example.com/.well-known/oauth-authorization-server",
		PKCEEnabled:           true,
	}

	handler := NewOAuthHandler(config)

	t.Run("RefreshToken with canceled context", func(t *testing.T) {
		// Create a canceled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// Attempt to refresh token with canceled context
		_, err := handler.RefreshToken(ctx, "valid-refresh-token")

		// Should return context.Canceled error (from getting old token)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled error, got %v", err)
		}
	})

	t.Run("RefreshToken with deadline exceeded", func(t *testing.T) {
		// Create a context with past deadline
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
		defer cancel()

		// Attempt to refresh token with expired context
		_, err := handler.RefreshToken(ctx, "valid-refresh-token")

		// Should return context.DeadlineExceeded or context.Canceled error
		// (HTTP client may convert deadline exceeded to canceled)
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) &&
			!strings.Contains(err.Error(), "context canceled") && !strings.Contains(err.Error(), "context deadline exceeded") {
			t.Errorf("Expected context cancellation/deadline error, got %v", err)
		}
	})
}

func TestOAuthHandler_CachedClientContextScenario(t *testing.T) {
	// This test simulates scenarios where a cached MCP client
	// may retain a stale or canceled context, causing context cancellation errors
	// during token retrieval operations.

	t.Run("Cached client with stale context", func(t *testing.T) {
		// Step 1: Create initial client with valid context and token
		tokenStore := NewMemoryTokenStore()
		validToken := &Token{
			AccessToken:  "initial-token",
			TokenType:    "Bearer",
			RefreshToken: "refresh-token",
			ExpiresIn:    3600,
			ExpiresAt:    time.Now().Add(1 * time.Hour),
		}

		// Save token with initial valid context
		initialCtx := context.Background()
		if err := tokenStore.SaveToken(initialCtx, validToken); err != nil {
			t.Fatalf("Failed to save initial token: %v", err)
		}

		config := OAuthConfig{
			ClientID:    "test-client",
			RedirectURI: "http://localhost:8085/callback",
			Scopes:      []string{"mcp.read", "mcp.write"},
			TokenStore:  tokenStore,
			PKCEEnabled: true,
		}

		// Create handler (simulating cached client)
		handler := NewOAuthHandler(config)

		// Verify initial operation works
		authHeader, err := handler.GetAuthorizationHeader(initialCtx)
		if err != nil {
			t.Fatalf("Initial operation should work: %v", err)
		}
		if authHeader != "Bearer initial-token" {
			t.Errorf("Expected 'Bearer initial-token', got %s", authHeader)
		}

		// Step 2: Simulate production scenario - context gets canceled
		// (this could happen due to request timeout, user cancellation, etc.)
		staleCancelableCtx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately to simulate stale context

		// Step 3: Try to use cached client with canceled context
		// This should properly detect context cancellation instead of causing
		// mysterious database errors or other issues
		_, err = handler.GetAuthorizationHeader(staleCancelableCtx)

		// Verify we get proper context cancellation error
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled error, got %v", err)
		}
	})

	t.Run("Cached client with context deadline exceeded", func(t *testing.T) {
		// Similar test but with deadline exceeded context
		tokenStore := NewMemoryTokenStore()
		validToken := &Token{
			AccessToken: "deadline-test-token",
			TokenType:   "Bearer",
			ExpiresAt:   time.Now().Add(1 * time.Hour),
		}

		// Save token with valid context
		validCtx := context.Background()
		if err := tokenStore.SaveToken(validCtx, validToken); err != nil {
			t.Fatalf("Failed to save token: %v", err)
		}

		config := OAuthConfig{
			ClientID:    "test-client",
			RedirectURI: "http://localhost:8085/callback",
			TokenStore:  tokenStore,
		}

		handler := NewOAuthHandler(config)

		// Create context with past deadline (simulating expired request context)
		expiredCtx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
		defer cancel()

		// Try to use cached client with expired context
		_, err := handler.GetAuthorizationHeader(expiredCtx)

		// Should get deadline exceeded error
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Expected context.DeadlineExceeded error, got %v", err)
		}
	})

	t.Run("Token refresh with canceled context during refresh", func(t *testing.T) {
		// Test the scenario where context gets canceled during token refresh
		// This simulates timing issues that can occur with context cancellation
		tokenStore := NewMemoryTokenStore()

		// Create an expired token that would trigger refresh
		expiredToken := &Token{
			AccessToken:  "expired-token",
			TokenType:    "Bearer",
			RefreshToken: "refresh-token",
			ExpiresAt:    time.Now().Add(-1 * time.Hour), // Expired
		}

		// Save expired token
		validCtx := context.Background()
		if err := tokenStore.SaveToken(validCtx, expiredToken); err != nil {
			t.Fatalf("Failed to save expired token: %v", err)
		}

		config := OAuthConfig{
			ClientID:              "test-client",
			ClientSecret:          "test-secret",
			RedirectURI:           "http://localhost:8085/callback",
			TokenStore:            tokenStore,
			AuthServerMetadataURL: "https://example.com/.well-known/oauth-authorization-server",
		}

		handler := NewOAuthHandler(config)

		// Create a context that's already canceled (simulating race condition)
		canceledCtx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel before the operation

		// This should detect the canceled context early in the refresh process
		_, err := handler.GetAuthorizationHeader(canceledCtx)

		// Should get context.Canceled error
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled error during refresh, got %v", err)
		}

		// Verify the error message is appropriate
		if strings.Contains(err.Error(), "database") || strings.Contains(err.Error(), "connection refused") {
			t.Errorf("Error message should not mention unrelated issues, got: %v", err)
		}
	})
}

func TestOAuthHandler_GetServerMetadata_FallbackToOAuthAuthorizationServer(t *testing.T) {
	// Test that when protected resource request fails, the handler falls back to
	// .well-known/oauth-authorization-server instead of getDefaultEndpoints

	protectedResourceRequested := false
	authServerRequested := false

	// Create a test server that simulates the OAuth provider
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			// Simulate failure of protected resource request
			protectedResourceRequested = true
			w.WriteHeader(http.StatusNotFound)

		case "/.well-known/oauth-authorization-server":
			// Return OAuth Authorization Server metadata
			authServerRequested = true
			metadata := AuthServerMetadata{
				Issuer:                server.URL,
				AuthorizationEndpoint: server.URL + "/authorize",
				TokenEndpoint:         server.URL + "/token",
				RegistrationEndpoint:  server.URL + "/register",
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(metadata); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Create OAuth handler with empty AuthServerMetadataURL to trigger discovery
	config := OAuthConfig{
		ClientID:              "test-client",
		RedirectURI:           server.URL + "/callback",
		Scopes:                []string{"mcp.read"},
		TokenStore:            NewMemoryTokenStore(),
		AuthServerMetadataURL: "", // Empty to trigger discovery
		PKCEEnabled:           true,
	}

	handler := NewOAuthHandler(config)
	handler.SetBaseURL(server.URL)

	// Call getServerMetadata which should trigger the fallback behavior
	metadata, err := handler.GetServerMetadata(context.Background())
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify that both requests were made in the correct order
	if !protectedResourceRequested {
		t.Error("Expected protected resource request to be made")
	}
	if !authServerRequested {
		t.Error("Expected OAuth Authorization Server request to be made as fallback")
	}

	// Verify the metadata was correctly parsed
	if metadata.Issuer != server.URL {
		t.Errorf("Expected issuer to be %s, got %s", server.URL, metadata.Issuer)
	}
	if metadata.AuthorizationEndpoint != server.URL+"/authorize" {
		t.Errorf("Expected authorization endpoint to be %s/authorize, got %s", server.URL, metadata.AuthorizationEndpoint)
	}
	if metadata.TokenEndpoint != server.URL+"/token" {
		t.Errorf("Expected token endpoint to be %s/token, got %s", server.URL, metadata.TokenEndpoint)
	}
}

func TestOAuthHandler_GetServerMetadata_FallbackToDefaultEndpoints(t *testing.T) {
	// Test that when both protected resource and oauth-authorization-server fail,
	// the handler falls back to default endpoints

	protectedResourceRequested := false
	authServerRequested := false

	// Create a test server that returns 404 for both endpoints
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			protectedResourceRequested = true
			w.WriteHeader(http.StatusNotFound)

		case "/.well-known/oauth-authorization-server":
			authServerRequested = true
			w.WriteHeader(http.StatusNotFound)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Create OAuth handler
	config := OAuthConfig{
		ClientID:              "test-client",
		RedirectURI:           server.URL + "/callback",
		Scopes:                []string{"mcp.read"},
		TokenStore:            NewMemoryTokenStore(),
		AuthServerMetadataURL: "", // Empty to trigger discovery
		PKCEEnabled:           true,
	}

	handler := NewOAuthHandler(config)
	handler.SetBaseURL(server.URL)

	// Call getServerMetadata which should fall back to default endpoints
	metadata, err := handler.GetServerMetadata(context.Background())
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify that both discovery requests were made
	if !protectedResourceRequested {
		t.Error("Expected protected resource request to be made")
	}
	if !authServerRequested {
		t.Error("Expected OAuth Authorization Server request to be made as first fallback")
	}

	// Verify the default endpoints were used
	if metadata.Issuer != server.URL {
		t.Errorf("Expected issuer to be %s, got %s", server.URL, metadata.Issuer)
	}
	if metadata.AuthorizationEndpoint != server.URL+"/authorize" {
		t.Errorf("Expected authorization endpoint to be %s/authorize, got %s", server.URL, metadata.AuthorizationEndpoint)
	}
	if metadata.TokenEndpoint != server.URL+"/token" {
		t.Errorf("Expected token endpoint to be %s/token, got %s", server.URL, metadata.TokenEndpoint)
	}
}
