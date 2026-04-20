package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/util"
)

type StreamableHTTPCOption func(*StreamableHTTP)

// WithContinuousListening enables receiving server-to-client notifications when no request is in flight.
// In particular, if you want to receive global notifications from the server (like ToolListChangedNotification),
// you should enable this option.
//
// It will establish a standalone long-live GET HTTP connection to the server.
// https://modelcontextprotocol.io/specification/2025-03-26/basic/transports#listening-for-messages-from-the-server
// NOTICE: Even enabled, the server may not support this feature.
func WithContinuousListening() StreamableHTTPCOption {
	return func(sc *StreamableHTTP) {
		sc.getListeningEnabled = true
	}
}

// WithHTTPClient sets a custom HTTP client on the StreamableHTTP transport.
func WithHTTPBasicClient(client *http.Client) StreamableHTTPCOption {
	return func(sc *StreamableHTTP) {
		sc.httpClient = client
	}
}

func WithHTTPHeaders(headers map[string]string) StreamableHTTPCOption {
	return func(sc *StreamableHTTP) {
		sc.headers = headers
	}
}

func WithHTTPHeaderFunc(headerFunc HTTPHeaderFunc) StreamableHTTPCOption {
	return func(sc *StreamableHTTP) {
		sc.headerFunc = headerFunc
	}
}

// WithHTTPTimeout sets the timeout for a HTTP request and stream.
func WithHTTPTimeout(timeout time.Duration) StreamableHTTPCOption {
	return func(sc *StreamableHTTP) {
		sc.httpClient.Timeout = timeout
	}
}

// WithHTTPOAuth enables OAuth authentication for the client.
func WithHTTPOAuth(config OAuthConfig) StreamableHTTPCOption {
	return func(sc *StreamableHTTP) {
		sc.oauthHandler = NewOAuthHandler(config)
	}
}

// WithHTTPLogger sets a custom logger for the StreamableHTTP transport.
func WithHTTPLogger(logger util.Logger) StreamableHTTPCOption {
	return func(sc *StreamableHTTP) {
		sc.logger = logger
	}
}

// Deprecated: Use [WithHTTPLogger] instead.
func WithLogger(logger util.Logger) StreamableHTTPCOption {
	return WithHTTPLogger(logger)
}

// WithSession creates a client with a pre-configured session
func WithSession(sessionID string) StreamableHTTPCOption {
	return func(sc *StreamableHTTP) {
		sc.sessionID.Store(sessionID)
	}
}

// WithStreamableHTTPHost sets a custom Host header for the StreamableHTTP client, enabling manual DNS resolution.
// This allows connecting to an IP address while sending a specific Host header to the server.
// For example, connecting to "http://192.168.1.100:8080/mcp" but sending Host: "api.example.com"
func WithStreamableHTTPHost(host string) StreamableHTTPCOption {
	return func(sc *StreamableHTTP) {
		sc.host = host
	}
}

// StreamableHTTP implements Streamable HTTP transport.
//
// It transmits JSON-RPC messages over individual HTTP requests. One message per request.
// The HTTP response body can either be a single JSON-RPC response,
// or an upgraded SSE stream that concludes with a JSON-RPC response for the same request.
//
// https://modelcontextprotocol.io/specification/2025-03-26/basic/transports
//
// The current implementation does not support the following features:
//   - resuming stream
//     (https://modelcontextprotocol.io/specification/2025-03-26/basic/transports#resumability-and-redelivery)
type StreamableHTTP struct {
	serverURL           *url.URL
	httpClient          *http.Client
	headers             map[string]string
	headerFunc          HTTPHeaderFunc
	host                string
	logger              util.Logger
	getListeningEnabled bool

	sessionID       atomic.Value // string
	protocolVersion atomic.Value // string

	initialized     chan struct{}
	initializedOnce sync.Once

	notificationHandler func(mcp.JSONRPCNotification)
	notifyMu            sync.RWMutex

	// Request handler for incoming server-to-client requests (like sampling)
	requestHandler RequestHandler
	requestMu      sync.RWMutex

	closed    chan struct{}
	closeOnce sync.Once

	// OAuth support
	oauthHandler *OAuthHandler
}

