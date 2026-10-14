package transport

import (
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
	ctx := t.Context()
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
	_, err = transport.SendRequest(t.Context(), JSONRPCRequest{
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
	response, err := transport.SendRequest(t.Context(), JSONRPCRequest{
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
	_, err = transport.SendRequest(t.Context(), JSONRPCRequest{
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

func TestStreamableHTTP_OAuthMetadataDiscovery(t *testing.T) {
	// Test that we correctly extract resource_metadata URL from WWW-Authenticate header per RFC9728
	const expectedMetadataURL = "https://auth.example.com/.well-known/oauth-protected-resource"

	// Create a test server that returns 401 with WWW-Authenticate header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 401 with WWW-Authenticate header containing resource_metadata
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+expectedMetadataURL+`"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	// Create a token store with a valid token so the request reaches the server
	// The server will still return 401 to simulate token rejection
	tokenStore := NewMemoryTokenStore()
	validToken := &Token{
		AccessToken:  "test-token",
		TokenType:    "Bearer",
		RefreshToken: "refresh-token",
		ExpiresIn:    3600,
		ExpiresAt:    time.Now().Add(1 * time.Hour), // Valid for 1 hour
	}
	if err := tokenStore.SaveToken(t.Context(), validToken); err != nil {
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

	// Send a request that will trigger 401
	_, err = transport.SendRequest(t.Context(), JSONRPCRequest{
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

	// Verify the discovered metadata URL was extracted from WWW-Authenticate header
	if oauthErr.ResourceMetadataURL != expectedMetadataURL {
		t.Errorf("Expected ResourceMetadataURL to be %q, got %q",
			expectedMetadataURL, oauthErr.ResourceMetadataURL)
	}
}

func TestParseAuthParams(t *testing.T) {
	testCases := []struct {
		name     string
		header   string
		expected map[string]string
	}{
		{
			name:   "Basic key=value",
			header: `Bearer resource_metadata="https://example.com"`,
			expected: map[string]string{
				"resource_metadata": "https://example.com",
			},
		},
		{
			name:   "Multiple params",
			header: `Bearer realm="example", resource_metadata="https://example.com/metadata", scope="read write"`,
			expected: map[string]string{
				"realm":             "example",
				"resource_metadata": "https://example.com/metadata",
				"scope":             "read write",
			},
		},
		{
			name:   "Unquoted token values",
			header: `Bearer realm=example, error=invalid_token`,
			expected: map[string]string{
				"realm": "example",
				"error": "invalid_token",
			},
		},
		{
			name:   "Escaped quotes in values",
			header: `Bearer realm="say \"hello\""`,
			expected: map[string]string{
				"realm": `say "hello"`,
			},
		},
		{
			name:     "Missing auth-scheme",
			header:   "",
			expected: map[string]string{},
		},
		{
			name:     "Auth-scheme only",
			header:   "Bearer",
			expected: map[string]string{},
		},
		{
			name:   "Extra whitespace",
			header: `Bearer   realm="example"  ,  scope="read"`,
			expected: map[string]string{
				"realm": "example",
				"scope": "read",
			},
		},
		{
			name:   "DPoP scheme",
			header: `DPoP resource_metadata="https://dpop.example.com/.well-known/oauth-protected-resource"`,
			expected: map[string]string{
				"resource_metadata": "https://dpop.example.com/.well-known/oauth-protected-resource",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := parseAuthParams(tc.header)
			if len(result) != len(tc.expected) {
				t.Errorf("Expected %d params, got %d: %v", len(tc.expected), len(result), result)
				return
			}
			for k, v := range tc.expected {
				if got, ok := result[k]; !ok {
					t.Errorf("Missing key %q", k)
				} else if got != v {
					t.Errorf("Key %q: expected %q, got %q", k, v, got)
				}
			}
		})
	}
}

func TestExtractResourceMetadataURL(t *testing.T) {
	testCases := []struct {
		name        string
		wwwAuth     string
		expectedURL string
	}{
		{
			name:        "Valid Bearer with resource_metadata",
			wwwAuth:     `Bearer resource_metadata="https://auth.example.com/.well-known/oauth-protected-resource"`,
			expectedURL: "https://auth.example.com/.well-known/oauth-protected-resource",
		},
		{
			name:        "Bearer with resource_metadata and other parameters",
			wwwAuth:     `Bearer realm="example", resource_metadata="https://example.com/metadata", scope="read write"`,
			expectedURL: "https://example.com/metadata",
		},
		{
			name:        "No resource_metadata parameter",
			wwwAuth:     `Bearer realm="example", scope="read"`,
			expectedURL: "",
		},
		{
			name:        "Empty header",
			wwwAuth:     "",
			expectedURL: "",
		},
		{
			// Truncated quoted-strings are rejected outright — extracting a
			// partial value from malformed input would let a hostile server
			// dictate where discovery lands.
			name:        "Malformed resource_metadata (no closing quote)",
			wwwAuth:     `Bearer resource_metadata="https://example.com/metadata`,
			expectedURL: "",
		},
		{
			name:        "DPoP scheme with resource_metadata",
			wwwAuth:     `DPoP resource_metadata="https://dpop.example.com/.well-known/oauth-protected-resource"`,
			expectedURL: "https://dpop.example.com/.well-known/oauth-protected-resource",
		},
		{
			name:        "Whitespace around equals",
			wwwAuth:     `Bearer resource_metadata = "https://example.com/meta"`,
			expectedURL: "https://example.com/meta",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := extractResourceMetadataURL([]string{tc.wwwAuth})
			if result != tc.expectedURL {
				t.Errorf("Expected %q, got %q", tc.expectedURL, result)
			}
		})
	}
}

func TestStreamableHTTP_OAuthMetadataFeedback(t *testing.T) {
	// Verify that after a 401 with resource_metadata, the OAuthHandler's
	// ProtectedResourceMetadataURL has been updated. Origin validation
	// requires the advertised URL to share scheme+host with the base URL,
	// so the advertised PRM URL is a path under the same test server.
	var server *httptest.Server
	var expectedMetadataURL string
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+expectedMetadataURL+`"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	expectedMetadataURL = server.URL + "/.well-known/oauth-protected-resource"

	tokenStore := NewMemoryTokenStore()
	_ = tokenStore.SaveToken(t.Context(), &Token{
		AccessToken: "test-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	})

	oauthConfig := OAuthConfig{
		ClientID:   "test-client",
		TokenStore: tokenStore,
	}

	transport, err := NewStreamableHTTP(server.URL, WithHTTPOAuth(oauthConfig))
	if err != nil {
		t.Fatalf("Failed to create StreamableHTTP: %v", err)
	}

	// Send request that triggers 401
	_, err = transport.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(1),
		Method:  "test",
	})

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Verify the OAuthHandler was updated with the discovered URL
	if got := transport.GetOAuthHandler().config.ProtectedResourceMetadataURL; got != expectedMetadataURL {
		t.Errorf("Expected OAuthHandler.config.ProtectedResourceMetadataURL to be %q, got %q", expectedMetadataURL, got)
	}
}
