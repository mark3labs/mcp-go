package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsLoopbackHost(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"localhost", true},
		{"LocalHost", true},
		{"localhost:3000", true},
		{"127.0.0.1", true},
		{"127.0.0.1:3000", true},
		{"127.1.2.3:3000", true},
		{"[::1]", true},
		{"[::1]:3000", true},
		{"::1", true},
		{"", false},
		{"evil.com", false},
		{"evil.com:80", false},
		{"localhost.evil.com", false},
		{"127.0.0.1.evil.com", false},
		{"192.168.1.10:8080", false},
		{"[2001:db8::1]:8080", false},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			assert.Equal(t, tt.want, isLoopbackHost(tt.addr), "isLoopbackHost(%q)", tt.addr)
		})
	}
}

// localhostProtectionTests is the shared table for the transport-level DNS
// rebinding protection tests below.
var localhostProtectionTests = []struct {
	name              string
	hostHeader        string
	disableProtection bool
	wantForbidden     bool
}{
	{
		name:       "accepts 127.0.0.1 host header",
		hostHeader: "127.0.0.1:1234",
	},
	{
		name:       "accepts localhost host header",
		hostHeader: "localhost:1234",
	},
	{
		name:       "accepts [::1] host header",
		hostHeader: "[::1]:1234",
	},
	{
		name:          "rejects evil.com",
		hostHeader:    "evil.com",
		wantForbidden: true,
	},
	{
		name:          "rejects evil.com:80",
		hostHeader:    "evil.com:80",
		wantForbidden: true,
	},
	{
		name:          "rejects localhost.evil.com",
		hostHeader:    "localhost.evil.com",
		wantForbidden: true,
	},
	{
		name:              "disabled accepts evil.com",
		hostHeader:        "evil.com",
		disableProtection: true,
	},
}

// startLoopbackServer serves handler on a real loopback listener so that
// http.LocalAddrContextKey reflects an actual 127.0.0.1 connection. The
// server and the returned client both carry explicit timeouts so a stalled
// handler or connection cannot hang the test run.
func startLoopbackServer(t *testing.T, handler http.Handler) (addr string, client *http.Client) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
	}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close() })
	return listener.Addr().String(), &http.Client{Timeout: 5 * time.Second}
}

// TestStreamableHTTP_LocalhostProtection verifies that DNS rebinding
// protection is automatically enabled for the streamable HTTP transport when
// requests arrive over a loopback connection.
func TestStreamableHTTP_LocalhostProtection(t *testing.T) {
	for _, tt := range localhostProtectionTests {
		t.Run(tt.name, func(t *testing.T) {
			mcpServer := NewMCPServer("test", "1.0.0")
			httpServer := NewStreamableHTTPServer(mcpServer,
				WithStateLess(true),
				WithDisableLocalhostProtection(tt.disableProtection),
			)
			addr, client := startLoopbackServer(t, httpServer)

			body, err := json.Marshal(initRequest)
			require.NoError(t, err)

			req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/mcp", addr), bytes.NewReader(body))
			require.NoError(t, err)
			req.Host = tt.hostHeader
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			if tt.wantForbidden {
				assert.Equal(t, http.StatusForbidden, resp.StatusCode)
				payload, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				assert.Contains(t, string(payload), "invalid Host header")
			} else {
				assert.Equal(t, http.StatusOK, resp.StatusCode)
			}
		})
	}
}

// TestSSE_LocalhostProtection verifies that DNS rebinding protection is
// automatically enabled for the SSE transport when requests arrive over a
// loopback connection.
func TestSSE_LocalhostProtection(t *testing.T) {
	for _, tt := range localhostProtectionTests {
		t.Run(tt.name, func(t *testing.T) {
			mcpServer := NewMCPServer("test", "1.0.0")
			sseServer := NewSSEServer(mcpServer,
				WithSSEDisableLocalhostProtection(tt.disableProtection),
			)
			addr, client := startLoopbackServer(t, sseServer)

			// A POST to the message endpoint without a session is enough to
			// distinguish the 403 protection response from normal handling
			// (400 Bad Request for the missing sessionId).
			req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/message", addr), bytes.NewReader(nil))
			require.NoError(t, err)
			req.Host = tt.hostHeader

			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			if tt.wantForbidden {
				assert.Equal(t, http.StatusForbidden, resp.StatusCode)
			} else {
				assert.NotEqual(t, http.StatusForbidden, resp.StatusCode)
			}
		})
	}
}

// TestStreamableHTTP_LocalhostProtection_NonLoopbackUnaffected verifies that
// the protection only applies to loopback connections: a handler invoked
// without a loopback local address must not reject foreign Host headers.
func TestStreamableHTTP_LocalhostProtection_NonLoopbackUnaffected(t *testing.T) {
	mcpServer := NewMCPServer("test", "1.0.0")
	httpServer := NewStreamableHTTPServer(mcpServer, WithStateLess(true))

	// Simulate a request that arrived on a non-loopback interface.
	body, err := json.Marshal(initRequest)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, "http://evil.com/mcp", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	localAddr := &net.TCPAddr{IP: net.ParseIP("192.168.1.10"), Port: 8080}
	req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, localAddr))

	rr := httptest.NewRecorder()
	httpServer.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}
