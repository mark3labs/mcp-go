package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// defaultDiscoverProbeTimeout bounds the server/discover probe sent during
// Initialize. Servers predating protocol version 2026-07-28 answer an unknown
// method with MethodNotFound, but some ignore it entirely, which on stdio
// would otherwise block the handshake indefinitely.
const defaultDiscoverProbeTimeout = 5 * time.Second

// Protocol version 2026-07-28 removed the initialize handshake: a client
// declares its protocol version, identity, and capabilities in the _meta of
// every request, and may learn a server's capabilities up front through the
// server/discover RPC (SEP-2575).
//
// This client speaks both eras. Connect probes with server/discover; if that
// fails with anything other than a recognized modern error, it falls back to
// initialize and stays on the legacy revision for the rest of the connection.

// isModern reports whether the client is speaking the stateless protocol core.
func (c *Client) isModern() bool {
	return mcp.IsModernProtocol(c.protocolVersion)
}

// legacyRequiringTransport is implemented by transports that have been
// configured in a way only protocol versions before 2026-07-28 can satisfy,
// such as a Streamable HTTP transport using continuous listening.
type legacyRequiringTransport interface {
	RequiresLegacyProtocol() bool
}

// transportRequiresLegacyProtocol reports whether the transport rules out the
// stateless protocol core.
func (c *Client) transportRequiresLegacyProtocol() bool {
	legacy, ok := c.transport.(legacyRequiringTransport)
	return ok && legacy.RequiresLegacyProtocol()
}

// ProtocolVersion returns the protocol version in effect for this client, or
// "" before the connection has been established.
func (c *Client) ProtocolVersion() string {
	return c.protocolVersion
}

// effectiveCapabilities returns the client capabilities to declare, merging
// the configured set with the capabilities implied by the registered handlers.
func (c *Client) effectiveCapabilities() mcp.ClientCapabilities {
	capabilities := c.clientCapabilities
	if c.samplingHandler != nil && capabilities.Sampling == nil {
		capabilities.Sampling = &mcp.SamplingCapability{}
	}
	if c.rootsHandler != nil && capabilities.Roots == nil {
		capabilities.Roots = &struct {
			ListChanged bool `json:"listChanged,omitempty"`
		}{ListChanged: true}
	}
	if c.elicitationHandler != nil && capabilities.Elicitation == nil {
		capabilities.Elicitation = &mcp.ElicitationCapability{}
	}
	return capabilities
}

// applyRequestMeta merges the per-request protocol metadata that protocol
// version 2026-07-28 requires into a request's params.
//
// It is a no-op for legacy connections, whose protocol version, identity, and
// capabilities were fixed by the initialize handshake.
func (c *Client) applyRequestMeta(params any) any {
	if !c.isModern() {
		return params
	}

	fields := map[string]json.RawMessage{}
	if params != nil {
		encoded, err := json.Marshal(params)
		if err != nil {
			return params
		}
		if err := json.Unmarshal(encoded, &fields); err != nil {
			// Params that are not a JSON object cannot carry _meta.
			return params
		}
	}

	meta := map[string]any{}
	if raw, ok := fields["_meta"]; ok {
		_ = json.Unmarshal(raw, &meta)
	}
	meta[mcp.MetaKeyProtocolVersion] = c.protocolVersion
	meta[mcp.MetaKeyClientCapabilities] = c.effectiveCapabilities()
	if c.clientInfo.Name != "" {
		meta[mcp.MetaKeyClientInfo] = c.clientInfo
	}
	if level := c.currentLogLevel(); level != "" {
		meta[mcp.MetaKeyLogLevel] = level
	}

	encodedMeta, err := json.Marshal(meta)
	if err != nil {
		return params
	}
	fields["_meta"] = encodedMeta
	return fields
}

// currentLogLevel returns the per-request log level to declare, if any.
func (c *Client) currentLogLevel() mcp.LoggingLevel {
	c.logLevelMu.RLock()
	defer c.logLevelMu.RUnlock()
	return c.logLevel
}

// applyStandardHeaders adds the Mcp-Method and Mcp-Name headers that protocol
// version 2026-07-28 requires on Streamable HTTP requests (SEP-2243).
//
// Transports other than Streamable HTTP ignore them, so they are set
// unconditionally for modern connections.
func (c *Client) applyStandardHeaders(header http.Header, method string, params any) http.Header {
	if !c.isModern() {
		return header
	}

	encoded, err := json.Marshal(params)
	if err != nil {
		return header
	}

	standard := mcp.StandardHeaders(c.protocolVersion, mcp.MCPMethod(method), encoded)
	if len(standard) == 0 {
		return header
	}

	if header == nil {
		header = make(http.Header, len(standard))
	} else {
		header = header.Clone()
	}
	for name, value := range standard {
		header.Set(name, value)
	}

	// Tool parameters annotated with x-mcp-header must be mirrored into
	// Mcp-Param-* headers so gateways can route on them.
	if mcp.MCPMethod(method) == mcp.MethodToolsCall {
		if tool := c.toolForCall(encoded); tool != nil {
			for name, value := range mcp.GenerateParamHeaders(tool, encoded) {
				header.Set(name, value)
			}
		}
	}

	return header
}

