package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		RedirectURI:           "",
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

	if !strings.Contains(err.Error(), "failed to extract base URL") &&
		!strings.Contains(err.Error(), "no base URL available") {
		t.Errorf("Expected error message to mention missing base URL, got %s", err.Error())
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

func TestOAuthHandler_GetServerMetadata_PathAwareDiscovery(t *testing.T) {
	protectedResourceRequested := false
	authServerRequested := false

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource/googledrive":
			protectedResourceRequested = true
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(OAuthProtectedResource{
				AuthorizationServers: []string{server.URL + "/oauth/googledrive"},
			})
		case "/.well-known/oauth-authorization-server/oauth/googledrive":
			authServerRequested = true
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(AuthServerMetadata{
				Issuer:                server.URL + "/oauth/googledrive",
				AuthorizationEndpoint: server.URL + "/oauth/googledrive/authorize",
				TokenEndpoint:         server.URL + "/oauth/googledrive/token",
				RegistrationEndpoint:  server.URL + "/oauth/googledrive/register",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	handler := NewOAuthHandler(OAuthConfig{
		ClientID:    "test-client",
		RedirectURI: "http://localhost/callback",
		TokenStore:  NewMemoryTokenStore(),
	})
	handler.SetBaseURL(server.URL + "/googledrive")

	metadata, err := handler.GetServerMetadata(context.Background())
	require.NoError(t, err)
	assert.True(t, protectedResourceRequested)
	assert.True(t, authServerRequested)
	assert.Equal(t, server.URL+"/oauth/googledrive", metadata.Issuer)
	assert.Equal(t, server.URL+"/oauth/googledrive/authorize", metadata.AuthorizationEndpoint)
	assert.Equal(t, server.URL+"/oauth/googledrive/token", metadata.TokenEndpoint)
}

// TestOAuthHandler_RefreshToken_GitHubErrorIn200Response tests that we properly detect
// GitHub's non-spec-compliant behavior of returning HTTP 200 with error details in the JSON body
func TestOAuthHandler_RefreshToken_GitHubErrorIn200Response(t *testing.T) {
	tokenStore := NewMemoryTokenStore()

	// Create a server that returns HTTP 200 with an error in the body (GitHub's behavior)
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"authorization_endpoint": serverURL + "/authorize",
				"token_endpoint":         serverURL + "/token",
			})
			return
		}

		if r.URL.Path == "/token" {
			// Return HTTP 200 but with error in JSON body (GitHub's behavior)
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":             "bad_refresh_token",
				"error_description": "The refresh token passed is incorrect or expired.",
				"error_uri":         "https://docs.github.com/apps/oauth",
			})
			return
		}
	}))
	serverURL = server.URL
	defer server.Close()

	config := OAuthConfig{
		ClientID:              "test-client",
		ClientSecret:          "test-secret",
		RedirectURI:           "http://localhost/callback",
		TokenStore:            tokenStore,
		AuthServerMetadataURL: server.URL + "/.well-known/oauth-authorization-server",
	}

	handler := NewOAuthHandler(config)

	// Attempt to refresh with a "bad" token
	ctx := context.Background()
	_, err := handler.RefreshToken(ctx, "bad-refresh-token")

	// Should detect the error even though status code is 200
	require.Error(t, err, "Expected error when GitHub returns error in 200 response")
	assert.Contains(t, err.Error(), "bad_refresh_token", "Error should contain OAuth error code")
	assert.Contains(t, err.Error(), "incorrect or expired", "Error should contain error description")

	// Verify no token was saved to token store (regression test for original bug)
	_, getErr := tokenStore.GetToken(ctx)
	assert.ErrorIs(t, getErr, ErrNoToken, "No token should be saved after OAuth error in 200 response")
}

// TestOAuthHandler_RefreshToken_EmptyAccessToken tests that mcp-go properly parses
// responses where access_token is missing or empty (GitHub can return this on errors)
// Note: The integration layer's MCPTokenStore validates and rejects empty tokens,
// but mcp-go itself just parses what it receives.
func TestOAuthHandler_RefreshToken_EmptyAccessToken(t *testing.T) {
	tokenStore := NewMemoryTokenStore()

	// Create a server that returns a token response with empty access_token
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"authorization_endpoint": serverURL + "/authorize",
				"token_endpoint":         serverURL + "/token",
			})
			return
		}

		if r.URL.Path == "/token" {
			// Return empty access_token (unusual but can happen with malformed responses)
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "", // Empty!
				"token_type":    "bearer",
				"expires_in":    28800,
				"refresh_token": "ghr_newrefreshtoken",
			})
			return
		}
	}))
	serverURL = server.URL
	defer server.Close()

	config := OAuthConfig{
		ClientID:              "test-client",
		ClientSecret:          "test-secret",
		RedirectURI:           "http://localhost/callback",
		TokenStore:            tokenStore,
		AuthServerMetadataURL: server.URL + "/.well-known/oauth-authorization-server",
	}

	handler := NewOAuthHandler(config)

	ctx := context.Background()
	token, err := handler.RefreshToken(ctx, "test-refresh-token")

	// mcp-go doesn't validate empty tokens - it just parses the response
	// The integration layer (MCPTokenStore) is responsible for validation
	require.NoError(t, err, "mcp-go should successfully parse the response")

	// Verify the token has empty access_token (as returned by server)
	assert.Equal(t, "", token.AccessToken, "Should parse empty access_token from response")
	assert.Equal(t, "ghr_newrefreshtoken", token.RefreshToken, "Should have refresh token")

	// Note: In production, the integration layer's MCPTokenStore.SaveToken would
	// reject this with: "access token is empty, refusing to save"
}

