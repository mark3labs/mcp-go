package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// modernMeta builds the per-request _meta every request must carry from
// protocol version 2026-07-28 onward.
func modernMeta() map[string]any {
	return map[string]any{
		mcp.MetaKeyProtocolVersion: mcp.ProtocolVersion20260728,
		mcp.MetaKeyClientInfo: map[string]any{
			"name":    "test-client",
			"version": "1.0.0",
		},
		mcp.MetaKeyClientCapabilities: map[string]any{},
	}
}

// postModern sends a modern JSON-RPC request with the headers SEP-2243
// requires, and returns the raw HTTP response.
func postModern(t *testing.T, url string, method mcp.MCPMethod, params map[string]any) *http.Response {
	t.Helper()

	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = modernMeta()

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set(mcp.HeaderProtocolVersion, mcp.ProtocolVersion20260728)
	req.Header.Set(mcp.HeaderMethod, string(method))

	rawParams, err := json.Marshal(params)
	require.NoError(t, err)
	if name, ok := mcp.ExtractHeaderName(method, rawParams); ok {
		encoded, ok := mcp.EncodeHeaderValue(name)
		require.True(t, ok)
		req.Header.Set(mcp.HeaderName, encoded)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func decodeJSONRPC(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var message map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&message))
	return message
}

func newModernTestServer(t *testing.T, opts ...ServerOption) *httptest.Server {
	t.Helper()

	base := []ServerOption{
		WithToolCapabilities(true),
		WithResourceCapabilities(true, true),
		WithPromptCapabilities(true),
		WithInstructions("be helpful"),
	}
	mcpServer := NewMCPServer("modern-test", "1.0.0", append(base, opts...)...)
	mcpServer.AddTool(
		mcp.NewTool("greet", mcp.WithString("name", mcp.Description("who to greet"))),
		func(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("hello " + request.GetString("name", "world")), nil
		},
	)

	httpServer := httptest.NewServer(NewStreamableHTTPServer(mcpServer))
	t.Cleanup(httpServer.Close)
	return httpServer
}

func TestModernProtocol_Discover(t *testing.T) {
	srv := newModernTestServer(t)

	resp := postModern(t, srv.URL, mcp.MethodServerDiscover, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	message := decodeJSONRPC(t, resp)
	result, ok := message["result"].(map[string]any)
	require.True(t, ok, "expected a result, got %v", message)

	// The server must advertise the versions it can serve, newest first.
	versions, ok := result["supportedVersions"].([]any)
	require.True(t, ok)
	assert.Equal(t, mcp.ProtocolVersion20260728, versions[0])

	// Every modern result carries resultType and the server identity.
	assert.Equal(t, string(mcp.ResultTypeComplete), result["resultType"])
	meta, ok := result["_meta"].(map[string]any)
	require.True(t, ok, "expected _meta on the result")
	serverInfo, ok := meta[mcp.MetaKeyServerInfo].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "modern-test", serverInfo["name"])

	// server/discover is a cacheable result (SEP-2549). With no hints
	// configured the scope is the fail-closed default.
	assert.Contains(t, result, "ttlMs")
	assert.Equal(t, string(mcp.CacheScopePrivate), result["cacheScope"])

	assert.Equal(t, "be helpful", result["instructions"])
}

