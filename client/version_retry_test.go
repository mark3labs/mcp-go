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
	supported  []string
	rejections int
	omitData   bool

	mu       sync.Mutex
	rejected int
	requests []string
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

	var result any
	switch request.Method {
	case string(mcp.MethodServerDiscover):
		result = map[string]any{
			"supportedVersions": s.supported,
			"capabilities":      map[string]any{},
			"serverInfo":        map[string]any{"name": "rejecting", "version": "1.0.0"},
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
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)

	httpTransport, err := transport.NewStreamableHTTP(httpServer.URL)
	require.NoError(t, err)
	client := NewClient(httpTransport)
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Start(t.Context()))
	return client.Initialize(t.Context(), mcp.InitializeRequest{})
}

// UnsupportedProtocolVersionError identifies a modern server, and the client
// retries with a version from its list, even the one it just tried, rather
// than falling back to the initialize handshake.
func TestInitializeRetriesWithAnAdvertisedVersion(t *testing.T) {
	server := &versionRejectingServer{supported: []string{mcp.ProtocolVersion20260728}, rejections: 1}

	result, err := initializeAgainst(t, server)
	require.NoError(t, err)
	assert.Equal(t, mcp.ProtocolVersion20260728, result.ProtocolVersion)
	assert.Equal(t, []string{
		"server/discover " + mcp.ProtocolVersion20260728,
		"server/discover " + mcp.ProtocolVersion20260728,
	}, server.seen())
}

// A server that also lists a legacy version is served through the handshake,
// here after rejecting both discover attempts.
func TestInitializeFallsBackToAnAdvertisedLegacyVersion(t *testing.T) {
	server := &versionRejectingServer{
		supported:  []string{mcp.ProtocolVersion20260728, mcp.ProtocolVersion20250618},
		rejections: -1,
	}

	result, err := initializeAgainst(t, server)
	require.NoError(t, err)
	assert.Equal(t, mcp.ProtocolVersion20250618, result.ProtocolVersion)
	requests := server.seen()
	require.Len(t, requests, 3)
	assert.Contains(t, requests[2], "initialize ")
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