// NewStreamableHTTP creates a new Streamable HTTP transport with the given server URL.
// Returns an error if the URL is invalid.
func NewStreamableHTTP(serverURL string, options ...StreamableHTTPCOption) (*StreamableHTTP, error) {
	parsedURL, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	smc := &StreamableHTTP{
		serverURL:   parsedURL,
		httpClient:  &http.Client{},
		headers:     make(map[string]string),
		closed:      make(chan struct{}),
		logger:      util.DefaultLogger(),
		initialized: make(chan struct{}),
	}
	smc.sessionID.Store("") // set initial value to simplify later usage

	for _, opt := range options {
		if opt != nil {
			opt(smc)
		}
	}

	// If OAuth is configured, set the base URL for metadata discovery
	if smc.oauthHandler != nil {
		discoveryURL := *parsedURL
		discoveryURL.RawQuery = ""
		discoveryURL.Fragment = ""
		baseURL := discoveryURL.String()
		smc.oauthHandler.SetBaseURL(baseURL)
	}

	return smc, nil
}

// Start initiates the HTTP connection to the server.
func (c *StreamableHTTP) Start(ctx context.Context) error {
	// Start is idempotent - check if already initialized
	select {
	case <-c.initialized:
		return nil
	default:
	}

	// For Streamable HTTP, we don't need to establish a persistent connection by default
	if c.getListeningEnabled {
		go func() {
			select {
			case <-c.initialized:
				ctx, cancel := c.contextAwareOfClientClose(ctx)
				defer cancel()
				c.listenForever(ctx)
			case <-c.closed:
				return
			case <-ctx.Done():
				return
			}
		}()
	}

	return nil
}

// Close closes the all the HTTP connections to the server.
func (c *StreamableHTTP) Close() error {
	c.closeOnce.Do(func() {
		// Cancel all in-flight requests
		close(c.closed)

		sessionId := c.sessionID.Load().(string)
		if sessionId != "" {
			c.sessionID.Store("")
			// notify server session closed
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.serverURL.String(), nil)
			if err != nil {
				c.logger.Errorf("failed to create close request: %v", err)
				return
			}
			req.Header.Set(HeaderKeySessionID, sessionId)
			// Set protocol version header if negotiated
			if v := c.protocolVersion.Load(); v != nil {
				if version, ok := v.(string); ok && version != "" {
					req.Header.Set(HeaderKeyProtocolVersion, version)
				}
			}

			// Set custom Host header if provided
			if c.host != "" {
				req.Host = c.host
			}
			res, err := c.httpClient.Do(req)
			if err != nil {
				c.logger.Errorf("failed to send close request: %v", err)
				return
			}
			res.Body.Close()
		}
	})
	return nil
}

// SetProtocolVersion sets the negotiated protocol version for this connection.
func (c *StreamableHTTP) SetProtocolVersion(version string) {
	c.protocolVersion.Store(version)
}

// ErrOAuthAuthorizationRequired is a sentinel error for OAuth authorization required
var ErrOAuthAuthorizationRequired = errors.New("no valid token available, authorization required")

// ErrAuthorizationRequired is a sentinel error for authorization required (401)
var ErrAuthorizationRequired = errors.New("authorization required")