// TestOAuthHandler_RefreshToken_RefreshTokenRotation tests that we properly save
// the new refresh token when GitHub rotates it
func TestOAuthHandler_RefreshToken_RefreshTokenRotation(t *testing.T) {
	tokenStore := NewMemoryTokenStore()

	callCount := 0
	var lastRefreshTokenSent string

	// Create a server that rotates refresh tokens (GitHub's behavior)
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"authorization_endpoint": serverURL + "/authorize",
				"token_endpoint":         serverURL + "/token",
			})
			return
		}

		if r.URL.Path == "/token" {
			callCount++

			// Parse the request to capture what refresh token was sent
			_ = r.ParseForm()
			lastRefreshTokenSent = r.FormValue("refresh_token")

			// Return a new refresh token each time (rotation)
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "ghu_access_" + strconv.Itoa(callCount),
				"token_type":    "bearer",
				"expires_in":    28800,
				"refresh_token": "ghr_refresh_" + strconv.Itoa(callCount), // New token each time
			})
			return
		}
	}))
	serverURL = server.URL
	defer server.Close()

	config := OAuthConfig{
		ClientID:              "test-client",
		ClientSecret:          "test-secret",
		RedirectURI:           "http://localhost/callback",
		TokenStore:            tokenStore,
		AuthServerMetadataURL: server.URL + "/.well-known/oauth-authorization-server",
	}

	handler := NewOAuthHandler(config)
	ctx := context.Background()
	token1, err := handler.RefreshToken(ctx, "ghr_original")
	require.NoError(t, err, "First refresh should succeed")
	assert.Equal(t, "ghr_refresh_1", token1.RefreshToken, "Should receive new refresh token")
	assert.Equal(t, "ghr_original", lastRefreshTokenSent, "Should send original refresh token")

	// Second refresh - should use the NEW refresh token
	token2, err := handler.RefreshToken(ctx, token1.RefreshToken)
	require.NoError(t, err, "Second refresh should succeed")
	assert.Equal(t, "ghr_refresh_2", token2.RefreshToken, "Should receive rotated refresh token")
	assert.Equal(t, "ghr_refresh_1", lastRefreshTokenSent, "Should send first rotated token")

	// Verify tokens are different
	assert.NotEqual(t, token1.AccessToken, token2.AccessToken, "Access tokens should be different after rotation")
	assert.NotEqual(t, token1.RefreshToken, token2.RefreshToken, "Refresh tokens should be rotated")
}

// TestOAuthHandler_RefreshToken_SingleUseRefreshToken tests that using an old
// refresh token after rotation fails appropriately (GitHub's single-use behavior)
func TestOAuthHandler_RefreshToken_SingleUseRefreshToken(t *testing.T) {
	tokenStore := NewMemoryTokenStore()

	usedTokens := make(map[string]bool)

	// Create a server that tracks used refresh tokens and rejects reuse
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"authorization_endpoint": serverURL + "/authorize",
				"token_endpoint":         serverURL + "/token",
			})
			return
		}

		if r.URL.Path == "/token" {
			_ = r.ParseForm()
			refreshToken := r.FormValue("refresh_token")

			// Check if this refresh token was already used
			if usedTokens[refreshToken] {
				// Return HTTP 200 with error (GitHub's behavior)
				w.WriteHeader(http.StatusOK)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":             "bad_refresh_token",
					"error_description": "The refresh token passed is incorrect or expired.",
				})
				return
			}

			// Mark as used and return new tokens
			usedTokens[refreshToken] = true

			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "ghu_new_access",
				"token_type":    "bearer",
				"expires_in":    28800,
				"refresh_token": "ghr_new_refresh_" + refreshToken, // Rotated token
			})
			return
		}
	}))
	serverURL = server.URL
	defer server.Close()

	config := OAuthConfig{
		ClientID:              "test-client",
		ClientSecret:          "test-secret",
		RedirectURI:           "http://localhost/callback",
		TokenStore:            tokenStore,
		AuthServerMetadataURL: server.URL + "/.well-known/oauth-authorization-server",
	}

	handler := NewOAuthHandler(config)
	ctx := context.Background()

	// First use of refresh token - should succeed
	token1, err := handler.RefreshToken(ctx, "ghr_original")
	require.NoError(t, err, "First refresh should succeed")

	// Try to use the SAME refresh token again - should fail
	_, err = handler.RefreshToken(ctx, "ghr_original")
	require.Error(t, err, "Expected error when reusing old refresh token")
	assert.Contains(t, err.Error(), "bad_refresh_token", "Error should contain bad_refresh_token")
	assert.Contains(t, err.Error(), "OAuth error", "Error should be wrapped as OAuth error")

	// Ensure the previously successful token is still stored after the failed reuse
	savedToken, getErr := tokenStore.GetToken(ctx)
	require.NoError(t, getErr)
	assert.Equal(t, token1.RefreshToken, savedToken.RefreshToken, "Stored refresh token should remain unchanged after failed reuse")

	// Using the NEW refresh token should succeed
	token2, err := handler.RefreshToken(ctx, token1.RefreshToken)
	require.NoError(t, err, "Refresh with new token should succeed")
	assert.NotEmpty(t, token2.AccessToken, "Should have received valid access token")
}

