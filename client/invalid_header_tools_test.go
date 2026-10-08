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

const annotatedTools = `[
	{"name": "query", "inputSchema": {"type": "object", "properties": {
		"region": {"type": "string", "x-mcp-header": "Region"}}}},
	{"name": "plain", "inputSchema": {"type": "object"}},
	{"name": "empty_header", "inputSchema": {"type": "object", "properties": {
		"region": {"type": "string", "x-mcp-header": ""}}}},
	{"name": "number_header", "inputSchema": {"type": "object", "properties": {
		"score": {"type": "number", "x-mcp-header": "Score"}}}},
	{"name": "duplicate_header", "inputSchema": {"type": "object", "properties": {
		"a": {"type": "string", "x-mcp-header": "Region"},
		"b": {"type": "string", "x-mcp-header": "region"}}}}
]`

// headerToolsServer answers tools/list with the listings given, one per call,
// repeating the last. It speaks protocol version 2026-07-28 unless legacy is
// set, in which case it rejects server/discover and completes the initialize
// handshake instead. It records the Mcp-Param-Region header of each tools/call.
type headerToolsServer struct {
	t        *testing.T
	legacy   bool
	listings []string

	mu            sync.Mutex
	lists         int
	regionHeaders []string
}

func (s *headerToolsServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.t.Errorf("reading request: %v", err)
		return
	}
	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		s.t.Errorf("decoding request: %v", err)
		return
	}
	if request.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	var result any
	switch request.Method {
	case string(mcp.MethodServerDiscover):
		if s.legacy {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": request.ID,
				"error": map[string]any{"code": mcp.METHOD_NOT_FOUND, "message": "method not found"},
			})
			return
		}
		result = map[string]any{
			"supportedVersions": []string{mcp.ProtocolVersion20260728},
			"capabilities":      map[string]any{"tools": map[string]any{}},
			"serverInfo":        map[string]any{"name": "header-tools", "version": "1.0.0"},
		}
	case string(mcp.MethodInitialize):
		result = map[string]any{
			"protocolVersion": mcp.LATEST_LEGACY_PROTOCOL_VERSION,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "header-tools", "version": "1.0.0"},
		}
	case string(mcp.MethodToolsList):
		s.mu.Lock()
		listing := s.listings[min(s.lists, len(s.listings)-1)]
		s.lists++
		s.mu.Unlock()
		result = map[string]any{"tools": json.RawMessage(listing)}
	case string(mcp.MethodToolsCall):
		s.mu.Lock()
		s.regionHeaders = append(s.regionHeaders, r.Header.Get(mcp.HeaderParamPrefix+"Region"))
		s.mu.Unlock()
		result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}}
	default:
		s.t.Errorf("unexpected method %q", request.Method)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
}

func connectHeaderClient(t *testing.T, server *headerToolsServer, wrap func(transport.Interface) transport.Interface) (*Client, string) {
	t.Helper()
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)

	httpTransport, err := transport.NewStreamableHTTP(httpServer.URL)
	require.NoError(t, err)
	var tr transport.Interface = httpTransport
	if wrap != nil {
		tr = wrap(tr)
	}
	client := NewClient(tr)
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Start(t.Context()))

	initialized, err := client.Initialize(t.Context(), mcp.InitializeRequest{})
	require.NoError(t, err)
	return client, initialized.ProtocolVersion
}

func listToolNames(t *testing.T, client *Client) []string {
	t.Helper()
	result, err := client.ListTools(t.Context(), mcp.ListToolsRequest{})
	require.NoError(t, err)
	var names []string
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// Over Streamable HTTP a client MUST exclude a tool whose x-mcp-header
// annotations break SEP-2243, and keep the rest usable.
func TestListToolsExcludesInvalidHeaderAnnotations(t *testing.T) {
	client, version := connectHeaderClient(t, &headerToolsServer{t: t, listings: []string{annotatedTools}}, nil)
	require.Equal(t, mcp.ProtocolVersion20260728, version)
	assert.Equal(t, []string{"query", "plain"}, listToolNames(t, client))
}

// A transport wrapped for logging is still Streamable HTTP underneath.
func TestListToolsExcludesInvalidHeaderAnnotationsThroughLogging(t *testing.T) {
	client, _ := connectHeaderClient(t, &headerToolsServer{t: t, listings: []string{annotatedTools}},
		func(inner transport.Interface) transport.Interface { return transport.NewLogging(inner, nil) })
	assert.Equal(t, []string{"query", "plain"}, listToolNames(t, client))
}

// A tool whose definition turns invalid is not called with headers mirrored
// from the definition cached before.
func TestListToolsForgetsAToolThatTurnsInvalid(t *testing.T) {
	server := &headerToolsServer{t: t, listings: []string{
		`[{"name": "q", "inputSchema": {"type": "object", "properties": {
			"region": {"type": "string", "x-mcp-header": "Region"}}}}]`,
		`[{"name": "q", "inputSchema": {"type": "object", "properties": {
			"region": {"type": "string", "x-mcp-header": "Region"},
			"zone": {"type": "string", "x-mcp-header": "region"}}}}]`,
	}}
	client, _ := connectHeaderClient(t, server, nil)

	assert.Equal(t, []string{"q"}, listToolNames(t, client))
	assert.Empty(t, listToolNames(t, client))

	_, err := client.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name:      "q",
		Arguments: map[string]any{"region": "us"},
	}})
	require.NoError(t, err)
	server.mu.Lock()
	defer server.mu.Unlock()
	assert.Equal(t, []string{""}, server.regionHeaders)
}

// Before protocol version 2026-07-28 the annotations mean nothing, so every
// tool is listed as before.
func TestListToolsKeepsAnnotatedToolsForLegacyConnections(t *testing.T) {
	client, version := connectHeaderClient(t, &headerToolsServer{t: t, legacy: true, listings: []string{annotatedTools}}, nil)
	require.Equal(t, mcp.LATEST_LEGACY_PROTOCOL_VERSION, version)
	assert.Equal(t, []string{"query", "plain", "empty_header", "number_header", "duplicate_header"}, listToolNames(t, client))
}
