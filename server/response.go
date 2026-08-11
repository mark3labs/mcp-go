package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"

	"github.com/mark3labs/mcp-go/mcp"
)

// defaultCacheTTLMs is the cache freshness hint applied to list and read
// results when the server does not configure one. Zero means "always
// revalidate", which preserves the polling behaviour of earlier protocol
// versions while still satisfying the SEP-2549 requirement that the field be
// present.
const defaultCacheTTLMs int64 = 0

// cacheHints describes the SEP-2549 caching hints a server advertises on a
// list or read result.
type cacheHints struct {
	ttlMs int64
	scope mcp.CacheScope
}

// decorateResponse applies the result metadata required by protocol version
// 2026-07-28 to an outgoing response.
//
// For modern requests it stamps resultType, the server identity in _meta, and
// the SEP-2549 caching hints. For legacy requests it is a no-op, so responses
// remain byte-identical to earlier releases.
func (s *MCPServer) decorateResponse(
	_ context.Context,
	info *RequestProtocolInfo,
	method mcp.MCPMethod,
	resp mcp.JSONRPCMessage,
) mcp.JSONRPCMessage {
	if info == nil || !info.Modern || resp == nil {
		return resp
	}

	response, ok := resp.(mcp.JSONRPCResponse)
	if !ok || response.Result == nil {
		return resp
	}

	decorated, ok := s.decorateResult(response.Result, method)
	if !ok {
		return resp
	}
	response.Result = decorated
	return response
}

// decorateResult stamps the modern result metadata onto a result value.
//
// The generated dispatcher stores results by value, so the value is copied
// into an addressable location before the pointer-receiver decoration
// interfaces are applied.
func (s *MCPServer) decorateResult(result any, method mcp.MCPMethod) (any, bool) {
	value := reflect.ValueOf(result)
	if !value.IsValid() {
		return nil, false
	}

	// Work on an addressable copy so that pointer-receiver methods promoted
	// from the embedded mcp.Result are reachable.
	byValue := value.Kind() != reflect.Pointer
	pointer := value
	if byValue {
		pointer = reflect.New(value.Type())
		pointer.Elem().Set(value)
	} else if value.IsNil() {
		return nil, false
	}

	metadata, ok := pointer.Interface().(mcp.ResultMetadata)
	if !ok {
		return nil, false
	}

	// resultType is required from 2026-07-28 onward. A handler that already
	// set it - to signal input_required, for example - keeps its value.
	if metadata.GetResultType() == "" {
		metadata.SetResultType(mcp.ResultTypeComplete)
	}

	// Servers SHOULD identify themselves in every result.
	if meta := metadata.EnsureResultMeta(); meta != nil && meta.ServerInfo() == nil {
		meta.SetServerInfo(s.serverImplementation())
	}

	// ttlMs and cacheScope are required on list and read results.
	if methodReturnsCacheableResult(method) {
		if cacheable, ok := pointer.Interface().(mcp.CacheHintSetter); ok {
			applyDefaultCacheHints(cacheable, s.cacheHintsFor(method))
		}
	}

	if byValue {
		return pointer.Elem().Interface(), true
	}
	return result, true
}

// cacheHintsFor returns the caching hints configured for the given method,
// falling back to the server-wide default.
func (s *MCPServer) cacheHintsFor(method mcp.MCPMethod) cacheHints {
	s.capabilitiesMu.RLock()
	defer s.capabilitiesMu.RUnlock()

	if s.cacheHints != nil {
		if configured, ok := s.cacheHints[method]; ok {
			return configured
		}
		if configured, ok := s.cacheHints[""]; ok {
			return configured
		}
	}
	return cacheHints{ttlMs: defaultCacheTTLMs, scope: mcp.CacheScopePublic}
}

// applyDefaultCacheHints populates caching hints on a result that has not
// already set them.
func applyDefaultCacheHints(result mcp.CacheHintSetter, hints cacheHints) {
	if cacheable, ok := result.(interface{ TTL() (int64, bool) }); ok {
		if _, alreadySet := cacheable.TTL(); alreadySet {
			return
		}
	}
	result.SetCacheHints(hints.ttlMs, hints.scope)
}

// methodReturnsCacheableResult reports whether protocol version 2026-07-28
// requires ttlMs and cacheScope on the result of the given method.
func methodReturnsCacheableResult(method mcp.MCPMethod) bool {
	switch method {
	case mcp.MethodToolsList,
		mcp.MethodPromptsList,
		mcp.MethodResourcesList,
		mcp.MethodResourcesTemplatesList,
		mcp.MethodResourcesRead,
		mcp.MethodServerDiscover:
		return true
	default:
		return false
	}
}

// errorResponseForProtocolError converts a protocol-level validation failure
// into the JSON-RPC error the specification prescribes.
func errorResponseForProtocolError(id any, err error) mcp.JSONRPCMessage {
	var unsupported mcp.UnsupportedProtocolVersionError
	if errors.As(err, &unsupported) {
		response := unsupported.JSONRPCError()
		response.ID = mcp.NewRequestId(id)
		return response
	}

	var mismatch mcp.HeaderMismatchError
	if errors.As(err, &mismatch) {
		return createErrorResponse(id, mcp.HEADER_MISMATCH, mismatch.Error())
	}

	var missing mcp.MissingRequiredClientCapabilityError
	if errors.As(err, &missing) {
		return createErrorResponse(id, mcp.MISSING_REQUIRED_CLIENT_CAPABILITY, missing.Error())
	}

	return createErrorResponse(id, mcp.INVALID_PARAMS, err.Error())
}

// validateStandardHeadersForMessage checks the Mcp-Method and Mcp-Name headers
// against the JSON-RPC message body, as required from protocol version
// 2026-07-28 (SEP-2243).
//
// Requests that did not arrive over HTTP carry no headers, so validation is
// skipped: the header contract binds the Streamable HTTP transport only.
func validateStandardHeadersForMessage(
	headers http.Header,
	protocolVersion string,
	method mcp.MCPMethod,
	message json.RawMessage,
) error {
	if len(headers) == 0 || headers.Get(mcp.HeaderProtocolVersion) == "" {
		return nil
	}

	var wrapper struct {
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(message, &wrapper); err != nil {
		return nil
	}

	return mcp.ValidateStandardHeaders(headers.Get, protocolVersion, method, wrapper.Params)
}