// TestOAuthHandler_ProcessAuthorizationResponse_ErrorIn200 tests that we detect
// errors in the authorization code exchange response
func TestOAuthHandler_ProcessAuthorizationResponse_ErrorIn200(t *testing.T) {
	tokenStore := NewMemoryTokenStore()

	// Create a server that returns HTTP 200 with error for token exchange
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"authorization_endpoint": serverURL + "/authorize",
				"token_endpoint":         serverURL + "/token",
			})
			return
		}

		if r.URL.Path == "/token" {
			// Return HTTP 200 with error in body
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":             "invalid_grant",
				"error_description": "The authorization code is invalid or expired.",
			})
			return
		}
	}))
	serverURL = server.URL
	defer server.Close()

	config := OAuthConfig{
		ClientID:              "test-client",
		ClientSecret:          "test-secret",
		RedirectURI:           server.URL + "/callback",
		TokenStore:            tokenStore,
		AuthServerMetadataURL: server.URL + "/.well-known/oauth-authorization-server",
		PKCEEnabled:           true,
	}

	handler := NewOAuthHandler(config)
	ctx := context.Background()

	// Set expected state
	handler.SetExpectedState("test-state")

	// Try to process authorization response
	err := handler.ProcessAuthorizationResponse(ctx, "bad-code", "test-state", "test-verifier")

	// Should fail with OAuth error
	require.Error(t, err, "Expected error when processing invalid authorization code")
	assert.Contains(t, err.Error(), "invalid_grant", "Error should contain invalid_grant")

	// Verify no token was saved
	_, getErr := tokenStore.GetToken(ctx)
	assert.ErrorIs(t, getErr, ErrNoToken, "No token should be saved after OAuth error in authorization response")
}

// TestOAuthHandler_RefreshToken_KeepsOldRefreshToken tests that when a new
// refresh token is not provided, we keep the old one
func TestOAuthHandler_RefreshToken_KeepsOldRefreshToken(t *testing.T) {
	tokenStore := NewMemoryTokenStore()

	// Create a server that doesn't return a new refresh token
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"authorization_endpoint": serverURL + "/authorize",
				"token_endpoint":         serverURL + "/token",
			})
			return
		}

		if r.URL.Path == "/token" {
			// Return new access token but NO refresh token (some providers do this)
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "ghu_new_access_token",
				"token_type":   "bearer",
				"expires_in":   3600,
				// Note: refresh_token is omitted
			})
			return
		}
	}))
	serverURL = server.URL
	defer server.Close()

	config := OAuthConfig{
		ClientID:              "test-client",
		ClientSecret:          "test-secret",
		RedirectURI:           "http://localhost/callback",
		TokenStore:            tokenStore,
		AuthServerMetadataURL: server.URL + "/.well-known/oauth-authorization-server",
	}

	handler := NewOAuthHandler(config)
	ctx := context.Background()

	originalRefreshToken := "ghr_original_refresh_token"

	// Refresh the token
	newToken, err := handler.RefreshToken(ctx, originalRefreshToken)
	require.NoError(t, err, "Refresh should succeed")

	// Should have new access token
	assert.Equal(t, "ghu_new_access_token", newToken.AccessToken, "Should receive new access token")

	// Should keep the old refresh token
	assert.Equal(t, originalRefreshToken, newToken.RefreshToken, "Should keep old refresh token when server doesn't provide new one")
}