func TestModernProtocol_NoSessionID(t *testing.T) {
	srv := newModernTestServer(t)

	resp := postModern(t, srv.URL, mcp.MethodServerDiscover, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Protocol version 2026-07-28 retired the session header: the server must
	// neither mint nor echo one.
	assert.Empty(t, resp.Header.Get(mcp.HeaderSessionID))
}

func TestModernProtocol_ToolsListIsCacheable(t *testing.T) {
	srv := newModernTestServer(t, WithCacheHints(60000, mcp.CacheScopePrivate))

	resp := postModern(t, srv.URL, mcp.MethodToolsList, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	result := decodeJSONRPC(t, resp)["result"].(map[string]any)
	assert.Equal(t, float64(60000), result["ttlMs"])
	assert.Equal(t, string(mcp.CacheScopePrivate), result["cacheScope"])
	assert.Equal(t, string(mcp.ResultTypeComplete), result["resultType"])
}

func TestModernProtocol_ToolCall(t *testing.T) {
	srv := newModernTestServer(t)

	resp := postModern(t, srv.URL, mcp.MethodToolsCall, map[string]any{
		"name":      "greet",
		"arguments": map[string]any{"name": "MCP"},
	})
	require.Equal(t, http.StatusOK, resp.StatusCode)

	result := decodeJSONRPC(t, resp)["result"].(map[string]any)
	assert.Equal(t, string(mcp.ResultTypeComplete), result["resultType"])

	content := result["content"].([]any)
	require.Len(t, content, 1)
	assert.Equal(t, "hello MCP", content[0].(map[string]any)["text"])
}

func TestModernProtocol_RemovedMethodsRejected(t *testing.T) {
	srv := newModernTestServer(t, WithLogging())

	for _, method := range []mcp.MCPMethod{
		mcp.MethodInitialize,
		mcp.MethodPing,
		mcp.MethodSetLogLevel,
		mcp.MethodResourcesSubscribe,
		mcp.MethodResourcesUnsubscribe,
	} {
		t.Run(string(method), func(t *testing.T) {
			resp := postModern(t, srv.URL, method, nil)

			// SEP-2575 maps MethodNotFound onto HTTP 404.
			assert.Equal(t, http.StatusNotFound, resp.StatusCode)

			errDetails := decodeJSONRPC(t, resp)["error"].(map[string]any)
			assert.Equal(t, float64(mcp.METHOD_NOT_FOUND), errDetails["code"])
			assert.Contains(t, errDetails["message"], "removed in protocol version")
		})
	}
}

func TestModernProtocol_ModernOnlyMethodsRejectedForLegacyClients(t *testing.T) {
	// Stateless, so a legacy request without a session ID still reaches the
	// dispatcher and is rejected on protocol grounds rather than session ones.
	mcpServer := NewMCPServer("modern-test", "1.0.0", WithToolCapabilities(true))
	srv := httptest.NewServer(NewStreamableHTTPServer(mcpServer, WithStateLess(true)))
	defer srv.Close()

	for _, method := range []mcp.MCPMethod{
		mcp.MethodServerDiscover,
		mcp.MethodSubscriptionsListen,
	} {
		t.Run(string(method), func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  method,
				"params":  map[string]any{},
			})
			require.NoError(t, err)

			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL, bytes.NewReader(body))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			errDetails := decodeJSONRPC(t, resp)["error"].(map[string]any)
			assert.Equal(t, float64(mcp.METHOD_NOT_FOUND), errDetails["code"])
			assert.Contains(t, errDetails["message"], "requires protocol version")
		})
	}
}

func TestModernProtocol_GetAndDeleteRejected(t *testing.T) {
	srv := newModernTestServer(t)

	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), method, srv.URL, nil)
			require.NoError(t, err)
			req.Header.Set(mcp.HeaderProtocolVersion, mcp.ProtocolVersion20260728)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
			// RFC 9110 requires an Allow header on 405 responses.
			assert.Equal(t, http.MethodPost, resp.Header.Get("Allow"))
		})
	}
}

func TestModernProtocol_HeaderValidation(t *testing.T) {
	srv := newModernTestServer(t)

	newRequest := func(t *testing.T, mutate func(*http.Request)) *http.Response {
		t.Helper()
		body, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  mcp.MethodToolsCall,
			"params": map[string]any{
				"name":      "greet",
				"arguments": map[string]any{},
				"_meta":     modernMeta(),
			},
		})
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL, bytes.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(mcp.HeaderProtocolVersion, mcp.ProtocolVersion20260728)
		req.Header.Set(mcp.HeaderMethod, string(mcp.MethodToolsCall))
		req.Header.Set(mcp.HeaderName, "greet")
		mutate(req)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}

	t.Run("missing Mcp-Method", func(t *testing.T) {
		resp := newRequest(t, func(r *http.Request) { r.Header.Del(mcp.HeaderMethod) })
		errDetails := decodeJSONRPC(t, resp)["error"].(map[string]any)
		assert.Equal(t, float64(mcp.HEADER_MISMATCH), errDetails["code"])
	})

	t.Run("Mcp-Method disagrees with body", func(t *testing.T) {
		resp := newRequest(t, func(r *http.Request) { r.Header.Set(mcp.HeaderMethod, "tools/list") })
		errDetails := decodeJSONRPC(t, resp)["error"].(map[string]any)
		assert.Equal(t, float64(mcp.HEADER_MISMATCH), errDetails["code"])
	})

	t.Run("Mcp-Name disagrees with body", func(t *testing.T) {
		resp := newRequest(t, func(r *http.Request) { r.Header.Set(mcp.HeaderName, "farewell") })
		errDetails := decodeJSONRPC(t, resp)["error"].(map[string]any)
		assert.Equal(t, float64(mcp.HEADER_MISMATCH), errDetails["code"])
	})

	t.Run("missing protocol version header", func(t *testing.T) {
		resp := newRequest(t, func(r *http.Request) { r.Header.Del(mcp.HeaderProtocolVersion) })
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		errDetails := decodeJSONRPC(t, resp)["error"].(map[string]any)
		assert.Equal(t, float64(mcp.HEADER_MISMATCH), errDetails["code"])
	})

	t.Run("Last-Event-ID is refused", func(t *testing.T) {
		resp := newRequest(t, func(r *http.Request) { r.Header.Set(mcp.HeaderLastEventID, "42") })
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		errDetails := decodeJSONRPC(t, resp)["error"].(map[string]any)
		assert.Equal(t, float64(mcp.HEADER_MISMATCH), errDetails["code"])
	})
}

