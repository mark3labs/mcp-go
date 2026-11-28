package server

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestStreamableHTTP_MalformedProtocolVersionHeader(t *testing.T) {
	mcpServer := NewMCPServer("test-server", "1.0.0", WithToolCapabilities(true))
	ts := NewTestStreamableHTTPServer(mcpServer, WithStateful(true))
	t.Cleanup(ts.Close)

	sessionID := establishLegacySession(t, ts.URL)

	doRequest := func(t *testing.T, method, version, body string) (int, string) {
		t.Helper()
		var reader io.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		req, err := http.NewRequestWithContext(t.Context(), method, ts.URL, reader)
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(HeaderKeySessionID, sessionID)
		req.Header.Set(mcp.HeaderProtocolVersion, version)

		resp, err := ts.Client().Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		payload, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, string(payload)
	}

	tests := []struct {
		name   string
		method string
		body   string
	}{
		{name: "POST request", method: http.MethodPost, body: `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`},
		{name: "POST client response", method: http.MethodPost, body: `{"jsonrpc":"2.0","id":1,"result":{}}`},
		{name: "GET", method: http.MethodGet},
		{name: "DELETE", method: http.MethodDelete},
	}

	for _, version := range []string{"invalid", "2026-99-99"} {
		for _, tt := range tests {
			t.Run(version+"/"+tt.name, func(t *testing.T) {
				status, payload := doRequest(t, tt.method, version, tt.body)
				assert.Equal(t, http.StatusBadRequest, status, payload)
				assert.Contains(t, payload, "Unsupported protocol version: "+version)
			})
		}
	}

	t.Run("valid modern version keeps 405 on GET", func(t *testing.T) {
		status, payload := doRequest(t, http.MethodGet, mcp.ProtocolVersion20260728, "")
		assert.Equal(t, http.StatusMethodNotAllowed, status, payload)
	})
}
