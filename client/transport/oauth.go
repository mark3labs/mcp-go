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

// maxMetadataBodyBytes caps the size of an OAuth metadata document we are
// willing to read.
const maxMetadataBodyBytes = 1 << 20 // 1 MiB

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
	// ProtectedResourceMetadataURL is the URL to the OAuth protected resource metadata
	// per RFC9728. If set, this URL will be used to discover the authorization server.
	// This is typically extracted from the WWW-Authenticate header's resource_metadata parameter.
	ProtectedResourceMetadataURL string
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
	ExpiresAt time.Time `json:"expires_at,omitzero"`
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
// as defined in RFC 8414 (https://www.rfc-editor.org/rfc/rfc8414.html).
//
// URL-bearing fields are validated by validateAuthServerMetadataURLs to
// reject non-http(s) schemes (e.g. javascript:, data:, file:) that could
// otherwise be injected by a hostile authorization server.
//
// Signed metadata (the "signed_metadata" JWT field) is not supported.
type AuthServerMetadata struct {
	Issuer                                             string   `json:"issuer"`
	AuthorizationEndpoint                              string   `json:"authorization_endpoint"`
	TokenEndpoint                                      string   `json:"token_endpoint"`
	JwksURI                                            string   `json:"jwks_uri,omitempty"`
	RegistrationEndpoint                               string   `json:"registration_endpoint,omitempty"`
	ScopesSupported                                    []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported                             []string `json:"response_types_supported"`
	ResponseModesSupported                             []string `json:"response_modes_supported,omitempty"`
	GrantTypesSupported                                []string `json:"grant_types_supported,omitempty"`
	TokenEndpointAuthMethodsSupported                  []string `json:"token_endpoint_auth_methods_supported,omitempty"`
	TokenEndpointAuthSigningAlgValuesSupported         []string `json:"token_endpoint_auth_signing_alg_values_supported,omitempty"`
	ServiceDocumentation                               string   `json:"service_documentation,omitempty"`
	UILocalesSupported                                 []string `json:"ui_locales_supported,omitempty"`
	OpPolicyURI                                        string   `json:"op_policy_uri,omitempty"`
	OpTOSURI                                           string   `json:"op_tos_uri,omitempty"`
	RevocationEndpoint                                 string   `json:"revocation_endpoint,omitempty"`
	RevocationEndpointAuthMethodsSupported             []string `json:"revocation_endpoint_auth_methods_supported,omitempty"`
	RevocationEndpointAuthSigningAlgValuesSupported    []string `json:"revocation_endpoint_auth_signing_alg_values_supported,omitempty"`
	IntrospectionEndpoint                              string   `json:"introspection_endpoint,omitempty"`
	IntrospectionEndpointAuthMethodsSupported          []string `json:"introspection_endpoint_auth_methods_supported,omitempty"`
	IntrospectionEndpointAuthSigningAlgValuesSupported []string `json:"introspection_endpoint_auth_signing_alg_values_supported,omitempty"`
	CodeChallengeMethodsSupported                      []string `json:"code_challenge_methods_supported,omitempty"`
}

