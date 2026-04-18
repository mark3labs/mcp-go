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
	resourceURL      string // RFC 8707 resource indicator; set from protected resource metadata

	mu                           sync.RWMutex // Protects expectedState and protectedResourceMetadataURL
	expectedState                string       // Expected state value for CSRF protection
	protectedResourceMetadataURL string       // RFC 9728 §5.1: PRM URL advertised by the server via WWW-Authenticate
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

	// Per RFC 6749 §5.1, token_type is case-insensitive.
	// Normalize to "Bearer" for strict implementations.
	tokenType := token.TokenType
	if strings.EqualFold(tokenType, "bearer") {
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
	// RFC 8707: Include resource parameter on refresh requests
	if h.resourceURL != "" {
		data.Set("resource", h.resourceURL)
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

// SetProtectedResourceMetadataURL stores the OAuth 2.0 Protected Resource
// Metadata URL advertised by the server. When set, metadata discovery
// fetches this URL in preference to constructing one from the base URL's
// /.well-known/oauth-protected-resource path.
//
// This setter does not validate the URL; callers using it directly are
// trusted to pass a value obtained out of band. For values parsed from a
// 401 WWW-Authenticate header, prefer HandleUnauthorizedResponse, which
// applies origin validation before storing.
//
// Note: the first call to GetServerMetadata latches the resolved metadata
// through a sync.Once. A PRM URL set after that first call is not applied
// retroactively — set it (or let the transport's 401 handling set it)
// before any code path that triggers metadata discovery.
func (h *OAuthHandler) SetProtectedResourceMetadataURL(prmURL string) {
	h.mu.Lock()
	h.protectedResourceMetadataURL = prmURL
	h.mu.Unlock()
}

// ProtectedResourceMetadataURL returns the Protected Resource Metadata URL
// that will be used during metadata discovery, or an empty string if none
// has been set.
func (h *OAuthHandler) ProtectedResourceMetadataURL() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.protectedResourceMetadataURL
}

// HandleUnauthorizedResponse inspects a 401 response for RFC 9728 §5.1
// WWW-Authenticate challenges and, when one carries a resource_metadata
// parameter whose URL shares the protected resource's origin, stores it
// so subsequent metadata discovery can use it. It iterates every
// WWW-Authenticate header line (a response can carry multiple challenges
// — Basic, Bearer, etc. — each on its own line), takes the first
// resource_metadata value that validates, and silently ignores headers
// that are absent, malformed, or advertise an unrelated origin.
//
// Origin validation rejects URLs whose scheme or host differs from the
// OAuth handler's configured base URL. This prevents a compromised or
// misconfigured resource from redirecting clients to an attacker's
// metadata endpoint.
//
// It is safe to call with a nil response.
func (h *OAuthHandler) HandleUnauthorizedResponse(resp *http.Response) {
	if resp == nil {
		return
	}
	for _, header := range resp.Header.Values("WWW-Authenticate") {
		candidate := extractResourceMetadataURL(header)
		if candidate == "" {
			continue
		}
		if err := h.validateAdvertisedPRMURL(candidate); err != nil {
			continue
		}
		h.SetProtectedResourceMetadataURL(candidate)
		return
	}
}

// validateAdvertisedPRMURL enforces that a PRM URL advertised by the
// resource (i.e. parsed from an untrusted WWW-Authenticate header) shares
// the configured base URL's scheme and host. Returns a non-nil error when
// the candidate is unparseable, carries a different scheme or host, or
// when no base URL has been configured to validate against.
//
// RFC 9728 §3.2 requires the protected resource to serve its own metadata;
// this check rejects attempts to redirect discovery to an unrelated
// origin, which would otherwise let the resource point the client at an
// attacker-controlled OAuth metadata endpoint.
func (h *OAuthHandler) validateAdvertisedPRMURL(candidate string) error {
	if h.baseURL == "" {
		return errors.New("no base URL configured for origin validation")
	}
	base, err := url.Parse(h.baseURL)
	if err != nil {
		return fmt.Errorf("invalid base URL %q: %w", h.baseURL, err)
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return fmt.Errorf("invalid advertised PRM URL %q: %w", candidate, err)
	}
	if !strings.EqualFold(parsed.Scheme, base.Scheme) {
		return fmt.Errorf("advertised PRM URL scheme %q does not match base %q", parsed.Scheme, base.Scheme)
	}
	if !strings.EqualFold(parsed.Host, base.Host) {
		return fmt.Errorf("advertised PRM URL host %q does not match base %q", parsed.Host, base.Host)
	}
	return nil
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

		// Prefer a PRM URL advertised via WWW-Authenticate (RFC 9728 §5.1)
		// when the server provided one; this is required for deployments
		// where the PRM endpoint sits under a path that origin-based
		// construction cannot reach.
		protectedResourceURL := h.ProtectedResourceMetadataURL()
		prmFromAdvertisement := protectedResourceURL != ""
		if !prmFromAdvertisement {
			protectedResourceURL, err = buildWellKnownURL(baseURL, "oauth-protected-resource")
			if err != nil {
				h.metadataFetchErr = fmt.Errorf("failed to build protected resource URL: %w", err)
				return
			}
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
			authMetadataURL, err := buildWellKnownURL(baseURL, "oauth-authorization-server")
			if err != nil {
				h.metadataFetchErr = fmt.Errorf("failed to build authorization server metadata URL: %w", err)
				return
			}
			h.fetchMetadataFromURL(ctx, authMetadataURL)
			if h.serverMetadata != nil {
				h.metadataFetchErr = nil
				return
			}
			// If that also fails, fall back to default endpoints
			metadata, err := h.getDefaultEndpoints(baseURL)
			if err != nil {
				h.metadataFetchErr = fmt.Errorf("failed to get default endpoints: %w", err)
				return
			}
			h.serverMetadata = metadata
			h.metadataFetchErr = nil
			return
		}

		// Parse the protected resource metadata
		var protectedResource OAuthProtectedResource
		if err := json.NewDecoder(resp.Body).Decode(&protectedResource); err != nil {
			h.metadataFetchErr = fmt.Errorf("failed to decode protected resource response: %w", err)
			return
		}

		// RFC 9728 §3.3/§7.3: when metadata is fetched from a PRM URL the
		// server advertised via WWW-Authenticate (an untrusted network
		// input), the declared resource identifier MUST match the
		// protected resource the client addressed — otherwise the
		// response MUST NOT be used. This prevents a compromised resource
		// from pointing clients at another resource's OAuth metadata.
		// The check is scoped to the advertised path because the
		// well-known origin-constructed path has already been validated
		// by same-origin URL construction.
		if prmFromAdvertisement && protectedResource.Resource != "" &&
			!resourceIdentifiersEqual(protectedResource.Resource, baseURL) {
			h.metadataFetchErr = fmt.Errorf(
				"advertised protected resource metadata declares resource %q which does not match base URL %q",
				protectedResource.Resource, baseURL,
			)
			return
		}

		// RFC 8707: Capture the resource identifier for use in authorization requests.
		// If not provided in metadata, fall back to base URL per RFC 8707 Section 2:
		// "The client SHOULD use the base URI of the API as the resource parameter value
		// unless specific knowledge of the resource dictates otherwise."
		if protectedResource.Resource != "" {
			h.resourceURL = protectedResource.Resource
		} else {
			h.resourceURL = baseURL
		}

		// If no authorization servers are specified, fall back to default endpoints
		if len(protectedResource.AuthorizationServers) == 0 {
			metadata, err := h.getDefaultEndpoints(baseURL)
			if err != nil {
				h.metadataFetchErr = fmt.Errorf("failed to get default endpoints: %w", err)
				return
			}
			h.serverMetadata = metadata
			h.metadataFetchErr = nil
			return
		}

		// Use the first authorization server
		authServerURL := protectedResource.AuthorizationServers[0]

		// Try OAuth Authorization Server Metadata first
		authMetadataURL, err := buildWellKnownURL(authServerURL, "oauth-authorization-server")
		if err != nil {
			h.metadataFetchErr = fmt.Errorf("failed to build authorization server metadata URL: %w", err)
			return
		}
		h.fetchMetadataFromURL(ctx, authMetadataURL)
		if h.serverMetadata != nil {
			h.metadataFetchErr = nil
			return
		}

		// If OAuth Authorization Server Metadata discovery fails, try OpenID Connect discovery
		openidMetadataURL, err := buildWellKnownURL(authServerURL, "openid-configuration")
		if err != nil {
			h.metadataFetchErr = fmt.Errorf("failed to build openid metadata URL: %w", err)
			return
		}
		h.fetchMetadataFromURL(ctx, openidMetadataURL)
		if h.serverMetadata != nil {
			h.metadataFetchErr = nil
			return
		}

		// If both discovery methods fail, use default endpoints based on the authorization server URL
		metadata, err := h.getDefaultEndpoints(authServerURL)
		if err != nil {
			h.metadataFetchErr = fmt.Errorf("failed to get default endpoints: %w", err)
			return
		}
		h.serverMetadata = metadata
		h.metadataFetchErr = nil
	})

	if h.metadataFetchErr != nil {
		return nil, h.metadataFetchErr
	}

	return h.serverMetadata, nil
}