// parseAuthParams parses the auth-params from a WWW-Authenticate header value
// per RFC 7235. It skips the auth-scheme (first token) and returns a map of
// key=value pairs. Values may be tokens or quoted-strings (with backslash
// escaping per RFC 7230 §3.2.6).
func parseAuthParams(header string) map[string]string {
	params := make(map[string]string)

	// Skip leading whitespace
	header = strings.TrimSpace(header)
	if header == "" {
		return params
	}

	// Skip the auth-scheme (first token before space)
	_, rest, found := strings.Cut(header, " ")
	if !found {
		return params // auth-scheme only, no params
	}
	rest = strings.TrimSpace(rest)

	for rest != "" {
		rest = strings.TrimSpace(rest)
		if rest == "" {
			break
		}

		// Parse key
		eqIdx := strings.IndexByte(rest, '=')
		if eqIdx == -1 {
			break
		}
		key := strings.TrimSpace(rest[:eqIdx])
		rest = strings.TrimLeft(rest[eqIdx+1:], " \t")

		// Parse value: quoted-string or token
		var value string
		if len(rest) > 0 && rest[0] == '"' {
			value, rest = parseQuotedString(rest)
		} else {
			// Token value: ends at comma, space, or end of string
			end := strings.IndexAny(rest, ", \t")
			if end == -1 {
				value = rest
				rest = ""
			} else {
				value = rest[:end]
				rest = rest[end:]
			}
		}

		params[key] = value

		// Skip comma separator
		rest = strings.TrimSpace(rest)
		if len(rest) > 0 && rest[0] == ',' {
			rest = rest[1:]
		}
	}

	return params
}

// parseQuotedString parses a quoted-string value per RFC 7230 §3.2.6.
// Input must start with a double-quote. Returns the unescaped value and
// the remaining unparsed input after the closing quote.
func parseQuotedString(s string) (value, rest string) {
	if len(s) == 0 || s[0] != '"' {
		return "", s
	}
	s = s[1:] // skip opening quote

	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			if i+1 < len(s) {
				b.WriteByte(s[i+1])
				i++ // skip escaped char
			}
		case '"':
			return b.String(), s[i+1:]
		default:
			b.WriteByte(s[i])
		}
	}
	// No closing quote found; return what we have
	return b.String(), ""
}

// extractResourceMetadataURL extracts the resource_metadata parameter from WWW-Authenticate headers
// per RFC9728 Section 5.1. Scans all provided header values since a response may contain multiple
// WWW-Authenticate headers (RFC 9110). Returns empty string if not found.
// Example: Bearer resource_metadata="https://resource.example.com/.well-known/oauth-protected-resource"
func extractResourceMetadataURL(wwwAuthHeaders []string) string {
	for _, header := range wwwAuthHeaders {
		for _, u := range extractResourceMetadataURLs(header) {
			if u != "" {
				return u
			}
		}
	}
	return ""
}

// extractResourceMetadataURLs returns every resource_metadata parameter
// value from a single WWW-Authenticate header value per RFC 9728 §5.1,
// in the order they appear. Returns an empty slice when the header is
// empty or no such parameters are present. Parameter names are matched
// case-insensitively per RFC 9110 §11.2; both quoted-string and token
// value forms are accepted. Multiple occurrences are possible when a
// single header value contains several Bearer challenges each carrying
// their own resource_metadata — an attacker-controlled first candidate
// must not mask a legitimate later one.
func extractResourceMetadataURLs(header string) []string {
	const target = "resource_metadata"
	var out []string
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
		value, next, ok := parseAuthParamValue(header, i)
		i = next
		if !ok {
			continue
		}
		if value != "" && strings.EqualFold(name, target) {
			out = append(out, value)
		}
	}
	return out
}

