package client

import (
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func malformedDiscoveryResults() []struct{ name, result string } {
	return []struct{ name, result string }{
		{"null result", `null`},
		{"empty object", `{}`},
		{"wrong method result", `{"tools":[]}`},
		{"missing versions", `{"capabilities":{}}`},
		{"null versions", `{"supportedVersions":null,"capabilities":{}}`},
		{"string versions", `{"supportedVersions":"2026-07-28","capabilities":{}}`},
		{"null version entry", `{"supportedVersions":[null],"capabilities":{}}`},
		{"missing capabilities", `{"supportedVersions":["2026-07-28"]}`},
		{"null capabilities", `{"supportedVersions":["2026-07-28"],"capabilities":null}`},
		{"array capabilities", `{"supportedVersions":["2026-07-28"],"capabilities":[]}`},
		{"null metadata", `{"supportedVersions":["2026-07-28"],"capabilities":{},"_meta":null}`},
		{"null identity", `{"supportedVersions":["2026-07-28"],"capabilities":{},"_meta":{"io.modelcontextprotocol/serverInfo":null}}`},
		{"missing identity name", `{"supportedVersions":["2026-07-28"],"capabilities":{},"_meta":{"io.modelcontextprotocol/serverInfo":{"version":"1"}}}`},
		{"missing identity version", `{"supportedVersions":["2026-07-28"],"capabilities":{},"_meta":{"io.modelcontextprotocol/serverInfo":{"name":"server"}}}`},
		{"null identity name", `{"supportedVersions":["2026-07-28"],"capabilities":{},"_meta":{"io.modelcontextprotocol/serverInfo":{"name":null,"version":"1"}}}`},
	}
}

func TestDiscoverRejectsMalformedResults(t *testing.T) {
	for _, tt := range malformedDiscoveryResults() {
		t.Run(tt.name, func(t *testing.T) {
			server := &versionRejectingServer{discoverResult: json.RawMessage(tt.result)}
			c := newClientAgainst(t, server)

			result, err := c.Discover(t.Context(), mcp.DiscoverRequest{})
			assert.Error(t, err)
			assert.Nil(t, result)
		})
	}
}

func TestInitializeFallsBackOnMalformedFirstDiscovery(t *testing.T) {
	for _, tt := range malformedDiscoveryResults() {
		t.Run(tt.name, func(t *testing.T) {
			server := &versionRejectingServer{discoverResult: json.RawMessage(tt.result)}
			c := newClientAgainst(t, server)

			result, err := c.Initialize(t.Context(), mcp.InitializeRequest{})
			require.NoError(t, err)
			assert.Equal(t, mcp.LATEST_LEGACY_PROTOCOL_VERSION, result.ProtocolVersion)
			assert.True(t, c.IsInitialized())
			assert.Equal(t, mcp.LATEST_LEGACY_PROTOCOL_VERSION, c.ProtocolVersion())
			assert.Equal(t, []string{
				"server/discover " + mcp.ProtocolVersion20260728,
				"initialize " + mcp.LATEST_LEGACY_PROTOCOL_VERSION,
			}, server.seen())
		})
	}
}

func TestInitializeDoesNotFallBackOnMalformedModernRetry(t *testing.T) {
	for _, tt := range malformedDiscoveryResults() {
		t.Run(tt.name, func(t *testing.T) {
			server := &versionRejectingServer{
				supported:  []string{mcp.ProtocolVersion20260728, mcp.LATEST_LEGACY_PROTOCOL_VERSION},
				rejections: 1, discoverResult: json.RawMessage(tt.result),
			}
			c := newClientAgainst(t, server, WithProtocolVersion("2099-01-01"))

			result, err := c.Initialize(t.Context(), mcp.InitializeRequest{})
			assert.Error(t, err)
			assert.Nil(t, result)
			assert.False(t, c.IsInitialized())
			assert.Empty(t, c.ProtocolVersion())
			assert.Equal(t, []string{
				"server/discover 2099-01-01",
				"server/discover " + mcp.ProtocolVersion20260728,
			}, server.seen())
		})
	}
}

func TestDiscoverAcceptsEraIdentifyingFields(t *testing.T) {
	tests := []struct{ name, result string }{
		{"empty versions", `{"supportedVersions":[],"capabilities":{}}`},
		{"unknown version", `{"supportedVersions":["2099-01-01"],"capabilities":{}}`},
		{"legacy version", `{"supportedVersions":["2025-11-25"],"capabilities":{}}`},
		{"omitted identity and cache hints", `{"supportedVersions":["2026-07-28"],"capabilities":{}}`},
		{"empty identity strings", `{"supportedVersions":["2026-07-28"],"capabilities":{},"_meta":{"io.modelcontextprotocol/serverInfo":{"name":"","version":""}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &versionRejectingServer{discoverResult: json.RawMessage(tt.result)}
			c := newClientAgainst(t, server)

			result, err := c.Discover(t.Context(), mcp.DiscoverRequest{})
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.NotNil(t, result.SupportedVersions)
			assert.True(t, result.ResultType.IsComplete())
		})
	}
}

func TestInitializeAcceptsMinimalModernDiscovery(t *testing.T) {
	server := &versionRejectingServer{
		discoverResult: json.RawMessage(`{"supportedVersions":["2026-07-28"],"capabilities":{}}`),
	}
	c := newClientAgainst(t, server)

	result, err := c.Initialize(t.Context(), mcp.InitializeRequest{})
	require.NoError(t, err)
	assert.Equal(t, mcp.ProtocolVersion20260728, result.ProtocolVersion)
	assert.True(t, c.IsInitialized())
	assert.Equal(t, []string{"server/discover " + mcp.ProtocolVersion20260728}, server.seen())
}