// TestOAuthHandler_RefreshToken_ProperHTTP400Error tests that we handle
// spec-compliant HTTP 400 error responses correctly
func TestOAuthHandler_RefreshToken_ProperHTTP400Error(t *testing.T) {
	tokenStore := NewMemoryTokenStore()

	// Create a server that returns proper HTTP 400 errors (spec-compliant)
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"authorization_endpoint": serverURL + "/authorize",
				"token_endpoint":         serverURL + "/token",
			})
			return
		}

		if r.URL.Path == "/token" {
			// Return HTTP 400 with error (spec-compliant)
			w.WriteHeader(http.StatusBadRequest)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":             "invalid_grant",
				"error_description": "The refresh token is invalid.",
			})
			return
		}
	}))
	serverURL = server.URL
	defer server.Close()

	config := OAuthConfig{
		ClientID:              "test-client",
		ClientSecret:          "test-secret",
		RedirectURI:           "http://localhost/callback",
		TokenStore:            tokenStore,
		AuthServerMetadataURL: server.URL + "/.well-known/oauth-authorization-server",
	}

	handler := NewOAuthHandler(config)
	ctx := context.Background()

	// Attempt refresh
	_, err := handler.RefreshToken(ctx, "invalid-token")

	// Should fail with OAuth error
	require.Error(t, err, "Expected error for HTTP 400 response")
	assert.Contains(t, err.Error(), "invalid_grant", "Error should contain invalid_grant")

	// Verify no token was saved to token store (regression test for original bug)
	_, getErr := tokenStore.GetToken(ctx)
	assert.ErrorIs(t, getErr, ErrNoToken, "No token should be saved after HTTP 400 error")
}

