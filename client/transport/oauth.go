package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
)

// ErrNoToken is returned when no token is available in the token store
var ErrNoToken = errors.New("no token available")

// OAuthConfig holds the OAuth configuration for the client
type OAuthConfig struct {
	// ClientID is the OAuth client ID
	ClientID string
	// ClientSecret is the OAuth client secret (for confidential clients)
	ClientSecret string
	// RedirectURI is the redirect URI for the OAuth flow
	RedirectURI string
	// Scopes is the list of OAuth scopes to request
	Scopes []string
	// TokenStore is the storage for OAuth tokens
	TokenStore TokenStore
	// AuthServerMetadataURL is the URL to the OAuth server metadata
	// If empty, the client will attempt to discover it from the base URL
	AuthServerMetadataURL string
	// PKCEEnabled enables PKCE for the OAuth flow (recommended for public clients)
	PKCEEnabled bool
	// HTTPClient is an optional HTTP client to use for requests.
	// If nil, a default HTTP client with a 30 second timeout will be used.
	HTTPClient *http.Client
}

// TokenStore is an interface for storing and retrieving OAuth tokens.
//
// Implementations must:
//   - Honor context cancellation and deadlines, returning context.Canceled
//     or context.DeadlineExceeded as appropriate
//   - Return ErrNoToken (or a sentinel error that wraps it) when no token
//     is available, rather than conflating this with other operational errors
//   - Properly propagate all other errors (database failures, I/O errors, etc.)
//   - Check ctx.Done() before performing operations and return ctx.Err() if cancelled
type TokenStore interface {
	// GetToken returns the current token.
	// Returns ErrNoToken if no token is available.
	// Returns context.Canceled or context.DeadlineExceeded if ctx is cancelled.
	// Returns other errors for operational failures (I/O, database, etc.).
	GetToken(ctx context.Context) (*Token, error)

	// SaveToken saves a token.
	// Returns context.Canceled or context.DeadlineExceeded if ctx is cancelled.
	// Returns other errors for operational failures (I/O, database, etc.).
	SaveToken(ctx context.Context, token *Token) error
}

// Token represents an OAuth token
type Token struct {
	// AccessToken is the OAuth access token
	AccessToken string `json:"access_token"`
	// TokenType is the type of token (usually "Bearer")
	TokenType string `json:"token_type"`
	// RefreshToken is the OAuth refresh token
	RefreshToken string `json:"refresh_token,omitempty"`
	// ExpiresIn is the number of seconds until the token expires
	ExpiresIn int64 `json:"expires_in,omitempty"`
	// Scope is the scope of the token
	Scope string `json:"scope,omitempty"`
	// ExpiresAt is the time when the token expires
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

// IsExpired returns true if the token is expired
func (t *Token) IsExpired() bool {
	if t.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(t.ExpiresAt)
}

// MemoryTokenStore is a simple in-memory token store
type MemoryTokenStore struct {
	token *Token
	mu    sync.RWMutex
}

// NewMemoryTokenStore creates a new in-memory token store
func NewMemoryTokenStore() *MemoryTokenStore {
	return &MemoryTokenStore{}
}

// GetToken returns the current token.
// Returns ErrNoToken if no token is available.
// Returns context.Canceled or context.DeadlineExceeded if ctx is cancelled.
func (s *MemoryTokenStore) GetToken(ctx context.Context) (*Token, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.token == nil {
		return nil, ErrNoToken
	}
	return s.token, nil
}

// SaveToken saves a token.
// Returns context.Canceled or context.DeadlineExceeded if ctx is cancelled.
func (s *MemoryTokenStore) SaveToken(ctx context.Context, token *Token) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = token
	return nil
}