// OAuthHandler handles OAuth authentication for HTTP requests
type OAuthHandler struct {
	config           OAuthConfig
	httpClient       *http.Client
	serverMetadata   *AuthServerMetadata
	metadataFetchErr error
	metadataOnce     sync.Once
	baseURL          string
	metadataMu       sync.Mutex // Protects baseURL, serverMetadata, metadataFetchErr, metadataOnce, config.ProtectedResourceMetadataURL, and resourceURL
	resourceURL      string     // RFC 8707 resource indicator; set from protected resource metadata

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
	if resourceURL := h.getResourceURL(); resourceURL != "" {
		data.Set("resource", resourceURL)
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

// SetProtectedResourceMetadataURL updates the protected resource metadata URL
// and resets the cached server metadata so it will be re-discovered on the next call.
//
// This setter does not validate the URL; callers that pass values obtained
// out of band are trusted. For values parsed from a 401 WWW-Authenticate
// header, prefer HandleUnauthorizedResponse, which applies origin
// validation before storing.
func (h *OAuthHandler) SetProtectedResourceMetadataURL(u string) {
	h.metadataMu.Lock()
	defer h.metadataMu.Unlock()
	h.config.ProtectedResourceMetadataURL = u
	h.serverMetadata = nil
	h.metadataFetchErr = nil
	h.metadataOnce = sync.Once{}
	h.resourceURL = ""
}

// HandleUnauthorizedResponse inspects a 401 response for RFC 9728 §5.1
// WWW-Authenticate challenges and, when one carries a resource_metadata
// parameter whose URL shares the protected resource's origin, stores it
// so subsequent metadata discovery can use it. It iterates every
// WWW-Authenticate header line (a response can carry multiple challenges
// — Basic, Bearer, etc. — each on its own line) and every
// resource_metadata parameter within each line, takes the first
// candidate that validates, and silently ignores headers that are
// absent, malformed, or advertise an unrelated origin.
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
		for _, candidate := range extractResourceMetadataURLs(header) {
			if err := h.validateAdvertisedPRMURL(candidate); err != nil {
				continue
			}
			h.SetProtectedResourceMetadataURL(candidate)
			return
		}
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
	h.metadataMu.Lock()
	baseURL := h.baseURL
	h.metadataMu.Unlock()
	if baseURL == "" {
		return errors.New("no base URL configured for origin validation")
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("invalid base URL %q: %w", baseURL, err)
	}
	// url.Parse accepts relative references (empty Scheme and Host)
	// without error. Reject those explicitly so two empty values do not
	// EqualFold-match each other and bypass origin validation.
	if base.Scheme == "" || base.Host == "" {
		return fmt.Errorf("base URL %q is not absolute (missing scheme or host)", baseURL)
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return fmt.Errorf("invalid advertised PRM URL %q: %w", candidate, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("advertised PRM URL %q is not absolute (missing scheme or host)", candidate)
	}
	if !strings.EqualFold(parsed.Scheme, base.Scheme) {
		return fmt.Errorf("advertised PRM URL scheme %q does not match base %q", parsed.Scheme, base.Scheme)
	}
	if !strings.EqualFold(parsed.Host, base.Host) {
		return fmt.Errorf("advertised PRM URL host %q does not match base %q", parsed.Host, base.Host)
	}
	return nil
}

// getResourceURL returns the RFC 8707 resource indicator under metadataMu.
func (h *OAuthHandler) getResourceURL() string {
	h.metadataMu.Lock()
	defer h.metadataMu.Unlock()
	return h.resourceURL
}

// GetClientSecret returns the client secret
func (h *OAuthHandler) GetClientSecret() string {
	return h.config.ClientSecret
}

// SetBaseURL sets the base URL for the API server.
// Must be called before any calls to getServerMetadata (i.e., during initialization).
func (h *OAuthHandler) SetBaseURL(baseURL string) {
	h.metadataMu.Lock()
	defer h.metadataMu.Unlock()
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

// getServerMetadata fetches the OAuth server metadata
func (h *OAuthHandler) getServerMetadata(ctx context.Context) (*AuthServerMetadata, error) {
	h.metadataMu.Lock()
	defer h.metadataMu.Unlock()
	h.metadataOnce.Do(func() {
		// If AuthServerMetadataURL is explicitly provided, use it directly
		if h.config.AuthServerMetadataURL != "" {
			h.fetchMetadataFromURL(ctx, h.config.AuthServerMetadataURL)
			return
		}

		// Always extract base URL for fallback scenarios
		baseURL, err := h.extractBaseURL()
		if err != nil {
			h.metadataFetchErr = fmt.Errorf("failed to extract base URL: %w", err)
			return
		}

		// Determine the protected resource metadata URL with priority:
		// 1. Explicit config (ProtectedResourceMetadataURL from RFC9728 WWW-Authenticate header)
		// 2. Constructed from base URL
		var protectedResourceURL string
		explicitMetadataURL := h.config.ProtectedResourceMetadataURL != ""
		if explicitMetadataURL {
			protectedResourceURL = h.config.ProtectedResourceMetadataURL
		} else {
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

		// If we can't get the protected resource metadata, try OAuth Authorization Server discovery.
		// However, if the resource_metadata URL was explicitly provided (via RFC 9728), don't
		// fall back to baseURL-derived discovery — the server specifically indicated where to
		// find metadata, so falling back would mask the signal.
		if resp.StatusCode != http.StatusOK {
			if explicitMetadataURL {
				h.metadataFetchErr = fmt.Errorf("protected resource metadata discovery failed for explicit URL %q: status %d", protectedResourceURL, resp.StatusCode)
				return
			}
			for _, u := range authorizationServerMetadataURLs(baseURL) {
				h.fetchMetadataFromURL(ctx, u)
				if h.serverMetadata != nil {
					h.metadataFetchErr = nil
					return
				}
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
		// response MUST NOT be used. An advertised PRM response that
		// omits the resource field is also rejected: since the PRM
		// endpoint may not share an origin with the protected resource,
		// the response cannot be implicitly trusted without an explicit
		// binding.
		//
		// The check is scoped to the advertised path because the
		// well-known origin-constructed path is already bound to the
		// protected resource by same-origin URL construction.
		if explicitMetadataURL {
			if protectedResource.Resource == "" {
				h.metadataFetchErr = fmt.Errorf(
					"advertised protected resource metadata from %q omits required resource field",
					protectedResourceURL,
				)
				return
			}
			if !resourceIdentifiersEqual(protectedResource.Resource, baseURL) {
				h.metadataFetchErr = fmt.Errorf(
					"advertised protected resource metadata declares resource %q which does not match base URL %q",
					protectedResource.Resource, baseURL,
				)
				return
			}
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

		// Try the MCP-specified discovery URLs in order (RFC 8414 with path
		// insertion, plus OpenID Connect Discovery variants).
		for _, u := range authorizationServerMetadataURLs(authServerURL) {
			h.fetchMetadataFromURL(ctx, u)
			if h.serverMetadata != nil {
				h.metadataFetchErr = nil
				return
			}
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

// buildWellKnownURL constructs a well-known discovery URL by inserting the
// given suffix between the authority and path of baseURL (RFC 8414 §3 /
// RFC 9728 path-insertion semantics).
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

// resourceIdentifiersEqual reports whether two OAuth protected resource
// identifiers refer to the same resource for the purposes of RFC 9728 §3.3
// equality checks. Scheme and host are compared case-insensitively per
// RFC 3986 §3.1 / §3.2.2, and a single trailing slash on either path is
// ignored because real-world OAuth deployments routinely emit the
// resource with or without it for the same URL; rejecting that variant
// would produce false positives on legitimate servers. Query, fragment,
// and userinfo components are significant. Unparseable inputs fall back
// to exact string equality.
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
	// Use EscapedPath rather than Path so percent-encoded reserved
	// characters stay distinct from their decoded forms (e.g. "a%2Fb"
	// must not compare equal to "a/b"), preserving RFC 3986 segment
	// semantics.
	if strings.TrimSuffix(ua.EscapedPath(), "/") != strings.TrimSuffix(ub.EscapedPath(), "/") {
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

// fetchMetadataFromURL fetches and parses OAuth server metadata from a URL.
// Non-200 responses are skipped silently so the caller can try the next
// candidate URL.
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
		return
	}

	var metadata AuthServerMetadata
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxMetadataBodyBytes))
	if err := dec.Decode(&metadata); err != nil {
		h.metadataFetchErr = fmt.Errorf("failed to decode metadata response: %w", err)
		return
	}

	if err := validateAuthServerMetadataURLs(&metadata); err != nil {
		h.metadataFetchErr = fmt.Errorf("invalid authorization server metadata from %s: %w", metadataURL, err)
		return
	}

	h.serverMetadata = &metadata
}

// validateAuthServerMetadataURLs ensures every URL-bearing field in m uses an
// http or https scheme. This prevents a hostile authorization server from
// advertising values like "javascript:..." or "file:..." that could be
// reflected into a browser or the local file system by downstream consumers.
// Empty optional fields are permitted.
func validateAuthServerMetadataURLs(m *AuthServerMetadata) error {
	fields := []struct {
		name  string
		value string
	}{
		{"issuer", m.Issuer},
		{"authorization_endpoint", m.AuthorizationEndpoint},
		{"token_endpoint", m.TokenEndpoint},
		{"jwks_uri", m.JwksURI},
		{"registration_endpoint", m.RegistrationEndpoint},
		{"service_documentation", m.ServiceDocumentation},
		{"op_policy_uri", m.OpPolicyURI},
		{"op_tos_uri", m.OpTOSURI},
		{"revocation_endpoint", m.RevocationEndpoint},
		{"introspection_endpoint", m.IntrospectionEndpoint},
	}
	for _, f := range fields {
		if f.value == "" {
			continue
		}
		u, err := url.Parse(f.value)
		if err != nil {
			return fmt.Errorf("%s: %w", f.name, err)
		}
		scheme := strings.ToLower(u.Scheme)
		if scheme != "http" && scheme != "https" {
			return fmt.Errorf("%s has disallowed scheme %q", f.name, u.Scheme)
		}
		if u.Host == "" {
			return fmt.Errorf("%s is missing host", f.name)
		}
	}
	return nil
}

// authorizationServerMetadataURLs returns the ordered list of discovery URLs to
// try for a given issuer URL, following the MCP authorization spec. This
// implements RFC 8414 §3 path-insertion semantics (the well-known segment is
// inserted between the authority and the issuer path, not appended), plus
// OpenID Connect Discovery 1.0 fallbacks.
func authorizationServerMetadataURLs(issuerURL string) []string {
	u, err := url.Parse(issuerURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil
	}
	u.RawQuery = ""
	u.Fragment = ""

	originalPath := strings.Trim(u.Path, "/")

	var urls []string
	if originalPath == "" {
		u.Path = "/.well-known/oauth-authorization-server"
		urls = append(urls, u.String())
		u.Path = "/.well-known/openid-configuration"
		urls = append(urls, u.String())
		return urls
	}
	u.Path = "/.well-known/oauth-authorization-server/" + originalPath
	urls = append(urls, u.String())
	u.Path = "/.well-known/openid-configuration/" + originalPath
	urls = append(urls, u.String())
	u.Path = "/" + originalPath + "/.well-known/openid-configuration"
	urls = append(urls, u.String())
	return urls
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
		regRequest["token_endpoint_auth_method"] = "client_secret_post"
	}

	// RFC 8707: Include resource parameter in client registration
	if resourceURL := h.getResourceURL(); resourceURL != "" {
		regRequest["resource"] = resourceURL
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
	if resourceURL := h.getResourceURL(); resourceURL != "" {
		data.Set("resource", resourceURL)
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
	if resourceURL := h.getResourceURL(); resourceURL != "" {
		params.Set("resource", resourceURL)
	}

	return metadata.AuthorizationEndpoint + "?" + params.Encode(), nil
}
