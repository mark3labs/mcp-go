package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newHeaderToolServer serves a tool whose region parameter is annotated to
// travel in an HTTP header, so gateways can route on it without parsing the
// body (SEP-2243).
//
// It is stateless so that legacy requests, which carry no session ID, reach
// the dispatcher rather than being turned away by session validation.
func newHeaderToolServer(t *testing.T) *httptest.Server {
	t.Helper()

	srv := NewMCPServer("header-test", "1.0.0", WithToolCapabilities(true))
	srv.AddTool(
		mcp.NewToolWithRawSchema("query", "runs a query", json.RawMessage(`{
			"type": "object",
			"properties": {
				"sql":    { "type": "string" },
				"region": { "type": "string", "x-mcp-header": "Region" }
			}
		}`)),
		func(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("ran in " + request.GetString("region", "")), nil
		},
	)

	httpServer := httptest.NewServer(NewStreamableHTTPServer(srv, WithStateLess(true)))
	t.Cleanup(httpServer.Close)
	return httpServer
}

// postWithHeaders sends a modern tools/call request with caller-controlled
// headers.
func postWithHeaders(
	t *testing.T,
	url string,
	arguments map[string]any,
	headers map[string]string,
) *http.Response {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  mcp.MethodToolsCall,
		"params": map[string]any{
			"name":      "query",
			"arguments": arguments,
			"_meta":     modernMeta(),
		},
	})
	require.NoError(t, err)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, bytes.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(mcp.HeaderProtocolVersion, mcp.ProtocolVersion20260728)
	request.Header.Set(mcp.HeaderMethod, string(mcp.MethodToolsCall))
	request.Header.Set(mcp.HeaderName, "query")
	for name, value := range headers {
		request.Header.Set(name, value)
	}

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func TestParamHeaders_MirroredValueIsAccepted(t *testing.T) {
	srv := newHeaderToolServer(t)

	response := postWithHeaders(t,
		srv.URL,
		map[string]any{"sql": "select 1", "region": "us-east-1"},
		map[string]string{mcp.HeaderParamPrefix + "Region": "us-east-1"},
	)
	require.Equal(t, http.StatusOK, response.StatusCode)

	result := decodeJSONRPC(t, response)["result"].(map[string]any)
	content := result["content"].([]any)
	assert.Equal(t, "ran in us-east-1", content[0].(map[string]any)["text"])
}

func TestParamHeaders_MismatchIsRejected(t *testing.T) {
	srv := newHeaderToolServer(t)

	// A gateway routing on the header would send this request somewhere the
	// body does not agree with, so the server refuses it.
	response := postWithHeaders(t,
		srv.URL,
		map[string]any{"region": "us-east-1"},
		map[string]string{mcp.HeaderParamPrefix + "Region": "eu-west-1"},
	)

	errDetails := decodeJSONRPC(t, response)["error"].(map[string]any)
	assert.Equal(t, float64(mcp.HEADER_MISMATCH), errDetails["code"])
}

func TestParamHeaders_MissingHeaderIsRejected(t *testing.T) {
	srv := newHeaderToolServer(t)

	response := postWithHeaders(t,
		srv.URL,
		map[string]any{"region": "us-east-1"},
		nil,
	)

	errDetails := decodeJSONRPC(t, response)["error"].(map[string]any)
	assert.Equal(t, float64(mcp.HEADER_MISMATCH), errDetails["code"])
}

func TestParamHeaders_AbsentParameterNeedsNoHeader(t *testing.T) {
	srv := newHeaderToolServer(t)

	response := postWithHeaders(t, srv.URL, map[string]any{"sql": "select 1"}, nil)
	require.Equal(t, http.StatusOK, response.StatusCode)
	assert.Contains(t, decodeJSONRPC(t, response), "result")
}

func TestParamHeaders_NotCheckedForLegacyClients(t *testing.T) {
	srv := newHeaderToolServer(t)

	// The header contract was introduced in 2026-07-28; a client using an
	// earlier revision is not held to it.
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  mcp.MethodToolsCall,
		"params": map[string]any{
			"name":      "query",
			"arguments": map[string]any{"region": "us-east-1"},
		},
	})
	require.NoError(t, err)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL, bytes.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()

	assert.Contains(t, decodeJSONRPC(t, response), "result")
}

func TestParamHeaders_InvalidAnnotationsAreRejectedAtRegistration(t *testing.T) {
	srv := NewMCPServer("header-test", "1.0.0", WithToolCapabilities(true))

	// x-mcp-header may only be applied to primitive types, so a server that
	// declares otherwise is refused rather than producing unroutable headers.
	assert.PanicsWithValue(t,
		`tool "bad" has invalid x-mcp-header annotations: property "items": `+
			`x-mcp-header can only be applied to primitive types (integer, string, boolean), got "array"`,
		func() {
			srv.AddTool(
				mcp.NewToolWithRawSchema("bad", "invalid", json.RawMessage(`{
					"type": "object",
					"properties": { "items": { "type": "array", "x-mcp-header": "Items" } }
				}`)),
				func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return nil, nil
				},
			)
		},
	)
}