// AuthServerMetadata represents the OAuth 2.0 Authorization Server Metadata
type AuthServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint,omitempty"`
	JwksURI                           string   `json:"jwks_uri,omitempty"`
	ScopesSupported                   []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported,omitempty"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported,omitempty"`
}

// OAuthHandler handles OAuth authentication for HTTP requests
type OAuthHandler struct {
	config           OAuthConfig
	httpClient       *http.Client
	serverMetadata   *AuthServerMetadata
	metadataFetchErr error
	metadataFetched  bool
	metadataMu       sync.Mutex // Protects metadata discovery state
	baseURL          string

	resourceMetadataURL string // URL from WWW-Authenticate resource_metadata param

	mu            sync.RWMutex // Protects expectedState
	expectedState string       // Expected state value for CSRF protection
}

// NewOAuthHandler creates a new OAuth handler
func NewOAuthHandler(config OAuthConfig) *OAuthHandler {
	if config.TokenStore == nil {
		config.TokenStore = NewMemoryTokenStore()
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}

	return &OAuthHandler{
		config:     config,
		httpClient: config.HTTPClient,
	}
}

// GetAuthorizationHeader returns the Authorization header value for a request
func (h *OAuthHandler) GetAuthorizationHeader(ctx context.Context) (string, error) {
	token, err := h.getValidToken(ctx)
	if err != nil {
		return "", err
	}

	// Some auth implementations are strict about token type
	tokenType := token.TokenType
	if tokenType == "bearer" {
		tokenType = "Bearer"
	}

	return fmt.Sprintf("%s %s", tokenType, token.AccessToken), nil
}

// getValidToken returns a valid token, refreshing if necessary
func (h *OAuthHandler) getValidToken(ctx context.Context) (*Token, error) {
	token, err := h.config.TokenStore.GetToken(ctx)
	if err != nil && !errors.Is(err, ErrNoToken) {
		return nil, err
	}
	if err == nil && !token.IsExpired() && token.AccessToken != "" {
		return token, nil
	}

	// If we have a refresh token, try to use it
	if err == nil && token.RefreshToken != "" {
		newToken, err := h.refreshToken(ctx, token.RefreshToken)
		if err == nil {
			return newToken, nil
		}
		// If refresh fails, continue to authorization flow
	}

	// We need to get a new token through the authorization flow
	return nil, ErrOAuthAuthorizationRequired
}

// refreshToken refreshes an OAuth token
func (h *OAuthHandler) refreshToken(ctx context.Context, refreshToken string) (*Token, error) {
	metadata, err := h.getServerMetadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get server metadata: %w", err)
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", h.config.ClientID)
	if h.config.ClientSecret != "" {
		data.Set("client_secret", h.config.ClientSecret)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		metadata.TokenEndpoint,
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send refresh token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, extractOAuthError(body, resp.StatusCode, "refresh token request failed")
	}

	// Read the response body for parsing
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token response body: %w", err)
	}

	// GitHub returns HTTP 200 even for errors, with error details in the JSON body
	// Check if the response contains an error field before parsing as Token
	var oauthErr OAuthError
	if err := json.Unmarshal(body, &oauthErr); err == nil && oauthErr.ErrorCode != "" {
		return nil, fmt.Errorf("refresh token request failed: %w", oauthErr)
	}

	var tokenResp Token
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	// Set expiration time
	if tokenResp.ExpiresIn > 0 {
		tokenResp.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	// If no new refresh token is provided, keep the old one
	if tokenResp.RefreshToken == "" {
		tokenResp.RefreshToken = refreshToken
	}

	// Save the token
	if err := h.config.TokenStore.SaveToken(ctx, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to save token: %w", err)
	}

	return &tokenResp, nil
}

// RefreshToken is a public wrapper for refreshToken
func (h *OAuthHandler) RefreshToken(ctx context.Context, refreshToken string) (*Token, error) {
	return h.refreshToken(ctx, refreshToken)
}

// GetClientID returns the client ID
func (h *OAuthHandler) GetClientID() string {
	return h.config.ClientID
}

// extractOAuthError attempts to parse an OAuth error response from the response body
func extractOAuthError(body []byte, statusCode int, context string) error {
	// Try to parse the error as an OAuth error response
	var oauthErr OAuthError
	if err := json.Unmarshal(body, &oauthErr); err == nil && oauthErr.ErrorCode != "" {
		return fmt.Errorf("%s: %w", context, oauthErr)
	}

	// If not a valid OAuth error, return the raw response
	return fmt.Errorf("%s with status %d: %s", context, statusCode, body)
}

// GetClientSecret returns the client secret
func (h *OAuthHandler) GetClientSecret() string {
	return h.config.ClientSecret
}

// SetBaseURL sets the base URL for the API server
func (h *OAuthHandler) SetBaseURL(baseURL string) {
	h.baseURL = baseURL
}

// GetExpectedState returns the expected state value (for testing purposes)
func (h *OAuthHandler) GetExpectedState() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.expectedState
}

// SetExpectedState sets the expected state value.
//
// This can be useful if you cannot maintain an OAuthHandler
// instance throughout the authentication flow; for example, if
// the initialization and callback steps are handled in different
// requests.
//
// In such cases, this should be called with the state value generated
// during the initial authentication request (e.g. by GenerateState)
// and included in the authorization URL.
func (h *OAuthHandler) SetExpectedState(expectedState string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.expectedState = expectedState
}

// OAuthError represents a standard OAuth 2.0 error response
type OAuthError struct {
	ErrorCode        string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
	ErrorURI         string `json:"error_uri,omitempty"`
}

// Error implements the error interface
func (e OAuthError) Error() string {
	if e.ErrorDescription != "" {
		return fmt.Sprintf("OAuth error: %s - %s", e.ErrorCode, e.ErrorDescription)
	}
	return fmt.Sprintf("OAuth error: %s", e.ErrorCode)
}

// OAuthProtectedResource represents the response from /.well-known/oauth-protected-resource
type OAuthProtectedResource struct {
	AuthorizationServers []string `json:"authorization_servers"`
	Resource             string   `json:"resource"`
	ResourceName         string   `json:"resource_name,omitempty"`
}

// SetResourceMetadataURL sets the resource metadata URL extracted from a
// WWW-Authenticate header. This allows re-discovery of server metadata
// using the hint provided by the server in its 401 response.
// If metadata has already been fetched, setting this URL will trigger
// re-discovery on the next call to getServerMetadata.
func (h *OAuthHandler) SetResourceMetadataURL(metadataURL string) {
	h.metadataMu.Lock()
	defer h.metadataMu.Unlock()
	if metadataURL != "" && metadataURL != h.resourceMetadataURL {
		h.resourceMetadataURL = metadataURL
		// Reset metadata to allow re-discovery with the new hint
		h.serverMetadata = nil
		h.metadataFetchErr = nil
		h.metadataFetched = false
	}
}

// ParseResourceMetadataURL extracts the resource_metadata URL from a raw
// WWW-Authenticate header value per RFC 9728.
// Uses a full RFC 9110 §11.6.1 challenge parser.
// Returns empty string if not present or on parse error.
//
// Parser adapted from github.com/modelcontextprotocol/go-sdk (MIT/Apache-2.0)
// https://github.com/modelcontextprotocol/go-sdk/blob/main/oauthex/resource_meta.go
func ParseResourceMetadataURL(wwwAuthenticate string) string {
	challenges, err := parseWWWAuthenticate([]string{wwwAuthenticate})
	if err != nil {
		return ""
	}
	for _, c := range challenges {
		if u := c.params["resource_metadata"]; u != "" {
			return u
		}
	}
	return ""
}

// wwwAuthChallenge represents a single authentication challenge from a WWW-Authenticate header.
// Adapted from github.com/modelcontextprotocol/go-sdk (MIT/Apache-2.0)
// https://github.com/modelcontextprotocol/go-sdk/blob/main/oauthex/resource_meta_public.go
type wwwAuthChallenge struct {
	scheme string
	params map[string]string
}

// parseWWWAuthenticate parses WWW-Authenticate header values per RFC 9110 §11.6.1.
// Adapted from github.com/modelcontextprotocol/go-sdk (MIT/Apache-2.0)
// https://github.com/modelcontextprotocol/go-sdk/blob/main/oauthex/resource_meta.go
func parseWWWAuthenticate(headers []string) ([]wwwAuthChallenge, error) {
	var challenges []wwwAuthChallenge
	for _, h := range headers {
		chunks, err := splitWWWAuthChallenges(h)
		if err != nil {
			return nil, err
		}
		for _, cs := range chunks {
			if strings.TrimSpace(cs) == "" {
				continue
			}
			c, err := parseSingleWWWAuthChallenge(cs)
			if err != nil {
				return nil, fmt.Errorf("failed to parse challenge %q: %w", cs, err)
			}
			challenges = append(challenges, c)
		}
	}
	return challenges, nil
}

// splitWWWAuthChallenges splits a header value containing one or more challenges.
// It correctly handles commas within quoted strings and distinguishes between
// commas separating auth-params and commas separating challenges.
func splitWWWAuthChallenges(header string) ([]string, error) {
	var challenges []string
	inQuotes := false
	start := 0
	for i, r := range header {
		if r == '"' {
			if i > 0 && header[i-1] != '\\' {
				inQuotes = !inQuotes
			} else if i == 0 {
				return nil, errors.New(`challenge begins with '"'`)
			}
		} else if r == ',' && !inQuotes {
			lookahead := strings.TrimSpace(header[i+1:])
			eqPos := strings.Index(lookahead, "=")

			isParam := false
			if eqPos > 0 {
				token := lookahead[:eqPos]
				if strings.IndexFunc(token, unicode.IsSpace) == -1 {
					isParam = true
				}
			}

			if !isParam {
				challenges = append(challenges, header[start:i])
				start = i + 1
			}
		}
	}
	challenges = append(challenges, header[start:])
	return challenges, nil
}

// parseSingleWWWAuthChallenge parses a string containing exactly one challenge.
func parseSingleWWWAuthChallenge(s string) (wwwAuthChallenge, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return wwwAuthChallenge{}, errors.New("empty challenge string")
	}

	scheme, paramsStr, found := strings.Cut(s, " ")
	c := wwwAuthChallenge{scheme: strings.ToLower(scheme)}
	if !found {
		return c, nil
	}

	params := make(map[string]string)
	for paramsStr != "" {
		keyEnd := strings.Index(paramsStr, "=")
		if keyEnd <= 0 {
			return wwwAuthChallenge{}, fmt.Errorf("malformed auth parameter: expected key=value, but got %q", paramsStr)
		}
		key := strings.TrimSpace(paramsStr[:keyEnd])
		paramsStr = strings.TrimSpace(paramsStr[keyEnd+1:])

		var value string
		if strings.HasPrefix(paramsStr, "\"") {
			paramsStr = paramsStr[1:]
			var valBuilder strings.Builder
			i := 0
			for ; i < len(paramsStr); i++ {
				if paramsStr[i] == '\\' && i+1 < len(paramsStr) {
					valBuilder.WriteByte(paramsStr[i+1])
					i++
				} else if paramsStr[i] == '"' {
					break
				} else {
					valBuilder.WriteByte(paramsStr[i])
				}
			}
			if i == len(paramsStr) {
				return wwwAuthChallenge{}, fmt.Errorf("unterminated quoted string in auth parameter")
			}
			value = valBuilder.String()
			paramsStr = strings.TrimSpace(paramsStr[i+1:])
		} else {
			commaPos := strings.Index(paramsStr, ",")
			if commaPos == -1 {
				value = paramsStr
				paramsStr = ""
			} else {
				value = strings.TrimSpace(paramsStr[:commaPos])
				paramsStr = strings.TrimSpace(paramsStr[commaPos:])
			}
		}
		if value == "" {
			return wwwAuthChallenge{}, fmt.Errorf("no value for auth param %q", key)
		}

		params[strings.ToLower(key)] = value

		if strings.HasPrefix(paramsStr, ",") {
			paramsStr = strings.TrimSpace(paramsStr[1:])
		} else if paramsStr != "" {
			return wwwAuthChallenge{}, fmt.Errorf("malformed auth parameter: expected comma after value, but got %q", paramsStr)
		}
	}

	c.params = params
	return c, nil
}

// getServerMetadata fetches the OAuth server metadata
func (h *OAuthHandler) getServerMetadata(ctx context.Context) (*AuthServerMetadata, error) {
	h.metadataMu.Lock()
	defer h.metadataMu.Unlock()

	if h.metadataFetched && h.serverMetadata != nil {
		return h.serverMetadata, nil
	}
	if h.metadataFetched && h.metadataFetchErr != nil {
		return nil, h.metadataFetchErr
	}

	h.metadataFetched = true

	// If AuthServerMetadataURL is explicitly provided, use it directly
	if h.config.AuthServerMetadataURL != "" {
		h.fetchMetadataFromURL(ctx, h.config.AuthServerMetadataURL)
		if h.metadataFetchErr != nil {
			return nil, h.metadataFetchErr
		}
		return h.serverMetadata, nil
	}

	// If we have a resource_metadata URL from WWW-Authenticate, try it first
	if h.resourceMetadataURL != "" {
		protectedResource := h.fetchProtectedResourceFromURL(ctx, h.resourceMetadataURL)
		if protectedResource != nil && len(protectedResource.AuthorizationServers) > 0 {
			if h.discoverAuthServerMetadata(ctx, protectedResource.AuthorizationServers[0]) {
				return h.serverMetadata, nil
			}
		}
	}

	// Try to discover the authorization server via OAuth Protected Resource
	// as per RFC 9728 (https://datatracker.ietf.org/doc/html/rfc9728)
	baseURL, err := h.extractBaseURL()
	if err != nil {
		h.metadataFetchErr = fmt.Errorf("failed to extract base URL: %w", err)
		return nil, h.metadataFetchErr
	}

	// Try to fetch the OAuth Protected Resource metadata
	protectedResourceURL := baseURL + "/.well-known/oauth-protected-resource"
	protectedResource := h.fetchProtectedResourceFromURL(ctx, protectedResourceURL)

	// If we can't get the protected resource metadata, try OAuth Authorization Server discovery
	if protectedResource == nil {
		h.fetchMetadataFromURL(ctx, baseURL+"/.well-known/oauth-authorization-server")
		if h.serverMetadata != nil {
			h.metadataFetchErr = nil
			return h.serverMetadata, nil
		}
		// If that also fails, fall back to default endpoints
		metadata, err := h.getDefaultEndpoints(baseURL)
		if err != nil {
			h.metadataFetchErr = fmt.Errorf("failed to get default endpoints: %w", err)
			return nil, h.metadataFetchErr
		}
		h.serverMetadata = metadata
		h.metadataFetchErr = nil
		return h.serverMetadata, nil
	}

	// If no authorization servers are specified, fall back to default endpoints
	if len(protectedResource.AuthorizationServers) == 0 {
		metadata, err := h.getDefaultEndpoints(baseURL)
		if err != nil {
			h.metadataFetchErr = fmt.Errorf("failed to get default endpoints: %w", err)
			return nil, h.metadataFetchErr
		}
		h.serverMetadata = metadata
		h.metadataFetchErr = nil
		return h.serverMetadata, nil
	}

	// Use the first authorization server
	authServerURL := protectedResource.AuthorizationServers[0]
	if h.discoverAuthServerMetadata(ctx, authServerURL) {
		return h.serverMetadata, nil
	}

	// If both discovery methods fail, use default endpoints based on the authorization server URL
	metadata, err := h.getDefaultEndpoints(authServerURL)
	if err != nil {
		h.metadataFetchErr = fmt.Errorf("failed to get default endpoints: %w", err)
		return nil, h.metadataFetchErr
	}
	h.serverMetadata = metadata
	h.metadataFetchErr = nil
	return h.serverMetadata, nil
}

// fetchProtectedResourceFromURL fetches OAuth Protected Resource metadata from a URL.
// Returns nil if the request fails or returns non-200.
func (h *OAuthHandler) fetchProtectedResourceFromURL(ctx context.Context, metadataURL string) *OAuthProtectedResource {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return nil
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("MCP-Protocol-Version", "2025-03-26")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var pr OAuthProtectedResource
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil
	}
	return &pr
}

// discoverAuthServerMetadata tries OAuth Authorization Server Metadata and
// OpenID Connect discovery for the given authorization server URL.
// Returns true if metadata was successfully discovered.
func (h *OAuthHandler) discoverAuthServerMetadata(ctx context.Context, authServerURL string) bool {
	// Try OAuth Authorization Server Metadata first
	h.fetchMetadataFromURL(ctx, authServerURL+"/.well-known/oauth-authorization-server")
	if h.serverMetadata != nil {
		h.metadataFetchErr = nil
		return true
	}

	// If OAuth Authorization Server Metadata discovery fails, try OpenID Connect discovery
	h.fetchMetadataFromURL(ctx, authServerURL+"/.well-known/openid-configuration")
	if h.serverMetadata != nil {
		h.metadataFetchErr = nil
		return true
	}

	return false
}

// fetchMetadataFromURL fetches and parses OAuth server metadata from a URL
func (h *OAuthHandler) fetchMetadataFromURL(ctx context.Context, metadataURL string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		h.metadataFetchErr = fmt.Errorf("failed to create metadata request: %w", err)
		return
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("MCP-Protocol-Version", "2025-03-26")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.metadataFetchErr = fmt.Errorf("failed to send metadata request: %w", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// If metadata discovery fails, don't set any metadata
		return
	}

	var metadata AuthServerMetadata
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		h.metadataFetchErr = fmt.Errorf("failed to decode metadata response: %w", err)
		return
	}

	h.serverMetadata = &metadata
}

// extractBaseURL extracts the base URL from the first request
func (h *OAuthHandler) extractBaseURL() (string, error) {
	// If we have a base URL from a previous request, use it
	if h.baseURL != "" {
		return h.baseURL, nil
	}

	// Otherwise, we need to infer it from the redirect URI
	if h.config.RedirectURI == "" {
		return "", fmt.Errorf("no base URL available and no redirect URI provided")
	}

	// Parse the redirect URI to extract the authority
	parsedURL, err := url.Parse(h.config.RedirectURI)
	if err != nil {
		return "", fmt.Errorf("failed to parse redirect URI: %w", err)
	}

	// Use the scheme and host from the redirect URI
	baseURL := fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)
	return baseURL, nil
}

// GetServerMetadata is a public wrapper for getServerMetadata
func (h *OAuthHandler) GetServerMetadata(ctx context.Context) (*AuthServerMetadata, error) {
	return h.getServerMetadata(ctx)
}

// getDefaultEndpoints returns default OAuth endpoints based on the base URL
func (h *OAuthHandler) getDefaultEndpoints(baseURL string) (*AuthServerMetadata, error) {
	// Parse the base URL to extract the authority
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base URL: %w", err)
	}

	// Discard any path component to get the authorization base URL
	parsedURL.Path = ""
	authBaseURL := parsedURL.String()

	// Validate that the URL has a scheme and host
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, fmt.Errorf("invalid base URL: missing scheme or host in %q", baseURL)
	}

	return &AuthServerMetadata{
		Issuer:                authBaseURL,
		AuthorizationEndpoint: authBaseURL + "/authorize",
		TokenEndpoint:         authBaseURL + "/token",
		RegistrationEndpoint:  authBaseURL + "/register",
	}, nil
}

// RegisterClient performs dynamic client registration
func (h *OAuthHandler) RegisterClient(ctx context.Context, clientName string) error {
	metadata, err := h.getServerMetadata(ctx)
	if err != nil {
		return fmt.Errorf("failed to get server metadata: %w", err)
	}

	if metadata.RegistrationEndpoint == "" {
		return errors.New("server does not support dynamic client registration")
	}

	// Prepare registration request
	regRequest := map[string]any{
		"client_name":                clientName,
		"redirect_uris":              []string{h.config.RedirectURI},
		"token_endpoint_auth_method": "none", // For public clients
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"scope":                      strings.Join(h.config.Scopes, " "),
	}

	// Add client_secret if this is a confidential client
	if h.config.ClientSecret != "" {
		regRequest["token_endpoint_auth_method"] = "client_secret_basic"
	}

	reqBody, err := json.Marshal(regRequest)
	if err != nil {
		return fmt.Errorf("failed to marshal registration request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		metadata.RegistrationEndpoint,
		bytes.NewReader(reqBody),
	)
	if err != nil {
		return fmt.Errorf("failed to create registration request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send registration request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return extractOAuthError(body, resp.StatusCode, "registration request failed")
	}

	var regResponse struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&regResponse); err != nil {
		return fmt.Errorf("failed to decode registration response: %w", err)
	}

	// Update the client configuration
	h.config.ClientID = regResponse.ClientID
	if regResponse.ClientSecret != "" {
		h.config.ClientSecret = regResponse.ClientSecret
	}

	return nil
}

// ErrInvalidState is returned when the state parameter doesn't match the expected value
var ErrInvalidState = errors.New("invalid state parameter, possible CSRF attack")

// ProcessAuthorizationResponse processes the authorization response and exchanges the code for a token
func (h *OAuthHandler) ProcessAuthorizationResponse(ctx context.Context, code, state, codeVerifier string) error {
	// Validate the state parameter to prevent CSRF attacks
	h.mu.Lock()
	expectedState := h.expectedState
	if expectedState == "" {
		h.mu.Unlock()
		return errors.New("no expected state found, authorization flow may not have been initiated properly")
	}

	if state != expectedState {
		h.mu.Unlock()
		return ErrInvalidState
	}

	// Clear the expected state after validation
	h.expectedState = ""
	h.mu.Unlock()

	metadata, err := h.getServerMetadata(ctx)
	if err != nil {
		return fmt.Errorf("failed to get server metadata: %w", err)
	}

	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("client_id", h.config.ClientID)
	data.Set("redirect_uri", h.config.RedirectURI)

	if h.config.ClientSecret != "" {
		data.Set("client_secret", h.config.ClientSecret)
	}

	if h.config.PKCEEnabled && codeVerifier != "" {
		data.Set("code_verifier", codeVerifier)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		metadata.TokenEndpoint,
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return extractOAuthError(body, resp.StatusCode, "token request failed")
	}

	// Read the response body for parsing
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read token response body: %w", err)
	}

	// GitHub returns HTTP 200 even for errors, with error details in the JSON body
	// Check if the response contains an error field before parsing as Token
	var oauthErr OAuthError
	if err := json.Unmarshal(body, &oauthErr); err == nil && oauthErr.ErrorCode != "" {
		return fmt.Errorf("token request failed: %w", oauthErr)
	}

	var tokenResp Token
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("failed to decode token response: %w", err)
	}

	// Set expiration time
	if tokenResp.ExpiresIn > 0 {
		tokenResp.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	// Save the token
	if err := h.config.TokenStore.SaveToken(ctx, &tokenResp); err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}

	return nil
}

// GetAuthorizationURL returns the URL for the authorization endpoint
func (h *OAuthHandler) GetAuthorizationURL(ctx context.Context, state, codeChallenge string) (string, error) {
	metadata, err := h.getServerMetadata(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get server metadata: %w", err)
	}

	// Store the state for later validation
	h.SetExpectedState(state)

	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", h.config.ClientID)
	params.Set("redirect_uri", h.config.RedirectURI)
	params.Set("state", state)

	if len(h.config.Scopes) > 0 {
		params.Set("scope", strings.Join(h.config.Scopes, " "))
	}

	if h.config.PKCEEnabled && codeChallenge != "" {
		params.Set("code_challenge", codeChallenge)
		params.Set("code_challenge_method", "S256")
	}

	return metadata.AuthorizationEndpoint + "?" + params.Encode(), nil
}
