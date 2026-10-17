package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsModernProtocol(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{"the revision that introduced the stateless core", ProtocolVersion20260728, true},
		{"the last handshake revision", ProtocolVersion20251125, false},
		{"an older revision", ProtocolVersion20241105, false},
		{"an unset version", "", false},
		// Versions are ISO dates, so an unknown future one sorts after
		// 2026-07-28 and is treated as modern rather than rejected outright.
		{"an unknown future revision", "2027-01-01", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsModernProtocol(tt.version))
		})
	}
}

func TestNegotiateLegacyVersion(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		want      string
	}{
		{"a supported legacy version is echoed", ProtocolVersion20250618, ProtocolVersion20250618},
		{"an absent version falls back to the header-less default", "", ProtocolVersion20250326},
		{"an unknown version yields the newest legacy one", "1999-01-01", LATEST_LEGACY_PROTOCOL_VERSION},
		// The handshake was removed in 2026-07-28, so it can never negotiate
		// that version or later.
		{"a modern version is capped", ProtocolVersion20260728, LATEST_LEGACY_PROTOCOL_VERSION},
		{"a future version is capped", "2030-01-01", LATEST_LEGACY_PROTOCOL_VERSION},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NegotiateLegacyVersion(tt.requested))
		})
	}
}

func TestNegotiateMutuallySupportedVersion(t *testing.T) {
	tests := []struct {
		name      string
		supported []string
		want      string
	}{
		{
			name:      "the newest common version wins",
			supported: []string{ProtocolVersion20250618, ProtocolVersion20251125},
			want:      ProtocolVersion20251125,
		},
		{
			name:      "a modern peer is preferred",
			supported: []string{ProtocolVersion20251125, ProtocolVersion20260728},
			want:      ProtocolVersion20260728,
		},
		{
			name:      "disjoint sets yield nothing",
			supported: []string{"1999-01-01"},
			want:      "",
		},
		{
			name:      "an empty list yields nothing",
			supported: nil,
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NegotiateMutuallySupportedVersion(tt.supported))
		})
	}
}

func TestLegacyProtocolVersions(t *testing.T) {
	legacy := LegacyProtocolVersions()

	require.NotEmpty(t, legacy)
	assert.Equal(t, LATEST_LEGACY_PROTOCOL_VERSION, legacy[0], "newest first")
	assert.NotContains(t, legacy, ProtocolVersion20260728)
	for _, version := range legacy {
		assert.False(t, IsModernProtocol(version))
	}
}

func TestResultTypeIsComplete(t *testing.T) {
	assert.True(t, ResultTypeComplete.IsComplete())
	assert.False(t, ResultTypeInputRequired.IsComplete())

	// Clients must treat a result from an earlier-protocol server, which omits
	// the field, as complete.
	assert.True(t, ResultType("").IsComplete())
}

func TestResultTypeOmittedForLegacyClients(t *testing.T) {
	// A result built without a result type must not put one on the wire, so
	// responses to clients using an earlier protocol version are unchanged.
	encoded, err := json.Marshal(Result{})
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(encoded))

	encoded, err = json.Marshal(Result{ResultType: ResultTypeComplete})
	require.NoError(t, err)
	assert.JSONEq(t, `{"resultType":"complete"}`, string(encoded))
}

func TestCacheableResultHints(t *testing.T) {
	var result CacheableResult

	_, ok := result.TTL()
	assert.False(t, ok, "an unset hint must be distinguishable from zero")

	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(encoded), "legacy results carry no caching hints")

	// An explicit zero means "always revalidate", and must survive the round
	// trip as a present field.
	result.SetCacheHints(0, CacheScopePublic)
	ttl, ok := result.TTL()
	require.True(t, ok)
	assert.Equal(t, int64(0), ttl)

	encoded, err = json.Marshal(result)
	require.NoError(t, err)
	assert.JSONEq(t, `{"ttlMs":0,"cacheScope":"public"}`, string(encoded))
}

func TestCacheableResultDefaultsToPrivateScope(t *testing.T) {
	// Fail closed: an unset scope must not publish an authorization-scoped
	// result to shared intermediaries.
	var result CacheableResult
	result.SetCacheHints(1000, "")
	assert.Equal(t, CacheScopePrivate, result.CacheScope)

	// An explicit public scope is honoured.
	var shared CacheableResult
	shared.SetCacheHints(1000, CacheScopePublic)
	assert.Equal(t, CacheScopePublic, shared.CacheScope)
}