// parseAuthParamValue reads a single WWW-Authenticate parameter value
// starting at offset i: a quoted-string (with backslash escapes) when the
// first byte is '"', otherwise a bare token. It returns the decoded
// value, the index of the first byte after it, and whether the value
// was well-formed. Truncated quoted strings (no closing '"') and lone
// trailing backslashes yield ok=false so malformed input is rejected
// rather than producing a partial value.
func parseAuthParamValue(s string, i int) (string, int, bool) {
	if i >= len(s) {
		return "", i, false
	}
	if s[i] == '"' {
		i++
		var b strings.Builder
		for i < len(s) {
			c := s[i]
			if c == '\\' {
				if i+1 >= len(s) {
					// Lone trailing backslash — the quoted string was
					// truncated mid-escape, so the value is malformed.
					return "", i + 1, false
				}
				b.WriteByte(s[i+1])
				i += 2
				continue
			}
			if c == '"' {
				return b.String(), i + 1, true
			}
			b.WriteByte(c)
			i++
		}
		// Reached end of input without a closing '"'.
		return "", i, false
	}
	start := i
	for i < len(s) && isAuthTokenChar(s[i]) {
		i++
	}
	return s[start:i], i, i > start
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

// AuthorizationRequiredError is returned when a 401 Unauthorized response is received.
// It contains the protected resource metadata URL from the WWW-Authenticate header if present.
type AuthorizationRequiredError struct {
	ResourceMetadataURL string // Extracted from WWW-Authenticate header per RFC9728
}

func (e *AuthorizationRequiredError) Error() string {
	return ErrAuthorizationRequired.Error()
}

func (e *AuthorizationRequiredError) Unwrap() error {
	return ErrAuthorizationRequired
}

// OAuthAuthorizationRequiredError is returned when OAuth authorization is required
// and an OAuth handler is available.
type OAuthAuthorizationRequiredError struct {
	Handler *OAuthHandler
	AuthorizationRequiredError
}

func (e *OAuthAuthorizationRequiredError) Error() string {
	return ErrOAuthAuthorizationRequired.Error()
}

func (e *OAuthAuthorizationRequiredError) Unwrap() error {
	return ErrOAuthAuthorizationRequired
}

// SendRequest sends a JSON-RPC request to the server and waits for a response.
// Returns the raw JSON response message or an error if the request fails.
func (c *StreamableHTTP) SendRequest(
	ctx context.Context,
	request JSONRPCRequest,
) (*JSONRPCResponse, error) {
	// Marshal request
	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	ctx, cancel := c.contextAwareOfClientClose(ctx)

	resp, err := c.sendHTTP(ctx, http.MethodPost, bytes.NewReader(requestBody), "application/json, text/event-stream", request.Header)
	if err != nil {
		cancel()
		if errors.Is(err, ErrSessionTerminated) && request.Method == string(mcp.MethodInitialize) {
			// Per the MCP spec's backwards compatibility section: a 404 on an
			// initialize POST means the server likely only supports legacy SSE.
			return nil, ErrLegacySSEServer
		}
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Only proceed if we have a valid response.
	if resp == nil {
		cancel()
		return nil, fmt.Errorf("failed to send request: no response received")
	}
	// Cancel the context before closing the body. On HTTP/2, Close() blocks in a
	// select on cs.donec (stream cleanup) or cs.ctx.Done() (context cancellation).
	// If cc.wmu is contended, cs.donec may never close, making ctx.Done() the only
	// exit path. Canceling first guarantees Close() returns promptly.
	defer func() { cancel(); resp.Body.Close() }()

	// Check if we got an error response
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {

		// Handle unauthorized error
		if resp.StatusCode == http.StatusUnauthorized {
			// Extract discovered metadata URL per RFC9728
			metadataURL := extractResourceMetadataURL(resp.Header.Values("WWW-Authenticate"))

			// Feed discovered URL back to OAuthHandler so next auth attempt uses it.
			// HandleUnauthorizedResponse applies RFC 9728 origin validation — a
			// compromised resource advertising a cross-origin PRM URL is ignored.
			if c.oauthHandler != nil {
				c.oauthHandler.HandleUnauthorizedResponse(resp)
			}

			// If OAuth handler exists, return OAuth-specific error
			if c.oauthHandler != nil {
				return nil, &OAuthAuthorizationRequiredError{
					Handler: c.oauthHandler,
					AuthorizationRequiredError: AuthorizationRequiredError{
						ResourceMetadataURL: metadataURL,
					},
				}
			}

			// No OAuth handler, return base authorization error
			return nil, &AuthorizationRequiredError{
				ResourceMetadataURL: metadataURL,
			}
		}

		// Per the MCP spec's backwards compatibility section: if an initialize
		// POST receives an HTTP 4xx (e.g. 405 Method Not Allowed, 404 Not Found),
		// the server likely only supports the legacy HTTP+SSE transport.
		if request.Method == string(mcp.MethodInitialize) && resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return nil, ErrLegacySSEServer
		}

		// handle error response
		var errResponse JSONRPCResponse
		body, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(body, &errResponse); err == nil {
			return &errResponse, nil
		}
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, body)
	}

	if request.Method == string(mcp.MethodInitialize) {
		// saved the received session ID in the response
		// empty session ID is allowed
		if sessionID := resp.Header.Get(HeaderKeySessionID); sessionID != "" {
			c.sessionID.Store(sessionID)
		}

		c.initializedOnce.Do(func() {
			close(c.initialized)
		})
	}

	// Handle different response types
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	switch mediaType {
	case "application/json":
		// Single response
		var response JSONRPCResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		// should not be a notification
		if response.ID.IsNil() {
			return nil, fmt.Errorf("response should contain RPC id: %v", response)
		}

		return &response, nil

	case "text/event-stream":
		// Server is using SSE for streaming responses
		return c.handleSSEResponse(ctx, resp.Body, false)

	default:
		return nil, fmt.Errorf("unexpected content type: %s", resp.Header.Get("Content-Type"))
	}
}