// badHeaderTool declares an x-mcp-header on a non-primitive property, which
// every registration path must refuse.
func badHeaderTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("bad", "invalid", json.RawMessage(`{
		"type": "object",
		"properties": { "items": { "type": "array", "x-mcp-header": "Items" } }
	}`))
}

const badHeaderToolPanic = `tool "bad" has invalid x-mcp-header annotations: property "items": ` +
	`x-mcp-header can only be applied to primitive types (integer, string, boolean), got "array"`

func TestParamHeaders_InvalidAnnotationsAreRejectedOnEveryRegistrationPath(t *testing.T) {
	noop := func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return nil, nil
	}
	noopTask := func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
		return nil, nil
	}

	// A valid tool precedes the bad one in every batch: rejecting the batch
	// must not leave the entries validated before the failure behind.
	tests := []struct {
		name     string
		register func(srv *MCPServer)
	}{
		{
			name: "AddTools",
			register: func(srv *MCPServer) {
				srv.AddTools(
					ServerTool{Tool: mcp.NewTool("good"), Handler: noop},
					ServerTool{Tool: badHeaderTool(), Handler: noop},
				)
			},
		},
		{
			name: "SetTools",
			register: func(srv *MCPServer) {
				srv.SetTools(
					ServerTool{Tool: mcp.NewTool("good"), Handler: noop},
					ServerTool{Tool: badHeaderTool(), Handler: noop},
				)
			},
		},
		{
			name: "AddTaskTools",
			register: func(srv *MCPServer) {
				srv.AddTaskTools(
					ServerTaskTool{Tool: mcp.NewTool("good"), Handler: noopTask},
					ServerTaskTool{Tool: badHeaderTool(), Handler: noopTask},
				)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewMCPServer("header-test", "1.0.0", WithToolCapabilities(true))

			assert.PanicsWithValue(t, badHeaderToolPanic, func() { tt.register(srv) })

			assert.Empty(t, srv.ListTools())
			srv.toolsMu.RLock()
			assert.Empty(t, srv.taskTools)
			srv.toolsMu.RUnlock()
		})
	}
}

// TestParamHeaders_InvalidAnnotationsAreRejectedForSessionTools checks that
// AddSessionTools refuses a batch holding a tool with invalid x-mcp-header
// annotations, leaving the session's previous tool set unchanged.
func TestParamHeaders_InvalidAnnotationsAreRejectedForSessionTools(t *testing.T) {
	srv := NewMCPServer("header-test", "1.0.0", WithToolCapabilities(true))
	session := &sessionTestClientWithTools{
		sessionID:           "header-session",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
		initialized:         true,
	}
	require.NoError(t, srv.RegisterSession(t.Context(), session))
	require.NoError(t, srv.AddSessionTool(session.SessionID(), mcp.NewTool("existing"), nil))

	err := srv.AddSessionTools(session.SessionID(),
		ServerTool{Tool: mcp.NewTool("good")},
		ServerTool{Tool: badHeaderTool()},
	)
	require.EqualError(t, err, badHeaderToolPanic)

	// The session keeps its previous tool set: the batch is rejected as a whole.
	tools := session.GetSessionTools()
	assert.Len(t, tools, 1)
	assert.Contains(t, tools, "existing")
}

// headerQueryTool is the annotated tool newHeaderToolServer serves, for tests
// that need the registry rather than a running server.
func headerQueryTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("query", "runs a query", json.RawMessage(`{
		"type": "object",
		"properties": {
			"sql":    { "type": "string" },
			"region": { "type": "string", "x-mcp-header": "Region" }
		}
	}`))
}

// TestParamHeaders_BindingsAreCachedOnEveryRegistrationPath checks that each
// way of registering a tool caches its x-mcp-header bindings, and that
// deleting or replacing tools drops their entries.
func TestParamHeaders_BindingsAreCachedOnEveryRegistrationPath(t *testing.T) {
	register := map[string]func(*MCPServer){
		"AddTool":     func(s *MCPServer) { s.AddTool(headerQueryTool(), nil) },
		"AddTools":    func(s *MCPServer) { s.AddTools(ServerTool{Tool: headerQueryTool()}) },
		"SetTools":    func(s *MCPServer) { s.SetTools(ServerTool{Tool: headerQueryTool()}) },
		"AddTaskTool": func(s *MCPServer) { s.AddTaskTool(headerQueryTool(), nil) },
	}
	for name, add := range register {
		t.Run(name, func(t *testing.T) {
			srv := NewMCPServer("header-test", "1.0.0", WithToolCapabilities(true))
			add(srv)

			bindings := srv.toolHeaderBindings["query"]
			require.Len(t, bindings, 1)
			assert.Equal(t, "Region", bindings[0].Header)
			assert.Equal(t, []string{"region"}, bindings[0].Path)
		})
	}

	t.Run("DeleteTools and SetTools drop the entry", func(t *testing.T) {
		srv := NewMCPServer("header-test", "1.0.0", WithToolCapabilities(true))
		srv.AddTool(headerQueryTool(), nil)
		srv.DeleteTools("query")
		assert.NotContains(t, srv.toolHeaderBindings, "query")

		srv.AddTool(headerQueryTool(), nil)
		srv.SetTools(ServerTool{Tool: mcp.NewTool("other")})
		assert.NotContains(t, srv.toolHeaderBindings, "query")
		assert.Contains(t, srv.toolHeaderBindings, "other")
	})
}

