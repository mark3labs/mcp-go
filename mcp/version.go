package mcp

import (
	"slices"
	"time"
)

// Protocol version constants for every MCP revision this SDK understands.
//
// Protocol versions are ISO-8601 dates, so they may be compared
// lexicographically: a simple string comparison against
// [ProtocolVersion20260728] is enough to distinguish the "modern"
// (stateless, per-request metadata) era from the "legacy"
// (initialize-handshake) era.
const (
	// ProtocolVersion20260728 introduced the stateless protocol core: no
	// initialize handshake, no sessions, server/discover, subscriptions/listen,
	// and multi round-trip requests (SEP-2575, SEP-2322, SEP-2243, SEP-2549).
	ProtocolVersion20260728 = "2026-07-28"

	// ProtocolVersion20251125 is the last revision that used the
	// initialize/initialized handshake.
	ProtocolVersion20251125 = "2025-11-25"

	// ProtocolVersion20250618 added the MCP-Protocol-Version header.
	ProtocolVersion20250618 = "2025-06-18"

	// ProtocolVersion20250326 introduced the Streamable HTTP transport.
	ProtocolVersion20250326 = "2025-03-26"

	// ProtocolVersion20241105 is the original revision, using the deprecated
	// HTTP+SSE transport.
	ProtocolVersion20241105 = "2024-11-05"
)

// LATEST_PROTOCOL_VERSION is the most recent version of the MCP protocol
// supported by this SDK.
const LATEST_PROTOCOL_VERSION = ProtocolVersion20260728

// LATEST_LEGACY_PROTOCOL_VERSION is the most recent protocol version that
// still uses the initialize/initialized handshake. It is the highest version
// that can be negotiated through initialize.
const LATEST_LEGACY_PROTOCOL_VERSION = ProtocolVersion20251125

// ValidProtocolVersions lists all known valid MCP protocol versions, in
// descending order (newest first).
var ValidProtocolVersions = []string{
	ProtocolVersion20260728,
	ProtocolVersion20251125,
	ProtocolVersion20250618,
	ProtocolVersion20250326,
	ProtocolVersion20241105,
}

// JSONRPC_VERSION is the version of JSON-RPC used by MCP.
const JSONRPC_VERSION = "2.0"

// IsModernProtocol reports whether version uses the stateless protocol core
// introduced in 2026-07-28, where requests carry their protocol version,
// client identity, and capabilities in _meta rather than establishing a
// session through initialize.
//
// Version must be a valid ISO-8601 calendar date (YYYY-MM-DD); malformed
// strings return false. Valid future dates at or after 2026-07-28 return true,
// so that this SDK degrades gracefully rather than rejecting them outright.
func IsModernProtocol(version string) bool {
	if len(version) != len(time.DateOnly) {
		return false
	}
	if _, err := time.Parse(time.DateOnly, version); err != nil {
		return false
	}
	return version >= ProtocolVersion20260728
}

// IsValidProtocolVersion reports whether version is known to this SDK.
func IsValidProtocolVersion(version string) bool {
	return slices.Contains(ValidProtocolVersions, version)
}

// NegotiateMutuallySupportedVersion returns the highest protocol version
// present in both [ValidProtocolVersions] and supported, or "" when the two
// sets are disjoint.
func NegotiateMutuallySupportedVersion(supported []string) string {
	for _, version := range ValidProtocolVersions {
		if slices.Contains(supported, version) {
			return version
		}
	}
	return ""
}

// NegotiateLegacyVersion returns the protocol version a server should report
// in an InitializeResult, given the version requested by the client.
//
// The initialize handshake was removed in 2026-07-28, so it can never
// negotiate that version or later: the result is always capped at
// [LATEST_LEGACY_PROTOCOL_VERSION]. Clients reach the modern protocol through
// server/discover instead.
func NegotiateLegacyVersion(clientVersion string) string {
	// For backwards compatibility, if the server does not receive an
	// MCP-Protocol-Version header, and has no other way to identify the
	// version, it SHOULD assume protocol version 2025-03-26.
	if clientVersion == "" {
		clientVersion = ProtocolVersion20250326
	}
	if IsValidProtocolVersion(clientVersion) && !IsModernProtocol(clientVersion) {
		return clientVersion
	}
	return LATEST_LEGACY_PROTOCOL_VERSION
}

// LegacyProtocolVersions returns the subset of [ValidProtocolVersions] that
// use the initialize handshake.
func LegacyProtocolVersions() []string {
	out := make([]string, 0, len(ValidProtocolVersions))
	for _, version := range ValidProtocolVersions {
		if !IsModernProtocol(version) {
			out = append(out, version)
		}
	}
	return out
}