func buildWellKnownURL(baseURL string, suffix string) (string, error) {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse base URL: %w", err)
	}

	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return "", fmt.Errorf("invalid base URL: missing scheme or host in %q", baseURL)
	}

	path := strings.TrimSuffix(parsedURL.EscapedPath(), "/")
	root := fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)
	if path == "" || path == "/" {
		return root + "/.well-known/" + suffix, nil
	}

	return root + "/.well-known/" + suffix + path, nil
}

// extractResourceMetadataURL returns the resource_metadata parameter value
// from a WWW-Authenticate header per RFC 9728 §5.1, or an empty string when
// the header is empty, no such parameter is present, or the value is
// malformed. Parameter names are matched case-insensitively per
// RFC 9110 §11.2; both quoted-string and token value forms are accepted.
func extractResourceMetadataURL(header string) string {
	const target = "resource_metadata"
	i := 0
	for i < len(header) {
		// Advance to the next token start.
		for i < len(header) && !isAuthTokenChar(header[i]) {
			i++
		}
		nameStart := i
		for i < len(header) && isAuthTokenChar(header[i]) {
			i++
		}
		name := header[nameStart:i]
		// Skip optional whitespace between the name and '='.
		for i < len(header) && (header[i] == ' ' || header[i] == '\t') {
			i++
		}
		if i >= len(header) || header[i] != '=' {
			// Name was a scheme token (e.g. "Bearer"), not a parameter.
			continue
		}
		// Skip '=' and optional whitespace.
		i++
		for i < len(header) && (header[i] == ' ' || header[i] == '\t') {
			i++
		}
		value, next := parseAuthParamValue(header, i)
		i = next
		if strings.EqualFold(name, target) {
			return value
		}
	}
	return ""
}

