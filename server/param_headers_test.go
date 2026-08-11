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