func TestModernProtocol_UnsupportedVersionNegotiation(t *testing.T) {
	mcpServer := NewMCPServer("modern-test", "1.0.0", WithToolCapabilities(true))
	// A deployment that depends on protocol-level session state pins itself to
	// the legacy revisions.
	streamable := NewStreamableHTTPServer(mcpServer,
		WithStreamableHTTPProtocolVersions(mcp.LegacyProtocolVersions()...))
	srv := httptest.NewServer(streamable)
	defer srv.Close()

	resp := postModern(t, srv.URL, mcp.MethodToolsList, nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	errDetails := decodeJSONRPC(t, resp)["error"].(map[string]any)
	assert.Equal(t, float64(mcp.UNSUPPORTED_PROTOCOL_VERSION), errDetails["code"])

	data := errDetails["data"].(map[string]any)
	assert.Equal(t, mcp.ProtocolVersion20260728, data["requested"])

	supported := data["supported"].([]any)
	assert.Equal(t, mcp.LATEST_LEGACY_PROTOCOL_VERSION, supported[0])
	assert.NotContains(t, supported, mcp.ProtocolVersion20260728)
}

func TestModernProtocol_LegacyClientsUnaffected(t *testing.T) {
	srv := newModernTestServer(t)

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  mcp.MethodInitialize,
		"params": map[string]any{
			"protocolVersion": mcp.LATEST_LEGACY_PROTOCOL_VERSION,
			"clientInfo":      map[string]any{"name": "legacy", "version": "1.0.0"},
			"capabilities":    map[string]any{},
		},
	})
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	result := decodeJSONRPC(t, resp)["result"].(map[string]any)

	// The handshake still works, still negotiates a legacy version, and still
	// mints a session.
	assert.Equal(t, mcp.LATEST_LEGACY_PROTOCOL_VERSION, result["protocolVersion"])
	assert.NotEmpty(t, resp.Header.Get(mcp.HeaderSessionID))

	// Legacy responses are untouched: no resultType, no server identity.
	assert.NotContains(t, result, "resultType")
	assert.NotContains(t, result, "ttlMs")
}

func TestModernProtocol_EventStoreDoesNotStrandTheForwarder(t *testing.T) {
	// Stream resumability was removed in 2026-07-28, so a modern request is
	// never resumable even when the server has an event store configured for
	// its legacy clients. The notification forwarder must still be stopped
	// when the request ends: a forwarder that outlives its request leaks a
	// goroutine and races the final response write.
	mcpServer := NewMCPServer("modern-test", "1.0.0",
		WithToolCapabilities(true),
		WithLogging(),
	)
	mcpServer.AddTool(
		mcp.NewTool("notify"),
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			server := ServerFromContext(ctx)
			_ = server.SendNotificationToClient(ctx, "notifications/progress", map[string]any{
				"progress": 1,
			})
			return mcp.NewToolResultText("notified"), nil
		},
	)

	httpServer := httptest.NewServer(NewStreamableHTTPServer(mcpServer,
		WithEventStore(NewInMemoryEventStore()),
	))
	defer httpServer.Close()

	before := runtime.NumGoroutine()

	const requests = 25
	for range requests {
		resp := postModern(t, httpServer.URL, mcp.MethodToolsCall, map[string]any{
			"name":      "notify",
			"arguments": map[string]any{},
		})
		require.Equal(t, http.StatusOK, resp.StatusCode)
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}

	// Each stranded forwarder blocks forever on its own request's channels, so
	// a leak shows up as a goroutine count that scales with the request count.
	var after int
	for range 20 {
		time.Sleep(100 * time.Millisecond)
		after = runtime.NumGoroutine()
		if after-before < requests/2 {
			break
		}
	}

	assert.Less(t, after-before, requests/2,
		"notification forwarders outlived their requests: %d goroutines before, %d after %d requests",
		before, after, requests)
}