func (c *StreamableHTTP) sendHTTP(
	ctx context.Context,
	method string,
	body io.Reader,
	acceptType string,
	header http.Header,
) (resp *http.Response, err error) {
	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, method, c.serverURL.String(), body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// request headers
	if header != nil {
		req.Header = header
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", acceptType)
	sessionID := c.sessionID.Load().(string)
	if sessionID != "" {
		req.Header.Set(HeaderKeySessionID, sessionID)
	}
	// Set protocol version header if negotiated
	if v := c.protocolVersion.Load(); v != nil {
		if version, ok := v.(string); ok && version != "" {
			req.Header.Set(HeaderKeyProtocolVersion, version)
		}
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	// Set custom Host header if provided
	if c.host != "" {
		req.Host = c.host
	}

	// Add OAuth authorization if configured
	if c.oauthHandler != nil {
		authHeader, err := c.oauthHandler.GetAuthorizationHeader(ctx)
		if err != nil {
			// If we get an authorization error, return a specific error that can be handled by the client
			if errors.Is(err, ErrOAuthAuthorizationRequired) {
				return nil, &OAuthAuthorizationRequiredError{
					Handler: c.oauthHandler,
					AuthorizationRequiredError: AuthorizationRequiredError{
						ResourceMetadataURL: "", // No response available in this code path
					},
				}
			}
			return nil, fmt.Errorf("failed to get authorization header: %w", err)
		}
		req.Header.Set("Authorization", authHeader)
	}

	if c.headerFunc != nil {
		for k, v := range c.headerFunc(ctx) {
			req.Header.Set(k, v)
		}
	}

	// Send request
	resp, err = c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// universal handling for session terminated
	if resp.StatusCode == http.StatusNotFound {
		c.sessionID.CompareAndSwap(sessionID, "")
		return nil, ErrSessionTerminated
	}

	return resp, nil
}

// handleSSEResponse processes an SSE stream for a specific request.
// It returns the final result for the request once received, or an error.
// If ignoreResponse is true, it won't return when a response messge is received. This is for continuous listening.
func (c *StreamableHTTP) handleSSEResponse(ctx context.Context, reader io.ReadCloser, ignoreResponse bool) (*JSONRPCResponse, error) {
	// Create a channel for this specific request
	responseChan := make(chan *JSONRPCResponse, 1)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start a goroutine to process the SSE stream
	go func() {
		// Ensure this goroutine respects the context
		defer close(responseChan)

		c.readSSE(ctx, reader, func(event, data string) {
			// Try to unmarshal as a response first
			var message JSONRPCResponse
			if err := json.Unmarshal([]byte(data), &message); err != nil {
				c.logger.Infof("failed to unmarshal message (non-fatal): %v", err, "message", data)
				return
			}

			// Handle notification
			if message.ID.IsNil() {
				var notification mcp.JSONRPCNotification
				if err := json.Unmarshal([]byte(data), &notification); err != nil {
					c.logger.Errorf("failed to unmarshal notification: %v", err)
					return
				}
				c.notifyMu.RLock()
				if c.notificationHandler != nil {
					c.notificationHandler(notification)
				}
				c.notifyMu.RUnlock()
				return
			}

			// Check if this is actually a request from the server by looking for method field
			var rawMessage map[string]json.RawMessage
			if err := json.Unmarshal([]byte(data), &rawMessage); err == nil {
				if _, hasMethod := rawMessage["method"]; hasMethod && !message.ID.IsNil() {
					var request JSONRPCRequest
					if err := json.Unmarshal([]byte(data), &request); err == nil {
						// This is a request from the server
						c.handleIncomingRequest(ctx, request)
						return
					}
				}
			}

			if !ignoreResponse {
				responseChan <- &message
			}
		})
	}()

	// Wait for the response or context cancellation
	select {
	case response := <-responseChan:
		if response == nil {
			return nil, fmt.Errorf("unexpected nil response")
		}
		return response, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// readSSE reads the SSE stream(reader) and calls the handler for each event and data pair.
// It will end when the reader is closed (or the context is done).
//
// A background goroutine closes the reader when ctx is cancelled, which unblocks
// any in-progress ReadString call. This is necessary because ReadString is blocking
// I/O that does not respect context cancellation on its own.
func (c *StreamableHTTP) readSSE(ctx context.Context, reader io.ReadCloser, handler func(event, data string)) {
	// Close the reader when context is cancelled to interrupt blocking reads.
	// This ensures ReadString returns immediately with an error instead of
	// blocking indefinitely when the SSE stream is open but idle.
	go func() {
		<-ctx.Done()
		reader.Close()
	}()

	br := bufio.NewReader(reader)
	var event, data string

	for {
		line, err := br.ReadString('\n')
		if err != nil {
			// Context was cancelled — reader was closed by the goroutine above.
			if ctx.Err() != nil {
				return
			}
			if err == io.EOF {
				// Process any pending event before exit
				if data != "" {
					if event == "" {
						event = "message"
					}
					handler(event, data)
				}
				return
			}
			c.logger.Errorf("SSE stream error: %v", err)
			return
		}

		// Remove only newline markers
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			// Empty line means end of event
			if data != "" {
				if event == "" {
					event = "message"
				}
				handler(event, data)
				event = ""
				data = ""
			}
			continue
		}

		if eventStr, ok := strings.CutPrefix(line, "event:"); ok {
			event = strings.TrimSpace(eventStr)
		} else if dataStr, ok := strings.CutPrefix(line, "data:"); ok {
			data = strings.TrimSpace(dataStr)
		}
	}
}

func (c *StreamableHTTP) SendNotification(ctx context.Context, notification mcp.JSONRPCNotification) error {
	// Marshal request
	requestBody, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	// Create HTTP request
	ctx, cancel := c.contextAwareOfClientClose(ctx)

	resp, err := c.sendHTTP(ctx, http.MethodPost, bytes.NewReader(requestBody), "application/json, text/event-stream", nil)
	if err != nil {
		cancel()
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { cancel(); resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusNoContent:
		return nil
	case http.StatusUnauthorized:
		// Extract discovered metadata URL per RFC9728
		metadataURL := extractResourceMetadataURL(resp.Header.Values("WWW-Authenticate"))

		// Feed discovered URL back to OAuthHandler so next auth attempt uses it.
		// HandleUnauthorizedResponse applies RFC 9728 origin validation — a
		// compromised resource advertising a cross-origin PRM URL is ignored.
		if c.oauthHandler != nil {
			c.oauthHandler.HandleUnauthorizedResponse(resp)
		}

		// If OAuth handler exists, return OAuth-specific error
		if c.oauthHandler != nil {
			return &OAuthAuthorizationRequiredError{
				Handler: c.oauthHandler,
				AuthorizationRequiredError: AuthorizationRequiredError{
					ResourceMetadataURL: metadataURL,
				},
			}
		}

		// No OAuth handler, return base authorization error
		return &AuthorizationRequiredError{
			ResourceMetadataURL: metadataURL,
		}
	default:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf(
			"notification failed with status %d: %s",
			resp.StatusCode,
			body,
		)
	}
}

func (c *StreamableHTTP) SetNotificationHandler(handler func(mcp.JSONRPCNotification)) {
	c.notifyMu.Lock()
	defer c.notifyMu.Unlock()
	c.notificationHandler = handler
}

// SetRequestHandler sets the handler for incoming requests from the server.
func (c *StreamableHTTP) SetRequestHandler(handler RequestHandler) {
	c.requestMu.Lock()
	defer c.requestMu.Unlock()
	c.requestHandler = handler
}

func (c *StreamableHTTP) GetSessionId() string {
	return c.sessionID.Load().(string)
}

// GetOAuthHandler returns the OAuth handler if configured
func (c *StreamableHTTP) GetOAuthHandler() *OAuthHandler {
	return c.oauthHandler
}

// IsOAuthEnabled returns true if OAuth is enabled
func (c *StreamableHTTP) IsOAuthEnabled() bool {
	return c.oauthHandler != nil
}

func (c *StreamableHTTP) listenForever(ctx context.Context) {
	c.logger.Infof("listening to server forever")
	for {
		// Use the original context for continuous listening - no per-iteration timeout
		// The SSE connection itself will detect disconnections via the underlying HTTP transport,
		// and the context cancellation will propagate from the parent to stop listening gracefully.
		// We don't add an artificial timeout here because:
		// 1. Persistent SSE connections are meant to stay open indefinitely
		// 2. Network-level timeouts and keep-alives handle connection health
		// 3. Context cancellation (user-initiated or system shutdown) provides clean shutdown
		err := c.createGETConnectionToServer(ctx)
		if errors.Is(err, ErrGetMethodNotAllowed) {
			// server does not support listening
			c.logger.Errorf("server does not support listening")
			return
		}

		select {
		case <-ctx.Done():
			return
		default:
		}

		if err != nil {
			c.logger.Errorf("failed to listen to server. retry in 1 second: %v", err)
		}

		// Use context-aware sleep
		select {
		case <-time.After(retryInterval):
		case <-ctx.Done():
			return
		}
	}
}

var (
	ErrSessionTerminated   = fmt.Errorf("session terminated (404). need to re-initialize")
	ErrGetMethodNotAllowed = fmt.Errorf("GET method not allowed")
	ErrUnauthorized        = fmt.Errorf("unauthorized (401)")
	ErrLegacySSEServer     = fmt.Errorf("server returned 4xx for initialize POST, likely a legacy SSE server")

	retryInterval = 1 * time.Second // a variable is convenient for testing
)

func (c *StreamableHTTP) createGETConnectionToServer(ctx context.Context) error {
	resp, err := c.sendHTTP(ctx, http.MethodGet, nil, "text/event-stream", nil)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	// Cancel the context before closing the body to prevent HTTP/2 drain hangs,
	// matching the pattern used in SendRequest and SendNotification.
	defer func() { resp.Body.Close() }()

	// Check if we got an error response
	if resp.StatusCode == http.StatusMethodNotAllowed {
		return ErrGetMethodNotAllowed
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, body)
	}

	// handle SSE response
	contentType := resp.Header.Get("Content-Type")
	if contentType != "text/event-stream" {
		return fmt.Errorf("unexpected content type: %s", contentType)
	}

	// When ignoreResponse is true, the function will never return expect context is done.
	// NOTICE: Due to the ambiguity of the specification, other SDKs may use the GET connection to transfer the response
	// messages. To be more compatible, we should handle this response, however, as the transport layer is message-based,
	// currently, there is no convenient way to handle this response.
	// So we ignore the response here. It's not a bug, but may be not compatible with other SDKs.
	_, err = c.handleSSEResponse(ctx, resp.Body, true)
	if err != nil {
		return fmt.Errorf("failed to handle SSE response: %w", err)
	}

	return nil
}

// handleIncomingRequest processes requests from the server (like sampling requests)
func (c *StreamableHTTP) handleIncomingRequest(ctx context.Context, request JSONRPCRequest) {
	c.requestMu.RLock()
	handler := c.requestHandler
	c.requestMu.RUnlock()

	if handler == nil {
		c.logger.Errorf("received request from server but no handler set: %s", request.Method)
		// Send method not found error
		errorResponse := NewJSONRPCErrorResponse(
			request.ID,
			mcp.METHOD_NOT_FOUND,
			fmt.Sprintf("no handler configured for method: %s", request.Method),
			nil,
		)
		c.sendResponseToServer(ctx, errorResponse)
		return
	}

	// Handle the request in a goroutine to avoid blocking the SSE reader
	go func() {
		// Create a new context with timeout for request handling, respecting parent context
		requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		response, err := handler(requestCtx, request)
		if err != nil {
			c.logger.Errorf("error handling request %s: %v", request.Method, err)

			// Determine appropriate JSON-RPC error code based on error type
			var errorCode int
			var errorMessage string

			// Check for specific sampling-related errors
			if errors.Is(err, context.Canceled) {
				errorCode = mcp.REQUEST_INTERRUPTED
				errorMessage = "request was cancelled"
			} else if errors.Is(err, context.DeadlineExceeded) {
				errorCode = mcp.REQUEST_INTERRUPTED
				errorMessage = "request timed out"
			} else {
				// Generic error cases
				switch request.Method {
				case string(mcp.MethodSamplingCreateMessage):
					errorCode = mcp.INTERNAL_ERROR
					errorMessage = fmt.Sprintf("sampling request failed: %v", err)
				default:
					errorCode = mcp.INTERNAL_ERROR
					errorMessage = err.Error()
				}
			}

			// Send error response
			errorResponse := NewJSONRPCErrorResponse(request.ID, errorCode, errorMessage, nil)
			c.sendResponseToServer(requestCtx, errorResponse)
			return
		}

		if response != nil {
			c.sendResponseToServer(requestCtx, response)
		}
	}()
}

// sendResponseToServer sends a response back to the server via HTTP POST
func (c *StreamableHTTP) sendResponseToServer(ctx context.Context, response *JSONRPCResponse) {
	if response == nil {
		c.logger.Errorf("cannot send nil response to server")
		return
	}

	responseBody, err := json.Marshal(response)
	if err != nil {
		c.logger.Errorf("failed to marshal response: %v", err)
		return
	}

	ctx, cancel := c.contextAwareOfClientClose(ctx)

	resp, err := c.sendHTTP(ctx, http.MethodPost, bytes.NewReader(responseBody), "application/json, text/event-stream", nil)
	if err != nil {
		cancel()
		c.logger.Errorf("failed to send response to server: %v", err)
		return
	}
	defer func() { cancel(); resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		c.logger.Errorf("server rejected response with status %d: %s", resp.StatusCode, body)
	}
}

func (c *StreamableHTTP) contextAwareOfClientClose(ctx context.Context) (context.Context, context.CancelFunc) {
	newCtx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-c.closed:
			cancel()
		case <-newCtx.Done():
			// The original context was canceled
			cancel()
		}
	}()
	return newCtx, cancel
}