// TestParamHeaders_CallsValidateAgainstTheCachedBindings checks that a
// tools/call is validated against the bindings cached at registration rather
// than against the tool's schema.
func TestParamHeaders_CallsValidateAgainstTheCachedBindings(t *testing.T) {
	srv := NewMCPServer("header-test", "1.0.0", WithToolCapabilities(true))
	srv.AddTool(headerQueryTool(), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	})
	// Blank the cached bindings: a call that re-read the schema would find the
	// annotation and refuse the mismatch below.
	srv.toolHeaderBindings["query"] = nil

	httpServer := httptest.NewServer(NewStreamableHTTPServer(srv, WithStateLess(true)))
	t.Cleanup(httpServer.Close)

	response := postWithHeaders(t,
		httpServer.URL,
		map[string]any{"region": "us-east-1"},
		map[string]string{mcp.HeaderParamPrefix + "Region": "eu-west-1"},
	)
	require.Equal(t, http.StatusOK, response.StatusCode)
	_, isResult := decodeJSONRPC(t, response)["result"]
	assert.True(t, isResult, "the mismatch was refused, so the call re-read the schema instead of the cache")
}

// TestParamHeaders_AMissingCacheEntryFallsBackToTheSchema checks that a
// registered tool without a cache entry is still validated, against the
// bindings in its schema.
func TestParamHeaders_AMissingCacheEntryFallsBackToTheSchema(t *testing.T) {
	srv := NewMCPServer("header-test", "1.0.0", WithToolCapabilities(true))
	srv.AddTool(headerQueryTool(), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	})
	// A registration path that forgot the cache must not switch the check off.
	delete(srv.toolHeaderBindings, "query")

	httpServer := httptest.NewServer(NewStreamableHTTPServer(srv, WithStateLess(true)))
	t.Cleanup(httpServer.Close)

	response := postWithHeaders(t,
		httpServer.URL,
		map[string]any{"region": "us-east-1"},
		map[string]string{mcp.HeaderParamPrefix + "Region": "eu-west-1"},
	)
	errDetails := decodeJSONRPC(t, response)["error"].(map[string]any)
	assert.Equal(t, float64(mcp.HEADER_MISMATCH), errDetails["code"])
}

// TestParamHeaders_ASessionToolReplacedThroughSetSessionToolsIsCheckedAgainstItsNewSchema
// checks that a session tool swapped in through SetSessionTools is validated
// against its new schema, not the one it was added with.
func TestParamHeaders_ASessionToolReplacedThroughSetSessionToolsIsCheckedAgainstItsNewSchema(t *testing.T) {
	srv := NewMCPServer("header-test", "1.0.0", WithToolCapabilities(true))
	session := &sessionTestClientWithTools{
		sessionID:           "s",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
		initialized:         true,
	}
	require.NoError(t, srv.RegisterSession(t.Context(), session))
	require.NoError(t, srv.AddSessionTool("s", headerQueryTool(), nil))

	// User code may read the session's tools back, give one a new schema under
	// the same name and store the map again; the call must see the new schema.
	tools := session.GetSessionTools()
	entry := tools["query"]
	entry.Tool = mcp.NewToolWithRawSchema("query", "runs a query", json.RawMessage(`{
		"type": "object",
		"properties": {
			"region": { "type": "string" },
			"zone":   { "type": "string", "x-mcp-header": "Zone" }
		}
	}`))
	tools["query"] = entry
	session.SetSessionTools(tools)

	ctx := srv.WithContext(t.Context(), session)
	message := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"query","arguments":{"region":"r1","zone":"z1"}}}`)
	headers := http.Header{}
	headers.Set(mcp.HeaderProtocolVersion, mcp.ProtocolVersion20260728)
	headers.Set(mcp.HeaderMethod, string(mcp.MethodToolsCall))
	headers.Set(mcp.HeaderName, "query")
	headers.Set(mcp.HeaderParamPrefix+"Zone", "z1")
	assert.NoError(t, srv.validateStandardHeadersForMessage(ctx, headers, mcp.ProtocolVersion20260728, mcp.MethodToolsCall, message))

	headers.Del(mcp.HeaderParamPrefix + "Zone")
	err := srv.validateStandardHeadersForMessage(ctx, headers, mcp.ProtocolVersion20260728, mcp.MethodToolsCall, message)
	require.Error(t, err)
	assert.True(t, mcp.IsHeaderMismatch(err))
}