func TestMetaTypedAccessors(t *testing.T) {
	meta := &Meta{}
	meta.SetProtocolVersion(ProtocolVersion20260728)
	meta.SetClientInfo(Implementation{Name: "client", Version: "1.0.0"})
	meta.SetClientCapabilities(ClientCapabilities{Elicitation: &ElicitationCapability{}})
	meta.SetLogLevel(LoggingLevelDebug)

	assert.Equal(t, ProtocolVersion20260728, meta.ProtocolVersion())
	require.NotNil(t, meta.ClientInfo())
	assert.Equal(t, "client", meta.ClientInfo().Name)
	require.NotNil(t, meta.ClientCapabilities())
	assert.NotNil(t, meta.ClientCapabilities().Elicitation)
	assert.Equal(t, LoggingLevelDebug, meta.LogLevel())
}

func TestMetaAccessorsSurviveTheWire(t *testing.T) {
	// Values constructed in process keep their Go type; values decoded from
	// JSON arrive as generic maps. Both must read back the same.
	original := &Meta{}
	original.SetProtocolVersion(ProtocolVersion20260728)
	original.SetClientInfo(Implementation{Name: "client", Version: "2.0.0"})
	original.SetClientCapabilities(ClientCapabilities{Sampling: &SamplingCapability{}})

	encoded, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded Meta
	require.NoError(t, json.Unmarshal(encoded, &decoded))

	assert.Equal(t, ProtocolVersion20260728, decoded.ProtocolVersion())
	require.NotNil(t, decoded.ClientInfo())
	assert.Equal(t, "client", decoded.ClientInfo().Name)
	assert.Equal(t, "2.0.0", decoded.ClientInfo().Version)
	require.NotNil(t, decoded.ClientCapabilities())
	assert.NotNil(t, decoded.ClientCapabilities().Sampling)
}

func TestMetaAccessorsOnNil(t *testing.T) {
	var meta *Meta

	assert.Empty(t, meta.ProtocolVersion())
	assert.Nil(t, meta.ClientInfo())
	assert.Nil(t, meta.ClientCapabilities())
	assert.Nil(t, meta.ServerInfo())
	assert.Empty(t, meta.LogLevel())
	assert.Nil(t, meta.SubscriptionID())
}

func TestUnsupportedProtocolVersionErrorCarriesTheSupportedList(t *testing.T) {
	err := UnsupportedProtocolVersionError{
		Version:   "2099-01-01",
		Supported: []string{ProtocolVersion20260728, ProtocolVersion20251125},
	}

	response := err.JSONRPCError()
	assert.Equal(t, UNSUPPORTED_PROTOCOL_VERSION, response.Error.Code)

	// Round-tripping through JSON must preserve the negotiation data, which is
	// what lets a client retry at a version the server accepts.
	encoded, err2 := json.Marshal(response.Error)
	require.NoError(t, err2)

	var details JSONRPCErrorDetails
	require.NoError(t, json.Unmarshal(encoded, &details))

	restored := details.AsError()
	var unsupported UnsupportedProtocolVersionError
	require.ErrorAs(t, restored, &unsupported)
	assert.Equal(t, "2099-01-01", unsupported.Version)
	assert.Equal(t, []string{ProtocolVersion20260728, ProtocolVersion20251125}, unsupported.Supported)
}

func TestErrorCodesMapToTypedErrors(t *testing.T) {
	tests := []struct {
		name  string
		code  int
		check func(error) bool
	}{
		{"header mismatch", HEADER_MISMATCH, IsHeaderMismatch},
		{"missing client capability", MISSING_REQUIRED_CLIENT_CAPABILITY, IsMissingRequiredClientCapability},
		{"unsupported protocol version", UNSUPPORTED_PROTOCOL_VERSION, IsUnsupportedProtocolVersion},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			details := JSONRPCErrorDetails{Code: tt.code, Message: "boom"}
			assert.True(t, tt.check(details.AsError()))
		})
	}
}