// TestOAuthHandler_RFC8707_ResourceParameter tests that the resource parameter
// from protected resource metadata is included in OAuth requests per RFC 8707
func TestOAuthHandler_RFC8707_ResourceParameter(t *testing.T) {
	t.Run("resource parameter captured from protected resource metadata", func(t *testing.T) {
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/.well-known/oauth-protected-resource":
				// Return protected resource metadata with resource field (RFC 9728)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"resource":              "https://api.example.com/mcp",
					"authorization_servers": []string{server.URL},
				})
			case "/.well-known/oauth-authorization-server":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"issuer":                 server.URL,
					"authorization_endpoint": server.URL + "/authorize",
					"token_endpoint":         server.URL + "/token",
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		config := OAuthConfig{
			ClientID:    "test-client",
			RedirectURI: server.URL + "/callback",
			TokenStore:  NewMemoryTokenStore(),
			PKCEEnabled: true,
		}

		handler := NewOAuthHandler(config)
		handler.SetBaseURL(server.URL)

		// Trigger metadata discovery
		_, err := handler.GetServerMetadata(context.Background())
		require.NoError(t, err)

		// Verify resourceURL was captured
		assert.Equal(t, "https://api.example.com/mcp", handler.resourceURL)
	})

	t.Run("resource parameter falls back to baseURL when not in metadata", func(t *testing.T) {
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/.well-known/oauth-protected-resource":
				// Return metadata WITHOUT resource field - should fall back to baseURL
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"authorization_servers": []string{server.URL},
				})
			case "/.well-known/oauth-authorization-server":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"issuer":                 server.URL,
					"authorization_endpoint": server.URL + "/authorize",
					"token_endpoint":         server.URL + "/token",
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		config := OAuthConfig{
			ClientID:    "test-client",
			RedirectURI: server.URL + "/callback",
			TokenStore:  NewMemoryTokenStore(),
		}

		handler := NewOAuthHandler(config)
		handler.SetBaseURL(server.URL)

		// Trigger metadata discovery
		_, err := handler.GetServerMetadata(context.Background())
		require.NoError(t, err)

		// Per RFC 8707 Section 2: "The client SHOULD use the base URI of the API
		// as the resource parameter value unless specific knowledge of the resource
		// dictates otherwise."
		assert.Equal(t, server.URL, handler.resourceURL, "resourceURL should fall back to baseURL")
	})

	t.Run("resource parameter included in authorization URL", func(t *testing.T) {
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/.well-known/oauth-protected-resource":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"resource":              "https://api.example.com/mcp",
					"authorization_servers": []string{server.URL},
				})
			case "/.well-known/oauth-authorization-server":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"issuer":                 server.URL,
					"authorization_endpoint": server.URL + "/authorize",
					"token_endpoint":         server.URL + "/token",
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		config := OAuthConfig{
			ClientID:    "test-client",
			RedirectURI: server.URL + "/callback",
			TokenStore:  NewMemoryTokenStore(),
			PKCEEnabled: true,
		}

		handler := NewOAuthHandler(config)
		handler.SetBaseURL(server.URL)

		authURL, err := handler.GetAuthorizationURL(context.Background(), "test-state", "test-challenge")
		require.NoError(t, err)

		// Verify resource parameter is in the URL
		assert.Contains(t, authURL, "resource=https%3A%2F%2Fapi.example.com%2Fmcp")
	})

	t.Run("resource parameter included in token exchange", func(t *testing.T) {
		var capturedResource string
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/.well-known/oauth-protected-resource":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"resource":              "https://api.example.com/mcp",
					"authorization_servers": []string{server.URL},
				})
			case "/.well-known/oauth-authorization-server":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"issuer":                 server.URL,
					"authorization_endpoint": server.URL + "/authorize",
					"token_endpoint":         server.URL + "/token",
				})
			case "/token":
				_ = r.ParseForm()
				capturedResource = r.FormValue("resource")
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token":  "test-access-token",
					"token_type":    "Bearer",
					"expires_in":    3600,
					"refresh_token": "test-refresh-token",
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		config := OAuthConfig{
			ClientID:    "test-client",
			RedirectURI: server.URL + "/callback",
			TokenStore:  NewMemoryTokenStore(),
			PKCEEnabled: true,
		}

		handler := NewOAuthHandler(config)
		handler.SetBaseURL(server.URL)
		handler.SetExpectedState("test-state")

		err := handler.ProcessAuthorizationResponse(context.Background(), "test-code", "test-state", "test-verifier")
		require.NoError(t, err)

		// Verify resource parameter was sent in token request
		assert.Equal(t, "https://api.example.com/mcp", capturedResource)
	})

	t.Run("resource parameter included in refresh token request", func(t *testing.T) {
		var capturedResource string
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/.well-known/oauth-protected-resource":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"resource":              "https://api.example.com/mcp",
					"authorization_servers": []string{server.URL},
				})
			case "/.well-known/oauth-authorization-server":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"issuer":                 server.URL,
					"authorization_endpoint": server.URL + "/authorize",
					"token_endpoint":         server.URL + "/token",
				})
			case "/token":
				_ = r.ParseForm()
				capturedResource = r.FormValue("resource")
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token":  "new-access-token",
					"token_type":    "Bearer",
					"expires_in":    3600,
					"refresh_token": "new-refresh-token",
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		config := OAuthConfig{
			ClientID:    "test-client",
			RedirectURI: server.URL + "/callback",
			TokenStore:  NewMemoryTokenStore(),
		}

		handler := NewOAuthHandler(config)
		handler.SetBaseURL(server.URL)

		_, err := handler.RefreshToken(context.Background(), "old-refresh-token")
		require.NoError(t, err)

		// Verify resource parameter was sent in refresh request
		assert.Equal(t, "https://api.example.com/mcp", capturedResource)
	})

	t.Run("baseURL used as resource when not in protected resource metadata", func(t *testing.T) {
		var capturedResource string
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/.well-known/oauth-protected-resource":
				// Return metadata WITHOUT resource field - per RFC 8707 Section 2,
				// client SHOULD use baseURL as resource parameter
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"authorization_servers": []string{server.URL},
				})
			case "/.well-known/oauth-authorization-server":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"issuer":                 server.URL,
					"authorization_endpoint": server.URL + "/authorize",
					"token_endpoint":         server.URL + "/token",
				})
			case "/token":
				_ = r.ParseForm()
				capturedResource = r.FormValue("resource")
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token":  "test-access-token",
					"token_type":    "Bearer",
					"expires_in":    3600,
					"refresh_token": "test-refresh-token",
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		config := OAuthConfig{
			ClientID:    "test-client",
			RedirectURI: server.URL + "/callback",
			TokenStore:  NewMemoryTokenStore(),
		}

		handler := NewOAuthHandler(config)
		handler.SetBaseURL(server.URL)

		_, err := handler.RefreshToken(context.Background(), "test-refresh-token")
		require.NoError(t, err)

		// Per RFC 8707 Section 2: resource SHOULD be sent, falling back to baseURL
		assert.Equal(t, server.URL, capturedResource, "resource should fall back to baseURL")
	})
}

// TestOAuthHandler_GetServerMetadata_AuthServerReturnsHTML tests that when the
// authorization server's .well-known endpoint returns 200 with HTML (e.g. a login
// page) instead of JSON, the fallback chain is not poisoned and default endpoints
// are used successfully.
func TestOAuthHandler_GetServerMetadata_AuthServerReturnsHTML(t *testing.T) {
	// Create a separate "auth server" that returns HTML at its .well-known endpoints
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 200 with HTML for all requests (simulating a login page)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>Login Page</body></html>"))
	}))
	defer authServer.Close()

	// Create the MCP server that returns valid protected resource metadata
	// pointing to the auth server above
	var mcpServerURL string
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-protected-resource" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resource":              mcpServerURL,
				"authorization_servers": []string{authServer.URL},
			})
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	mcpServerURL = mcpServer.URL
	defer mcpServer.Close()

	config := OAuthConfig{
		ClientID:    "test-client",
		RedirectURI: mcpServer.URL + "/callback",
		Scopes:      []string{"mcp.read"},
		TokenStore:  NewMemoryTokenStore(),
		PKCEEnabled: true,
	}

	handler := NewOAuthHandler(config)
	handler.SetBaseURL(mcpServer.URL)

	metadata, err := handler.GetServerMetadata(context.Background())
	require.NoError(t, err, "Should fall back to default endpoints when auth server returns HTML")

	// Verify default endpoints were derived from the auth server URL
	assert.Equal(t, authServer.URL+"/authorize", metadata.AuthorizationEndpoint)
	assert.Equal(t, authServer.URL+"/token", metadata.TokenEndpoint)
	assert.Equal(t, authServer.URL+"/register", metadata.RegistrationEndpoint)
}