// toolForCall returns the cached tool definition named by a tools/call
// request, or nil when the client has not listed tools yet.
func (c *Client) toolForCall(params json.RawMessage) *mcp.Tool {
	var call struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(params, &call); err != nil || call.Name == "" {
		return nil
	}
	c.toolsMu.RLock()
	defer c.toolsMu.RUnlock()
	tool, ok := c.knownTools[call.Name]
	if !ok {
		return nil
	}
	return &tool
}

// rememberTools caches tool definitions so that subsequent tools/call requests
// can mirror x-mcp-header annotated parameters into HTTP headers.
func (c *Client) rememberTools(tools []mcp.Tool) {
	if len(tools) == 0 {
		return
	}
	c.toolsMu.Lock()
	defer c.toolsMu.Unlock()
	if c.knownTools == nil {
		c.knownTools = make(map[string]mcp.Tool, len(tools))
	}
	for _, tool := range tools {
		c.knownTools[tool.Name] = tool
	}
}

// Discover asks the server which protocol versions it supports, what its
// capabilities are, and who it is, without performing the legacy initialize
// handshake (SEP-2575).
//
// Servers implementing protocol version 2026-07-28 or later must support this
// method. Against an older server it fails, which is how [Client.Initialize]
// detects that it must fall back to the handshake.
func (c *Client) Discover(ctx context.Context, request mcp.DiscoverRequest) (*mcp.DiscoverResult, error) {
	response, err := c.sendRequest(
		ctx,
		string(mcp.MethodServerDiscover),
		c.applyRequestMeta(request.Params),
		outboundHeader(request.Header, request.Method),
	)
	if err != nil {
		return nil, err
	}

	var result mcp.DiscoverResult
	if err := json.Unmarshal(*response, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal discover result: %w", err)
	}
	return &result, nil
}

// negotiateModern attempts to establish a modern, stateless connection.
//
// It sends server/discover at the client's preferred version. If the server
// rejects that version but names the ones it does support, the request is
// retried once at the highest mutually supported modern version. Any other
// failure means the server is not modern, and the caller falls back to the
// initialize handshake.
//
// The probe is bounded by a timeout: a well-behaved JSON-RPC server answers an
// unknown method with MethodNotFound, but a server predating this method may
// simply ignore it, which on stdio would otherwise block forever.
func (c *Client) negotiateModern(ctx context.Context, preferred string) (*mcp.DiscoverResult, error) {
	if preferred == "" {
		preferred = mcp.LATEST_PROTOCOL_VERSION
	}

	probeCtx := ctx
	if timeout := c.discoverProbeTimeout(); timeout > 0 {
		var cancel context.CancelFunc
		probeCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// The version travels in each request's _meta and, over HTTP, in the
	// Mcp-Protocol-Version header, which must agree. Both must therefore be in
	// place before the probe is sent; they are restored on failure.
	previous := c.protocolVersion
	c.applyNegotiatedVersion(preferred)

	for range 2 {
		result, err := c.Discover(probeCtx, mcp.DiscoverRequest{})
		if err == nil {
			return result, nil
		}

		// A recognized modern error identifies a modern server: renegotiate
		// rather than falling back.
		var unsupported mcp.UnsupportedProtocolVersionError
		if errors.As(err, &unsupported) && len(unsupported.Supported) > 0 {
			negotiated := mcp.NegotiateMutuallySupportedVersion(unsupported.Supported)
			if negotiated != "" && mcp.IsModernProtocol(negotiated) && negotiated != c.protocolVersion {
				c.applyNegotiatedVersion(negotiated)
				continue
			}
		}

		c.applyNegotiatedVersion(previous)
		return nil, err
	}

	c.applyNegotiatedVersion(previous)
	return nil, fmt.Errorf("server/discover: exhausted version negotiation attempts")
}

// discoverProbeTimeout returns how long to wait for a server/discover reply
// before concluding the server is legacy.
func (c *Client) discoverProbeTimeout() time.Duration {
	if c.discoverTimeout != 0 {
		return c.discoverTimeout
	}
	return defaultDiscoverProbeTimeout
}

// initializeResultFromDiscover renders a DiscoverResult in the shape of an
// InitializeResult, so that callers of [Client.Initialize] observe the same
// value regardless of which era the connection settled on.
func initializeResultFromDiscover(protocolVersion string, result *mcp.DiscoverResult) *mcp.InitializeResult {
	initialized := &mcp.InitializeResult{
		ProtocolVersion: protocolVersion,
		Capabilities:    result.Capabilities,
		Instructions:    result.Instructions,
	}
	if info := result.GetResultMeta().ServerInfo(); info != nil {
		initialized.ServerInfo = *info
	}
	return initialized
}