// parseAuthParamValue reads a single WWW-Authenticate parameter value
// starting at offset i: a quoted-string (with backslash escapes) when the
// first byte is '"', otherwise a bare token. It returns the decoded value
// and the index of the first byte after it.
func parseAuthParamValue(s string, i int) (string, int) {
	if i >= len(s) {
		return "", i
	}
	if s[i] == '"' {
		i++
		var b strings.Builder
		for i < len(s) {
			c := s[i]
			if c == '\\' {
				if i+1 < len(s) {
					b.WriteByte(s[i+1])
					i += 2
					continue
				}
				// Lone trailing backslash in a truncated quoted string:
				// drop it rather than emitting a stray '\'.
				return b.String(), i + 1
			}
			if c == '"' {
				return b.String(), i + 1
			}
			b.WriteByte(c)
			i++
		}
		return b.String(), i
	}
	start := i
	for i < len(s) && isAuthTokenChar(s[i]) {
		i++
	}
	return s[start:i], i
}

// resourceIdentifiersEqual reports whether two OAuth protected resource
// identifiers refer to the same resource for the purposes of RFC 9728 §3.3
// equality checks. Scheme and host are compared case-insensitively and a
// single trailing slash on either path is ignored; query, fragment, and
// userinfo components are considered significant. Unparseable inputs
// fall back to exact string equality.
func resourceIdentifiersEqual(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	if errA != nil || errB != nil {
		return a == b
	}
	if !strings.EqualFold(ua.Scheme, ub.Scheme) {
		return false
	}
	if !strings.EqualFold(ua.Host, ub.Host) {
		return false
	}
	if strings.TrimSuffix(ua.Path, "/") != strings.TrimSuffix(ub.Path, "/") {
		return false
	}
	if ua.RawQuery != ub.RawQuery {
		return false
	}
	if ua.Fragment != ub.Fragment {
		return false
	}
	return ua.User.String() == ub.User.String()
}

// isAuthTokenChar reports whether c is a valid RFC 9110 §5.6.2 token
// character — the character class used for scheme and parameter names in
// WWW-Authenticate.
func isAuthTokenChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0
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

	if h.config.ClientURI != "" {
		regRequest["client_uri"] = h.config.ClientURI
	}

	// Add client_secret if this is a confidential client
	if h.config.ClientSecret != "" {
		regRequest["token_endpoint_auth_method"] = "client_secret_basic"
	}

	// RFC 8707: Include resource parameter in client registration
	if h.resourceURL != "" {
		regRequest["resource"] = h.resourceURL
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

	// RFC 8707: Include resource parameter in token exchange
	if h.resourceURL != "" {
		data.Set("resource", h.resourceURL)
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

	// RFC 8707: Include resource parameter in authorization URL
	if h.resourceURL != "" {
		params.Set("resource", h.resourceURL)
	}

	return metadata.AuthorizationEndpoint + "?" + params.Encode(), nil
}