// establishLegacySession performs the legacy handshake and returns the minted
// session ID.
func establishLegacySession(t *testing.T, url string) string {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  mcp.MethodInitialize,
		"params": map[string]any{
			"protocolVersion": mcp.LATEST_LEGACY_PROTOCOL_VERSION,
			"clientInfo":      map[string]any{"name": "legacy", "version": "1.0.0"},
			"capabilities":    map[string]any{},
		},
	})
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	sessionID := resp.Header.Get(mcp.HeaderSessionID)
	require.NotEmpty(t, sessionID, "the legacy handshake must mint a session")
	return sessionID
}

func TestClientToServerResponsesByProtocolEra(t *testing.T) {
	// A POST carrying an id plus a result is a client-to-server response,
	// answering a server-initiated request. Protocol version 2026-07-28
	// replaced that pattern with multi round-trip requests, so a modern
	// response is refused: otherwise a valid Mcp-Session-Id could steer a
	// modern message onto the legacy, session-scoped delivery path.
	tests := []struct {
		name string
		// stateful mints real sessions, so the replayed ID is genuinely valid.
		stateful bool
		// withSession performs the legacy handshake first and replays the
		// resulting session ID on the response message.
		withSession bool
		// protocolVersion is sent in the Mcp-Protocol-Version header.
		protocolVersion string
		wantRejected    bool
	}{
		{
			name:            "a modern response replaying a valid session is rejected",
			stateful:        true,
			withSession:     true,
			protocolVersion: mcp.ProtocolVersion20260728,
			wantRejected:    true,
		},
		{
			name:            "a modern response without a session is rejected",
			protocolVersion: mcp.ProtocolVersion20260728,
			wantRejected:    true,
		},
		{
			name:         "a legacy response still reaches the delivery path",
			wantRejected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mcpServer := NewMCPServer("modern-test", "1.0.0", WithToolCapabilities(true))

			var options []StreamableHTTPOption
			if tt.stateful {
				options = append(options, WithStateful(true))
			}
			httpServer := httptest.NewServer(NewStreamableHTTPServer(mcpServer, options...))
			defer httpServer.Close()

			var sessionID string
			if tt.withSession {
				sessionID = establishLegacySession(t, httpServer.URL)
			}

			body, err := json.Marshal(map[string]any{
				"jsonrpc": "2.0",
				"id":      42,
				"result":  map[string]any{"action": "accept"},
			})
			require.NoError(t, err)

			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, httpServer.URL, bytes.NewReader(body))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			if tt.protocolVersion != "" {
				req.Header.Set(mcp.HeaderProtocolVersion, tt.protocolVersion)
			}
			if sessionID != "" {
				req.Header.Set(mcp.HeaderSessionID, sessionID)
			}

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			payload, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			if !tt.wantRejected {
				// The legacy path reports its own outcome; what matters is
				// that the modern-era rejection did not fire.
				assert.NotContains(t, string(payload), "not supported in protocol version",
					"legacy responses must still reach the session-scoped delivery path")
				return
			}

			assert.Equal(t, http.StatusBadRequest, resp.StatusCode,
				"a modern client-to-server response must not reach the session-scoped path")

			var message map[string]any
			require.NoError(t, json.Unmarshal(payload, &message))
			errDetails := message["error"].(map[string]any)
			assert.Equal(t, float64(mcp.INVALID_REQUEST), errDetails["code"])
			assert.Contains(t, errDetails["message"], "not supported in protocol version")
		})
	}
}
