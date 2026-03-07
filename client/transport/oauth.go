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
)

// ErrNoToken is returned when no token is available in the token store
var ErrNoToken = errors.New("no token available")

// OAuthConfig holds the OAuth configuration for the client
type OAuthConfig struct {
	// ClientID is the OAuth client ID
	ClientID string
	// ClientURI is the URI of the client
	ClientURI string
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
	// ProtectedResourceURL is the URL to the OAuth protected resource metadata
	// (RFC 9728). If empty, the client derives it from the base URL as
	// {baseURL}/.well-known/oauth-protected-resource. Transports populate this
	// automatically from the WWW-Authenticate resource_metadata parameter on a
	// 401 response; set it manually only if you need to override discovery.
	ProtectedResourceURL string
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
	metadataOnce     sync.Once
	baseURL          string

	mu                   sync.RWMutex // Protects the fields below
	expectedState        string       // Expected state value for CSRF protection
	protectedResourceURL string       // From WWW-Authenticate; overrides config.ProtectedResourceURL
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

// SetProtectedResourceURL records the RFC 9728 protected-resource metadata URL
// advertised by the resource server in a WWW-Authenticate challenge. It takes
// precedence over OAuthConfig.ProtectedResourceURL during discovery. Calling
// this after metadata has already been fetched has no effect.
func (h *OAuthHandler) SetProtectedResourceURL(u string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.protectedResourceURL = u
}

func (h *OAuthHandler) getProtectedResourceURL() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.protectedResourceURL != "" {
		return h.protectedResourceURL
	}
	return h.config.ProtectedResourceURL
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

// getServerMetadata fetches the OAuth server metadata
func (h *OAuthHandler) getServerMetadata(ctx context.Context) (*AuthServerMetadata, error) {
	h.metadataOnce.Do(func() {
		// If AuthServerMetadataURL is explicitly provided, use it directly
		if h.config.AuthServerMetadataURL != "" {
			h.fetchMetadataFromURL(ctx, h.config.AuthServerMetadataURL)
			return
		}

		// Try to discover the authorization server via OAuth Protected Resource
		// as per RFC 9728 (https://datatracker.ietf.org/doc/html/rfc9728)
		baseURL, err := h.extractBaseURL()
		if err != nil {
			h.metadataFetchErr = fmt.Errorf("failed to extract base URL: %w", err)
			return
		}

		// Try to fetch the OAuth Protected Resource metadata. Prefer the URL
		// supplied via WWW-Authenticate (RFC 9728 section 5.1) if one was set.
		protectedResourceURL := h.getProtectedResourceURL()
		if protectedResourceURL == "" {
			protectedResourceURL = baseURL + "/.well-known/oauth-protected-resource"
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, protectedResourceURL, nil)
		if err != nil {
			h.metadataFetchErr = fmt.Errorf("failed to create protected resource request: %w", err)
			return
		}

		req.Header.Set("Accept", "application/json")
		req.Header.Set("MCP-Protocol-Version", "2025-03-26")

		resp, err := h.httpClient.Do(req)
		if err != nil {
			h.metadataFetchErr = fmt.Errorf("failed to send protected resource request: %w", err)
			return
		}
		defer resp.Body.Close()

		// If we can't get the protected resource metadata, try OAuth Authorization Server discovery
		if resp.StatusCode != http.StatusOK {
			h.fetchMetadataFromURL(ctx, baseURL+"/.well-known/oauth-authorization-server")
			if h.serverMetadata != nil {
				return
			}
			// If that also fails, fall back to default endpoints
			metadata, err := h.getDefaultEndpoints(baseURL)
			if err != nil {
				h.metadataFetchErr = fmt.Errorf("failed to get default endpoints: %w", err)
				return
			}
			h.serverMetadata = metadata
			return
		}

		// Parse the protected resource metadata
		var protectedResource OAuthProtectedResource
		if err := json.NewDecoder(resp.Body).Decode(&protectedResource); err != nil {
			h.metadataFetchErr = fmt.Errorf("failed to decode protected resource response: %w", err)
			return
		}

		// If no authorization servers are specified, fall back to default endpoints
		if len(protectedResource.AuthorizationServers) == 0 {
			metadata, err := h.getDefaultEndpoints(baseURL)
			if err != nil {
				h.metadataFetchErr = fmt.Errorf("failed to get default endpoints: %w", err)
				return
			}
			h.serverMetadata = metadata
			return
		}

		// Probe each advertised authorization server until one responds.
		// Clear transient errors between attempts so we try all candidates.
		for _, authServerURL := range protectedResource.AuthorizationServers {
			for _, candidate := range authServerMetadataCandidates(authServerURL) {
				h.metadataFetchErr = nil
				h.fetchMetadataFromURL(ctx, candidate)
				if h.serverMetadata != nil {
					return
				}
			}
		}

		// If discovery fails for all advertised servers, use default endpoints
		// based on the first authorization server URL.
		metadata, err := h.getDefaultEndpoints(protectedResource.AuthorizationServers[0])
		if err != nil {
			h.metadataFetchErr = fmt.Errorf("failed to get default endpoints: %w", err)
			return
		}
		h.serverMetadata = metadata
	})

	if h.metadataFetchErr != nil {
		return nil, h.metadataFetchErr
	}

	return h.serverMetadata, nil
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

// authServerMetadataCandidates returns the ordered list of well-known metadata
// URLs to try for a given authorization server issuer identifier, following
// RFC 8414 section 3.1 and OpenID Connect Discovery 1.0 section 4.
//
// For an issuer with no path component (https://as.example.com), this is:
//  1. https://as.example.com/.well-known/oauth-authorization-server
//  2. https://as.example.com/.well-known/openid-configuration
//
// For an issuer with a path (https://as.example.com/tenant/x), RFC 8414
// inserts the well-known segment between host and path, while OIDC historically
// appended it. Both are tried:
//  1. https://as.example.com/.well-known/oauth-authorization-server/tenant/x
//  2. https://as.example.com/.well-known/openid-configuration/tenant/x
//  3. https://as.example.com/tenant/x/.well-known/openid-configuration
func authServerMetadataCandidates(issuer string) []string {
	u, err := url.Parse(strings.TrimSuffix(issuer, "/"))
	if err != nil || u.Host == "" {
		// Unparseable: fall back to naive suffix append so callers still get
		// something to probe rather than an empty slice.
		return []string{
			issuer + "/.well-known/oauth-authorization-server",
			issuer + "/.well-known/openid-configuration",
		}
	}

	// Build candidates by mutating only the Path field so that scheme, host,
	// port, and userinfo are preserved and correctly re-encoded by URL.String.
	issuerPath := strings.Trim(u.Path, "/")
	withPath := func(p string) string {
		c := *u
		c.Path, c.RawPath = p, ""
		return c.String()
	}

	if issuerPath == "" {
		return []string{
			withPath("/.well-known/oauth-authorization-server"),
			withPath("/.well-known/openid-configuration"),
		}
	}
	return []string{
		withPath("/.well-known/oauth-authorization-server/" + issuerPath),
		withPath("/.well-known/openid-configuration/" + issuerPath),
		withPath("/" + issuerPath + "/.well-known/openid-configuration"),
	}
}

// ParseResourceMetadataFromWWWAuthenticate extracts the resource_metadata URL
// from an RFC 9728 WWW-Authenticate challenge. The resource server advertises
// the location of its protected resource metadata on a 401 response via:
//
//	WWW-Authenticate: Bearer resource_metadata="https://rs.example.com/.well-known/oauth-protected-resource"
//
// Returns empty string if the header is absent or does not contain the
// parameter. Transports call this automatically on 401 responses and plumb
// the result into the OAuthHandler, so most callers need not invoke it directly.
func ParseResourceMetadataFromWWWAuthenticate(h http.Header) string {
	for _, challenge := range h.Values("WWW-Authenticate") {
		scheme, params, ok := strings.Cut(challenge, " ")
		if !ok || !strings.EqualFold(strings.TrimSpace(scheme), "Bearer") {
			continue
		}
		if v := parseAuthParam(params, "resource_metadata"); v != "" {
			return v
		}
	}
	return ""
}

// parseAuthParam extracts a named parameter from an RFC 9110 auth-param list.
// It handles quoted-string values (which may contain commas) without splitting
// on comma. Backslash-escapes inside quoted strings are not processed since
// RFC 9728 resource_metadata values are absolute URIs that never need escaping.
func parseAuthParam(params, key string) string {
	for len(params) > 0 {
		params = strings.TrimLeft(params, " \t,")

		eq := strings.IndexByte(params, '=')
		if eq < 0 {
			return ""
		}
		name := strings.TrimSpace(params[:eq])
		params = params[eq+1:]

		val, rest := consumeParamValue(params)
		params = rest

		if strings.EqualFold(name, key) {
			return val
		}
	}
	return ""
}

// consumeParamValue reads a single auth-param value (quoted or token) from the
// front of s and returns (value, remainder).
func consumeParamValue(s string) (val, rest string) {
	if len(s) == 0 {
		return "", ""
	}
	if s[0] == '"' {
		if end := strings.IndexByte(s[1:], '"'); end >= 0 {
			return s[1 : end+1], s[end+2:]
		}
		return s[1:], "" // unterminated quote — take rest
	}
	if end := strings.IndexAny(s, ", \t"); end >= 0 {
		return s[:end], s[end:]
	}
	return s, ""
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

	if h.config.ClientURI != "" {
		regRequest["client_uri"] = h.config.ClientURI
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