func TestExtractResourceMetadataURLs(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   []string
	}{
		{
			name:   "empty header",
			header: "",
			want:   nil,
		},
		{
			name:   "bearer challenge with quoted resource_metadata",
			header: `Bearer resource_metadata="https://example.com/.well-known/oauth-protected-resource/tenant"`,
			want:   []string{"https://example.com/.well-known/oauth-protected-resource/tenant"},
		},
		{
			name:   "bearer challenge with realm, error, and resource_metadata",
			header: `Bearer realm="mcp", error="invalid_token", resource_metadata="https://example.com/.well-known/oauth-protected-resource"`,
			want:   []string{"https://example.com/.well-known/oauth-protected-resource"},
		},
		{
			name:   "parameter name matched case-insensitively",
			header: `Bearer Resource_Metadata="https://example.com/prm"`,
			want:   []string{"https://example.com/prm"},
		},
		{
			name:   "unquoted token value",
			header: `Bearer resource_metadata=abc123`,
			want:   []string{"abc123"},
		},
		{
			name:   "quoted value with escaped quote",
			header: `Bearer resource_metadata="https://example.com/with-\"quote\""`,
			want:   []string{`https://example.com/with-"quote"`},
		},
		{
			name:   "value with tabs and extra whitespace",
			header: "Bearer\tresource_metadata\t=\t\"https://example.com/prm\"",
			want:   []string{"https://example.com/prm"},
		},
		{
			name:   "no resource_metadata parameter",
			header: `Bearer realm="mcp", error="invalid_token"`,
			want:   nil,
		},
		{
			name:   "word containing resource_metadata is not a match",
			header: `Bearer realm="foo resource_metadata bar"`,
			want:   nil,
		},
		{
			name:   "missing equals after parameter name",
			header: `Bearer resource_metadata`,
			want:   nil,
		},
		{
			name:   "truncated quoted value returns what was read",
			header: `Bearer resource_metadata="https://example.com/prm`,
			want:   []string{"https://example.com/prm"},
		},
		{
			name:   "returns all occurrences in order",
			header: `Bearer resource_metadata="https://example.com/a", resource_metadata="https://example.com/b"`,
			want:   []string{"https://example.com/a", "https://example.com/b"},
		},
		{
			name:   "multiple Bearer challenges each with resource_metadata",
			header: `Bearer realm="tenant-a", resource_metadata="https://example.com/a", Bearer realm="tenant-b", resource_metadata="https://example.com/b"`,
			want:   []string{"https://example.com/a", "https://example.com/b"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractResourceMetadataURLs(tc.header)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestOAuthHandler_ProtectedResourceMetadataURL_SetGet(t *testing.T) {
	handler := NewOAuthHandler(OAuthConfig{
		ClientID:    "test-client",
		RedirectURI: "http://localhost/callback",
	})

	assert.Equal(t, "", handler.ProtectedResourceMetadataURL(), "should default to empty")

	handler.SetProtectedResourceMetadataURL("https://example.com/prm")
	assert.Equal(t, "https://example.com/prm", handler.ProtectedResourceMetadataURL())

	handler.SetProtectedResourceMetadataURL("")
	assert.Equal(t, "", handler.ProtectedResourceMetadataURL(), "empty value should clear")
}

func TestOAuthHandler_HandleUnauthorizedResponse(t *testing.T) {
	const baseURL = "https://example.com/mcp"
	cases := []struct {
		name     string
		response *http.Response
		want     string
	}{
		{
			name:     "nil response is a no-op",
			response: nil,
			want:     "",
		},
		{
			name: "no WWW-Authenticate header leaves PRM unset",
			response: &http.Response{
				Header: http.Header{},
			},
			want: "",
		},
		{
			name: "bearer challenge without resource_metadata leaves PRM unset",
			response: &http.Response{
				Header: http.Header{
					"Www-Authenticate": []string{`Bearer realm="mcp"`},
				},
			},
			want: "",
		},
		{
			name: "bearer challenge with same-origin resource_metadata stores the URL",
			response: &http.Response{
				Header: http.Header{
					"Www-Authenticate": []string{`Bearer resource_metadata="https://example.com/mcp/.well-known/oauth-protected-resource"`},
				},
			},
			want: "https://example.com/mcp/.well-known/oauth-protected-resource",
		},
		{
			name: "cross-host PRM URL is rejected",
			response: &http.Response{
				Header: http.Header{
					"Www-Authenticate": []string{`Bearer resource_metadata="https://attacker.example/evil-prm"`},
				},
			},
			want: "",
		},
		{
			name: "scheme-downgraded PRM URL is rejected",
			response: &http.Response{
				Header: http.Header{
					"Www-Authenticate": []string{`Bearer resource_metadata="http://example.com/prm"`},
				},
			},
			want: "",
		},
		{
			name: "unparseable PRM URL is rejected",
			response: &http.Response{
				Header: http.Header{
					"Www-Authenticate": []string{`Bearer resource_metadata="://not-a-url"`},
				},
			},
			want: "",
		},
		{
			name: "multiple WWW-Authenticate headers: picks first that validates",
			response: &http.Response{
				Header: http.Header{
					"Www-Authenticate": []string{
						`Basic realm="mcp"`,
						`Bearer realm="mcp", resource_metadata="https://example.com/mcp/prm"`,
					},
				},
			},
			want: "https://example.com/mcp/prm",
		},
		{
			name: "multiple headers: skips cross-origin and takes the valid one",
			response: &http.Response{
				Header: http.Header{
					"Www-Authenticate": []string{
						`Bearer resource_metadata="https://attacker.example/evil"`,
						`Bearer resource_metadata="https://example.com/mcp/prm"`,
					},
				},
			},
			want: "https://example.com/mcp/prm",
		},
		{
			name: "single header with two resource_metadata params: bad first, good second",
			response: &http.Response{
				Header: http.Header{
					"Www-Authenticate": []string{
						`Bearer resource_metadata="https://attacker.example/evil", Bearer resource_metadata="https://example.com/mcp/prm"`,
					},
				},
			},
			want: "https://example.com/mcp/prm",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewOAuthHandler(OAuthConfig{
				ClientID:    "test-client",
				RedirectURI: "http://localhost/callback",
			})
			handler.SetBaseURL(baseURL)
			handler.HandleUnauthorizedResponse(tc.response)
			assert.Equal(t, tc.want, handler.ProtectedResourceMetadataURL())
		})
	}
}

// TestOAuthHandler_HandleUnauthorizedResponse_NoBaseURL verifies that origin
// validation refuses to accept an advertised PRM URL when no base URL has
// been configured — otherwise there is nothing to validate against.
func TestOAuthHandler_HandleUnauthorizedResponse_NoBaseURL(t *testing.T) {
	handler := NewOAuthHandler(OAuthConfig{
		ClientID:    "test-client",
		RedirectURI: "http://localhost/callback",
	})
	handler.HandleUnauthorizedResponse(&http.Response{
		Header: http.Header{
			"Www-Authenticate": []string{`Bearer resource_metadata="https://example.com/prm"`},
		},
	})
	assert.Equal(t, "", handler.ProtectedResourceMetadataURL())
}

func TestResourceIdentifiersEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"identical", "https://example.com/mcp", "https://example.com/mcp", true},
		{"scheme case-insensitive", "HTTPS://example.com/mcp", "https://example.com/mcp", true},
		{"host case-insensitive", "https://Example.COM/mcp", "https://example.com/mcp", true},
		{"trailing slash ignored", "https://example.com/mcp", "https://example.com/mcp/", true},
		{"different host", "https://example.com/mcp", "https://other.example/mcp", false},
		{"different scheme", "http://example.com/mcp", "https://example.com/mcp", false},
		{"different path", "https://example.com/mcp", "https://example.com/other", false},
		{"different query", "https://example.com/mcp?a=1", "https://example.com/mcp?a=2", false},
		{"userinfo difference", "https://u@example.com/mcp", "https://example.com/mcp", false},
		{"unparseable falls back to string equality", "://bad", "://bad", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, resourceIdentifiersEqual(tc.a, tc.b))
		})
	}
}

