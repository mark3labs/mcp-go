package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestStreamableHTTPServer_ClientErrorLabeledByMethod verifies that a JSON-RPC
// error returned by the client is surfaced with the method that actually
// failed. handleSamplingResponse is the shared response path for
// sampling/createMessage, elicitation/create and roots/list, so a generic
// "sampling error" prefix previously misattributed elicitation and roots
// failures (see #817).
func TestStreamableHTTPServer_ClientErrorLabeledByMethod(t *testing.T) {
	cases := []struct {
		name    string
		method  mcp.MCPMethod
		wantErr string
	}{
		{"sampling", mcp.MethodSamplingCreateMessage, "sampling/createMessage error -32601: Method not found"},
		{"elicitation", mcp.MethodElicitationCreate, "elicitation/create error -32601: Method not found"},
		{"roots", mcp.MethodListRoots, "roots/list error -32601: Method not found"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mcpServer := NewMCPServer("test-server", "1.0.0")
			httpServer := NewStreamableHTTPServer(mcpServer, WithStateLess(true)) // any session ID validates

			const sessionID = "test-session"
			session := newStreamableHttpSession(sessionID, nil, nil, nil, nil, new(atomic.Int64))
			httpServer.activeSessions.Store(sessionID, session)

			const requestID int64 = 1
			respChan := make(chan samplingResponseItem, 1)
			session.samplingRequests.Store(requestID, pendingClientRequest{
				method:   tc.method,
				response: respChan,
			})

			// Mirror the anonymous struct that handleSamplingResponse accepts.
			responseMessage := struct {
				ID     json.RawMessage `json:"id"`
				Result json.RawMessage `json:"result,omitempty"`
				Error  json.RawMessage `json:"error,omitempty"`
				Method mcp.MCPMethod   `json:"method,omitempty"`
			}{
				ID:    json.RawMessage("1"),
				Error: json.RawMessage(`{"code":-32601,"message":"Method not found"}`),
			}

			header := http.Header{}
			header.Set(HeaderKeySessionID, sessionID)
			r := &HTTPRequest{
				Method:  http.MethodPost,
				URL:     &url.URL{Path: "/mcp"},
				Header:  header,
				Context: t.Context(),
			}
			w := newBufferingHTTPResponseWriter()

			if err := httpServer.handleSamplingResponse(w, r, responseMessage); err != nil {
				t.Fatalf("handleSamplingResponse returned error: %v", err)
			}

			select {
			case resp := <-respChan:
				if resp.err == nil {
					t.Fatalf("expected a delivered error, got nil")
				}
				if got := resp.err.Error(); got != tc.wantErr {
					t.Errorf("error = %q, want %q", got, tc.wantErr)
				}
				if tc.method != mcp.MethodSamplingCreateMessage &&
					strings.HasPrefix(resp.err.Error(), "sampling error") {
					t.Errorf("%s failure was mislabeled as sampling: %q", tc.method, resp.err.Error())
				}
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for the delivered response")
			}
		})
	}
}
