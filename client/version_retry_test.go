package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// versionRejectingServer rejects server/discover with
// UnsupportedProtocolVersionError, listing supported, the first rejections
// times (every time when rejections is negative); with omitData the error
// carries no data at all. Otherwise it serves discover and the initialize
// handshake, answering initialize like a legacy server would. It records the
// method and requested version of every request.
type versionRejectingServer struct {
	supported      []string
	rejections     int
	omitData       bool
	retryError     bool
	discoverResult json.RawMessage

	mu       sync.Mutex
	rejected int
	requests []string
	headers  []string
}

func (s *versionRejectingServer) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

func (s *versionRejectingServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			ProtocolVersion string   `json:"protocolVersion"`
			Meta            mcp.Meta `json:"_meta"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &request); err != nil || request.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	version := request.Params.Meta.ProtocolVersion()
	if request.Method == string(mcp.MethodInitialize) {
		version = request.Params.ProtocolVersion
	}

	s.mu.Lock()
	s.requests = append(s.requests, request.Method+" "+version)
	s.headers = append(s.headers, r.Header.Get("MCP-Protocol-Version"))
	reject := request.Method == string(mcp.MethodServerDiscover) && (s.rejections < 0 || s.rejected < s.rejections)
	if reject {
		s.rejected++
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if reject {
		response := mcp.UnsupportedProtocolVersionError{Version: version, Supported: s.supported}.JSONRPCError()
		response.ID = mcp.NewRequestId(request.ID)
		if s.omitData {
			response.Error.Data = nil
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(response)
		return
	}

	if s.retryError && request.Method == string(mcp.MethodServerDiscover) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": request.ID,
			"error": map[string]any{"code": mcp.METHOD_NOT_FOUND, "message": "Method not found"},
		})
		return
	}

	var result any
	switch request.Method {
	case string(mcp.MethodServerDiscover):
		result = map[string]any{
			"resultType":        "complete",
			"supportedVersions": s.supported,
			"capabilities":      map[string]any{},
			"_meta": map[string]any{
				mcp.MetaKeyServerInfo: map[string]any{"name": "rejecting", "version": "1.0.0"},
			},
			"ttlMs":      0,
			"cacheScope": "private",
		}
		if s.discoverResult != nil {
			result = s.discoverResult
		}
	case string(mcp.MethodInitialize):
		// A legacy server answers with the newest legacy version it supports.
		answer := mcp.LATEST_LEGACY_PROTOCOL_VERSION
		for _, supported := range s.supported {
			if !mcp.IsModernProtocol(supported) {
				answer = supported
				break
			}
		}
		result = map[string]any{
			"protocolVersion": answer,
			"capabilities":    map[string]any{},
			"serverInfo":      map[string]any{"name": "rejecting", "version": "1.0.0"},
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
}

func initializeAgainst(t *testing.T, server *versionRejectingServer) (*mcp.InitializeResult, error) {
	t.Helper()
	client := newClientAgainst(t, server)
	return client.Initialize(t.Context(), mcp.InitializeRequest{})
}

func newClientAgainst(t *testing.T, server *versionRejectingServer, opts ...ClientOption) *Client {
	t.Helper()
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)

	httpTransport, err := transport.NewStreamableHTTP(httpServer.URL)
	require.NoError(t, err)
	client := NewClient(httpTransport, opts...)
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Start(t.Context()))
	return client
}

func TestInitializeRetriesAnAdvertisedRejectedVersion(t *testing.T) {
	tests := []struct {
		name      string
		supported []string
	}{
		{"modern only", []string{mcp.ProtocolVersion20260728}},
		{"also advertises legacy", []string{mcp.ProtocolVersion20260728, mcp.ProtocolVersion20250618}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &versionRejectingServer{supported: tt.supported, rejections: 1}
			client := newClientAgainst(t, server)

			result, err := client.Initialize(t.Context(), mcp.InitializeRequest{})
			require.NoError(t, err)
			assert.Equal(t, mcp.ProtocolVersion20260728, result.ProtocolVersion)
			assert.True(t, client.IsInitialized())
			assert.Equal(t, mcp.ProtocolVersion20260728, client.ProtocolVersion())
			assert.Equal(t, []string{
				"server/discover " + mcp.ProtocolVersion20260728,
				"server/discover " + mcp.ProtocolVersion20260728,
			}, server.seen())
			server.mu.Lock()
			defer server.mu.Unlock()
			assert.Equal(t, []string{mcp.ProtocolVersion20260728, mcp.ProtocolVersion20260728}, server.headers)
		})
	}
}

func TestInitializeStopsAfterRepeatedSameVersionRejection(t *testing.T) {
	server := &versionRejectingServer{supported: []string{mcp.ProtocolVersion20260728}, rejections: -1}
	client := newClientAgainst(t, server)

	result, err := client.Initialize(t.Context(), mcp.InitializeRequest{})
	var unsupported mcp.UnsupportedProtocolVersionError
	require.ErrorAs(t, err, &unsupported)
	assert.Nil(t, result)
	assert.False(t, client.IsInitialized())
	assert.Empty(t, client.ProtocolVersion())
	assert.Equal(t, []string{
		"server/discover " + mcp.ProtocolVersion20260728,
		"server/discover " + mcp.ProtocolVersion20260728,
	}, server.seen())
}

func TestInitializeRetriesWithADifferentAdvertisedVersion(t *testing.T) {
	server := &versionRejectingServer{supported: []string{mcp.ProtocolVersion20260728}, rejections: 1}
	client := newClientAgainst(t, server, WithProtocolVersion("2099-01-01"))

	result, err := client.Initialize(t.Context(), mcp.InitializeRequest{})
	require.NoError(t, err)
	assert.Equal(t, mcp.ProtocolVersion20260728, result.ProtocolVersion)
	assert.True(t, client.IsInitialized())
	assert.Equal(t, mcp.ProtocolVersion20260728, client.ProtocolVersion())
	assert.Equal(t, []string{
		"server/discover 2099-01-01",
		"server/discover " + mcp.ProtocolVersion20260728,
	}, server.seen())
	server.mu.Lock()
	defer server.mu.Unlock()
	assert.Equal(t, []string{"2099-01-01", mcp.ProtocolVersion20260728}, server.headers)
}

func TestInitializeDoesNotFallBackAfterModernVersionError(t *testing.T) {
	tests := []struct {
		name       string
		rejections int
		retryError bool
		wantCalls  []string
	}{
		{"legacy versions only", 1, false, []string{"server/discover " + mcp.ProtocolVersion20260728}},
		{"retry rejected", -1, false, []string{"server/discover 2099-01-01", "server/discover " + mcp.ProtocolVersion20260728}},
		{"non-modern retry error", 1, true, []string{"server/discover 2099-01-01", "server/discover " + mcp.ProtocolVersion20260728}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			supported := []string{mcp.ProtocolVersion20250618}
			preferred := mcp.ProtocolVersion20260728
			if tt.name != "legacy versions only" {
				supported = append(supported, mcp.ProtocolVersion20260728)
				preferred = "2099-01-01"
			}
			server := &versionRejectingServer{supported: supported, rejections: tt.rejections, retryError: tt.retryError}
			client := newClientAgainst(t, server, WithProtocolVersion(preferred))

			result, err := client.Initialize(t.Context(), mcp.InitializeRequest{})
			assert.Error(t, err)
			assert.Nil(t, result)
			assert.False(t, client.IsInitialized())
			assert.Empty(t, client.ProtocolVersion())
			assert.Equal(t, tt.wantCalls, server.seen())
		})
	}
}

// With no version in common the client reports the error instead of trying a
// handshake the server did not offer.
func TestInitializeReportsNoCommonVersion(t *testing.T) {
	server := &versionRejectingServer{supported: []string{"2099-01-01"}, rejections: 1}

	_, err := initializeAgainst(t, server)
	require.Error(t, err)
	assert.True(t, mcp.IsUnsupportedProtocolVersion(err), "got %v", err)
	assert.Equal(t, []string{"server/discover " + mcp.ProtocolVersion20260728}, server.seen())
}

// An UnsupportedProtocolVersionError without the supported list is not a
// recognizable modern answer, so the client falls back as before.
func TestInitializeFallsBackOnUnsupportedVersionWithoutData(t *testing.T) {
	server := &versionRejectingServer{omitData: true, rejections: -1}

	result, err := initializeAgainst(t, server)
	require.NoError(t, err)
	assert.Equal(t, mcp.LATEST_LEGACY_PROTOCOL_VERSION, result.ProtocolVersion)
	requests := server.seen()
	require.Len(t, requests, 2)
	assert.Contains(t, requests[1], "initialize ")
}

func TestInitializeDoesNotFallBackOnEmptyAdvertisedVersions(t *testing.T) {
	server := &versionRejectingServer{supported: []string{}, rejections: -1}
	client := newClientAgainst(t, server)

	result, err := client.Initialize(t.Context(), mcp.InitializeRequest{})
	var unsupported mcp.UnsupportedProtocolVersionError
	require.ErrorAs(t, err, &unsupported)
	assert.NotNil(t, unsupported.Supported)
	assert.Empty(t, unsupported.Supported)
	assert.Nil(t, result)
	assert.False(t, client.IsInitialized())
	assert.Empty(t, client.ProtocolVersion())
	assert.Equal(t, []string{"server/discover " + mcp.ProtocolVersion20260728}, server.seen())
}

func TestInitializeFallsBackOnNullAdvertisedVersions(t *testing.T) {
	server := &versionRejectingServer{rejections: -1}

	result, err := initializeAgainst(t, server)
	require.NoError(t, err)
	assert.Equal(t, mcp.LATEST_LEGACY_PROTOCOL_VERSION, result.ProtocolVersion)
	assert.Equal(t, []string{
		"server/discover " + mcp.ProtocolVersion20260728,
		"initialize " + mcp.LATEST_LEGACY_PROTOCOL_VERSION,
	}, server.seen())
}

// A successful discovery stays modern, even without a usable modern version.
func TestInitializeReportsNoUsableModernDiscoverVersion(t *testing.T) {
	tests := []struct {
		name      string
		supported []string
	}{
		{"empty list", []string{}},
		{"unknown version", []string{"2099-01-01"}},
		{"legacy only", []string{mcp.ProtocolVersion20250618}},
		{"unknown and legacy", []string{"2099-01-01", mcp.ProtocolVersion20250618}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &versionRejectingServer{supported: tt.supported}
			client := newClientAgainst(t, server)

			result, err := client.Initialize(t.Context(), mcp.InitializeRequest{})
			assert.ErrorContains(t, err, "server/discover result lists no modern version this client supports")
			assert.Nil(t, result)
			assert.False(t, client.IsInitialized())
			assert.Empty(t, client.ProtocolVersion())
			assert.Equal(t, []string{"server/discover " + mcp.ProtocolVersion20260728}, server.seen())
		})
	}
}