// TestOAuthHandler_GetServerMetadata_RejectsMismatchedResourceFromAdvertisedPRM
// verifies RFC 9728 §3.3 / §7.3: when metadata is fetched from a PRM URL the
// server advertised via WWW-Authenticate and the response declares a
// `resource` identifier that does not match the protected resource the
// client addressed, the metadata MUST NOT be used. Without this check, a
// compromised resource could impersonate another and redirect OAuth
// discovery to an attacker's authorization server.
func TestOAuthHandler_GetServerMetadata_RejectsMismatchedResourceFromAdvertisedPRM(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/advertised-prm" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(OAuthProtectedResource{
				Resource:             "https://impersonated.example/mcp",
				AuthorizationServers: []string{"https://attacker.example/oauth"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	handler := NewOAuthHandler(OAuthConfig{
		ClientID:    "test-client",
		RedirectURI: "http://localhost/callback",
		TokenStore:  NewMemoryTokenStore(),
	})
	handler.SetBaseURL(server.URL)
	handler.SetProtectedResourceMetadataURL(server.URL + "/advertised-prm")

	_, err := handler.GetServerMetadata(context.Background())
	require.Error(t, err, "mismatched resource identifier must be rejected")
	assert.Contains(t, err.Error(), "does not match base URL")
}

// TestOAuthHandler_GetServerMetadata_RejectsEmptyResourceFromAdvertisedPRM
// verifies RFC 9728 compliance: because an advertised PRM URL may not
// share an origin with the protected resource, the response MUST declare
// its `resource` identifier explicitly. Omitting it leaves no way to bind
// the metadata to the protected resource the client addressed, so the
// response is rejected.
func TestOAuthHandler_GetServerMetadata_RejectsEmptyResourceFromAdvertisedPRM(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/advertised-prm" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(OAuthProtectedResource{
				// Resource deliberately omitted
				AuthorizationServers: []string{"https://attacker.example/oauth"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	handler := NewOAuthHandler(OAuthConfig{
		ClientID:    "test-client",
		RedirectURI: "http://localhost/callback",
		TokenStore:  NewMemoryTokenStore(),
	})
	handler.SetBaseURL(server.URL)
	handler.SetProtectedResourceMetadataURL(server.URL + "/advertised-prm")

	_, err := handler.GetServerMetadata(context.Background())
	require.Error(t, err, "advertised PRM response without a resource field must be rejected")
	assert.Contains(t, err.Error(), "omits required resource field")
}

// TestOAuthHandler_GetServerMetadata_AcceptsMatchingResourceFromAdvertisedPRM
// verifies that an advertised PRM response whose `resource` identifier
// matches the base URL is accepted and drives downstream discovery normally.
func TestOAuthHandler_GetServerMetadata_AcceptsMatchingResourceFromAdvertisedPRM(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/advertised-prm":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(OAuthProtectedResource{
				Resource:             server.URL + "/",
				AuthorizationServers: []string{server.URL},
			})
		case "/.well-known/oauth-authorization-server":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(AuthServerMetadata{
				Issuer:                server.URL,
				AuthorizationEndpoint: server.URL + "/authorize",
				TokenEndpoint:         server.URL + "/token",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	handler := NewOAuthHandler(OAuthConfig{
		ClientID:    "test-client",
		RedirectURI: "http://localhost/callback",
		TokenStore:  NewMemoryTokenStore(),
	})
	handler.SetBaseURL(server.URL)
	handler.SetProtectedResourceMetadataURL(server.URL + "/advertised-prm")

	metadata, err := handler.GetServerMetadata(context.Background())
	require.NoError(t, err)
	assert.Equal(t, server.URL+"/token", metadata.TokenEndpoint)
}

// TestOAuthHandler_GetServerMetadata_UsesAdvertisedPRMURL verifies that when
// the server has advertised a Protected Resource Metadata URL via
// WWW-Authenticate (RFC 9728 §5.1), discovery fetches that URL in preference
// to the origin-based /.well-known/oauth-protected-resource construction.
func TestOAuthHandler_GetServerMetadata_UsesAdvertisedPRMURL(t *testing.T) {
	advertisedPRMRequested := false
	wellKnownPRMRequested := false
	authServerRequested := false

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/custom/prm-path":
			advertisedPRMRequested = true
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(OAuthProtectedResource{
				Resource:             server.URL,
				AuthorizationServers: []string{server.URL},
			})
		case "/.well-known/oauth-protected-resource":
			wellKnownPRMRequested = true
			w.WriteHeader(http.StatusNotFound)
		case "/.well-known/oauth-authorization-server":
			authServerRequested = true
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(AuthServerMetadata{
				Issuer:                server.URL,
				AuthorizationEndpoint: server.URL + "/authorize",
				TokenEndpoint:         server.URL + "/token",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	handler := NewOAuthHandler(OAuthConfig{
		ClientID:    "test-client",
		RedirectURI: "http://localhost/callback",
		TokenStore:  NewMemoryTokenStore(),
	})
	handler.SetBaseURL(server.URL)
	handler.SetProtectedResourceMetadataURL(server.URL + "/custom/prm-path")

	metadata, err := handler.GetServerMetadata(context.Background())
	require.NoError(t, err)
	assert.True(t, advertisedPRMRequested, "advertised PRM URL should be fetched")
	assert.False(t, wellKnownPRMRequested, "well-known PRM URL should be skipped when an advertised one is set")
	assert.True(t, authServerRequested, "authorization-server metadata should still be fetched")
	assert.Equal(t, server.URL+"/token", metadata.TokenEndpoint)
}
