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
	"strings"
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

// TestHostAllowed verifies Host header allowlist matching: case
// insensitivity, port handling (including default ports) and IP literal
// normalization.
func TestHostAllowed(t *testing.T) {
	tests := []struct {
		name         string
		reqHost      string
		allowedHosts []string
		want         bool
	}{
		{name: "no allowlist", reqHost: "mcp.example.com", want: false},
		{name: "bare entry matches bare host", reqHost: "mcp.example.com", allowedHosts: []string{"mcp.example.com"}, want: true},
		{name: "bare entry matches any port", reqHost: "mcp.example.com:9090", allowedHosts: []string{"mcp.example.com"}, want: true},
		{name: "case insensitive", reqHost: "MCP.Example.COM:9090", allowedHosts: []string{" mcp.EXAMPLE.com "}, want: true},
		{name: "port entry matches same port", reqHost: "mcp.example.com:9090", allowedHosts: []string{"mcp.example.com:9090"}, want: true},
		{name: "port entry rejects other port", reqHost: "mcp.example.com:9091", allowedHosts: []string{"mcp.example.com:9090"}, want: false},
		{name: "port entry rejects bare host", reqHost: "mcp.example.com", allowedHosts: []string{"mcp.example.com:9090"}, want: false},
		{name: "default https port matches bare host", reqHost: "mcp.example.com", allowedHosts: []string{"mcp.example.com:443"}, want: true},
		{name: "default http port matches bare host", reqHost: "mcp.example.com", allowedHosts: []string{"mcp.example.com:80"}, want: true},
		{name: "default port entry rejects other explicit port", reqHost: "mcp.example.com:8443", allowedHosts: []string{"mcp.example.com:443"}, want: false},
		{name: "other host rejected", reqHost: "evil.com", allowedHosts: []string{"mcp.example.com"}, want: false},
		{name: "suffix match rejected", reqHost: "mcp.example.com.evil.com", allowedHosts: []string{"mcp.example.com"}, want: false},
		{name: "matches any entry", reqHost: "b.example.com", allowedHosts: []string{"a.example.com", "b.example.com"}, want: true},
		{name: "empty entries ignored", reqHost: "evil.com", allowedHosts: []string{"", "  "}, want: false},
		{name: "empty host rejected", reqHost: "", allowedHosts: []string{"mcp.example.com", ""}, want: false},
		{name: "ip entry", reqHost: "192.168.1.10:8080", allowedHosts: []string{"192.168.1.10"}, want: true},
		{name: "bracketed ipv6 host and entry", reqHost: "[fe80::1]", allowedHosts: []string{"[fe80::1]"}, want: true},
		{name: "bracketed ipv6 host, unbracketed entry", reqHost: "[fe80::1]", allowedHosts: []string{"fe80::1"}, want: true},
		{name: "ipv6 host with port, bracketed entry", reqHost: "[fe80::1]:8080", allowedHosts: []string{"[fe80::1]"}, want: true},
		{name: "ipv6 host with port, unbracketed entry", reqHost: "[fe80::1]:8080", allowedHosts: []string{"fe80::1"}, want: true},
		{name: "expanded ipv6 host matches compressed entry", reqHost: "[2001:0db8:0:0:0:0:0:1]:8080", allowedHosts: []string{"2001:db8::1"}, want: true},
		{name: "compressed ipv6 host matches expanded entry", reqHost: "[2001:db8::1]", allowedHosts: []string{"[2001:0DB8::0001]"}, want: true},
		{name: "ipv6 port entry rejects other port", reqHost: "[fe80::1]:8081", allowedHosts: []string{"[fe80::1]:8080"}, want: false},
		{name: "different ipv6 rejected", reqHost: "[fe80::2]", allowedHosts: []string{"fe80::1"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, hostAllowed(tt.reqHost, tt.allowedHosts), "hostAllowed(%q, %q)", tt.reqHost, tt.allowedHosts)
		})
	}
}

// withLocalAddr returns r with http.LocalAddrContextKey set to ip, simulating
// a request that net/http delivered on that interface.
func withLocalAddr(r *http.Request, ip string) *http.Request {
	localAddr := &net.TCPAddr{IP: net.ParseIP(ip), Port: 8080}
	return r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, localAddr))
}

// TestRejectDNSRebinding verifies rejectDNSRebinding for loopback,
// non-loopback and missing local addresses, with and without an allowlist.
func TestRejectDNSRebinding(t *testing.T) {
	tests := []struct {
		name          string
		localIP       string
		hostHeader    string
		allowedHosts  []string
		wantForbidden bool
	}{
		{name: "no allowlist, loopback conn, loopback host", localIP: "127.0.0.1", hostHeader: "localhost:8080"},
		{name: "no allowlist, loopback conn, foreign host", localIP: "127.0.0.1", hostHeader: "evil.com", wantForbidden: true},
		{name: "no allowlist, non-loopback conn, foreign host", localIP: "192.168.1.10", hostHeader: "evil.com"},
		{name: "no allowlist, no local addr, foreign host", hostHeader: "evil.com"},
		{name: "allowlist, non-loopback conn, foreign host", localIP: "192.168.1.10", hostHeader: "evil.com", allowedHosts: []string{"mcp.example.com"}, wantForbidden: true},
		{name: "allowlist, non-loopback conn, allowed host", localIP: "192.168.1.10", hostHeader: "mcp.example.com:8080", allowedHosts: []string{"mcp.example.com"}},
		{name: "allowlist, non-loopback conn, loopback host", localIP: "192.168.1.10", hostHeader: "127.0.0.1:8080", allowedHosts: []string{"mcp.example.com"}},
		{name: "allowlist, loopback conn, allowed host", localIP: "127.0.0.1", hostHeader: "mcp.example.com", allowedHosts: []string{"mcp.example.com"}},
		{name: "allowlist, loopback conn, foreign host", localIP: "::1", hostHeader: "evil.com", allowedHosts: []string{"mcp.example.com"}, wantForbidden: true},
		{name: "allowlist, no local addr, foreign host", hostHeader: "evil.com", allowedHosts: []string{"mcp.example.com"}, wantForbidden: true},
		{name: "allowlist, no local addr, allowed host", hostHeader: "mcp.example.com", allowedHosts: []string{"mcp.example.com"}},
		{name: "empty-entry allowlist, non-loopback conn, foreign host", localIP: "192.168.1.10", hostHeader: "evil.com", allowedHosts: []string{""}, wantForbidden: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			req.Host = tt.hostHeader
			if tt.localIP != "" {
				req = withLocalAddr(req, tt.localIP)
			}

			rr := httptest.NewRecorder()
			got := rejectDNSRebinding(rr, req, tt.allowedHosts)

			assert.Equal(t, tt.wantForbidden, got)
			if tt.wantForbidden {
				assert.Equal(t, http.StatusForbidden, rr.Code)
				assert.Contains(t, rr.Body.String(), "invalid Host header")
			} else {
				assert.Zero(t, rr.Body.Len(), "nothing should be written for accepted requests")
			}
		})
	}
}

// allowedHostsTests is the shared table for the transport-level allowlist
// tests below. Every request arrives on a non-loopback interface.
var allowedHostsTests = []struct {
	name              string
	hostHeader        string
	allowedHosts      []string
	disableProtection bool
	wantForbidden     bool
}{
	{
		name:       "no allowlist accepts foreign host",
		hostHeader: "evil.com",
	},
	{
		name:          "allowlist rejects foreign host",
		hostHeader:    "evil.com",
		allowedHosts:  []string{"mcp.example.com"},
		wantForbidden: true,
	},
	{
		name:         "allowlist accepts allowed host",
		hostHeader:   "mcp.example.com:8080",
		allowedHosts: []string{"mcp.example.com"},
	},
	{
		name:         "allowlist accepts loopback host",
		hostHeader:   "localhost:8080",
		allowedHosts: []string{"mcp.example.com"},
	},
	{
		name:              "disabled protection ignores allowlist",
		hostHeader:        "evil.com",
		allowedHosts:      []string{"mcp.example.com"},
		disableProtection: true,
	},
}

// TestStreamableHTTP_AllowedHosts verifies that WithAllowedHosts enforces the
// Host header allowlist on requests arriving via non-loopback addresses.
func TestStreamableHTTP_AllowedHosts(t *testing.T) {
	for _, tt := range allowedHostsTests {
		t.Run(tt.name, func(t *testing.T) {
			mcpServer := NewMCPServer("test", "1.0.0")
			httpServer := NewStreamableHTTPServer(mcpServer,
				WithStateLess(true),
				WithAllowedHosts(tt.allowedHosts...),
				WithDisableLocalhostProtection(tt.disableProtection),
			)

			body, err := json.Marshal(initRequest)
			require.NoError(t, err)
			req, err := http.NewRequest(http.MethodPost, "http://192.168.1.10:8080/mcp", bytes.NewReader(body))
			require.NoError(t, err)
			req.Host = tt.hostHeader
			req.Header.Set("Content-Type", "application/json")
			req = withLocalAddr(req, "192.168.1.10")

			rr := httptest.NewRecorder()
			httpServer.ServeHTTP(rr, req)

			if tt.wantForbidden {
				assert.Equal(t, http.StatusForbidden, rr.Code)
				assert.Contains(t, rr.Body.String(), "invalid Host header")
			} else {
				assert.Equal(t, http.StatusOK, rr.Code)
			}
		})
	}
}

// TestSSE_AllowedHosts verifies that WithSSEAllowedHosts enforces the Host
// header allowlist on requests arriving via non-loopback addresses, for
// ServeHTTP as well as the SSEHandler and MessageHandler sub-handlers.
func TestSSE_AllowedHosts(t *testing.T) {
	for _, tt := range allowedHostsTests {
		t.Run(tt.name, func(t *testing.T) {
			mcpServer := NewMCPServer("test", "1.0.0")
			sseServer := NewSSEServer(mcpServer,
				WithSSEAllowedHosts(tt.allowedHosts...),
				WithSSEDisableLocalhostProtection(tt.disableProtection),
			)

			// Every request is a POST: the message endpoint answers 400 Bad
			// Request for the missing sessionId, and the SSE endpoint answers
			// 405 Method Not Allowed without opening an event stream. Either
			// distinguishes normal handling from the 403 protection response.
			handlers := []struct {
				name        string
				handler     http.Handler
				path        string
				wantAllowed int
			}{
				{name: "ServeHTTP", handler: sseServer, path: "/message", wantAllowed: http.StatusBadRequest},
				{name: "SSEHandler", handler: sseServer.SSEHandler(), path: "/sse", wantAllowed: http.StatusMethodNotAllowed},
				{name: "MessageHandler", handler: sseServer.MessageHandler(), path: "/message", wantAllowed: http.StatusBadRequest},
			}
			for _, h := range handlers {
				t.Run(h.name, func(t *testing.T) {
					req, err := http.NewRequest(http.MethodPost, "http://192.168.1.10:8080"+h.path, bytes.NewReader(nil))
					require.NoError(t, err)
					req.Host = tt.hostHeader
					req = withLocalAddr(req, "192.168.1.10")

					rr := httptest.NewRecorder()
					h.handler.ServeHTTP(rr, req)

					if tt.wantForbidden {
						assert.Equal(t, http.StatusForbidden, rr.Code)
					} else {
						assert.Equal(t, h.wantAllowed, rr.Code)
					}
				})
			}
		})
	}
}

// openSSESession opens an SSE stream on the loopback server at addr and
// returns the session ID announced in its endpoint event. The stream stays
// open, keeping the session registered, until the test ends.
func openSSESession(t *testing.T, client *http.Client, addr string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+addr+"/sse", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)

	event, err := readSSEEvent(resp)
	require.NoError(t, err)
	_, endpoint, ok := strings.Cut(event, "data: ")
	require.True(t, ok, "expected an endpoint event, got %q", event)
	_, sessionID, ok := strings.Cut(strings.TrimSpace(endpoint), "sessionId=")
	require.True(t, ok, "expected a sessionId in endpoint %q", endpoint)
	return sessionID
}

// TestSSE_ForeignHostRejectedForStreamsAndSessions verifies that a foreign
// Host is rejected for SSE stream requests and for messages that carry a
// valid session, through ServeHTTP and the SSEHandler and MessageHandler
// sub-handlers, under both the default loopback protection and an allowlist
// on a non-loopback connection.
func TestSSE_ForeignHostRejectedForStreamsAndSessions(t *testing.T) {
	tests := []struct {
		name        string
		opts        []SSEOption
		localIP     string
		allowedHost string
	}{
		{
			name:        "loopback connection",
			localIP:     "127.0.0.1",
			allowedHost: "localhost:8080",
		},
		{
			name:        "allowlist on non-loopback connection",
			opts:        []SSEOption{WithSSEAllowedHosts("mcp.example.com")},
			localIP:     "192.168.1.10",
			allowedHost: "mcp.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sseServer := NewSSEServer(NewMCPServer("test", "1.0.0"), tt.opts...)
			addr, client := startLoopbackServer(t, sseServer)
			messagePath := "/message?sessionId=" + openSSESession(t, client, addr)
			ping := `{"jsonrpc":"2.0","id":1,"method":"ping"}`

			// serve sends a request with the given Host straight to handler,
			// as if it arrived on tt.localIP. The timeout bounds the stream a
			// regression would open instead of answering 403.
			serve := func(t *testing.T, handler http.Handler, method, path, host, body string) int {
				t.Helper()
				ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
				defer cancel()
				target := "http://" + net.JoinHostPort(tt.localIP, "8080") + path
				req, err := http.NewRequestWithContext(ctx, method, target, strings.NewReader(body))
				require.NoError(t, err)
				req.Host = host
				rr := httptest.NewRecorder()
				handler.ServeHTTP(rr, withLocalAddr(req, tt.localIP))
				return rr.Code
			}

			// wantAllowed is the status for an allowed Host. It is zero for
			// stream requests, which would keep streaming once accepted;
			// openSSESession already shows a stream opening.
			requests := []struct {
				name        string
				handler     http.Handler
				method      string
				path        string
				body        string
				wantAllowed int
			}{
				{name: "GET stream via ServeHTTP", handler: sseServer, method: http.MethodGet, path: "/sse"},
				{name: "GET stream via SSEHandler", handler: sseServer.SSEHandler(), method: http.MethodGet, path: "/sse"},
				{name: "POST with session via ServeHTTP", handler: sseServer, method: http.MethodPost, path: messagePath, body: ping, wantAllowed: http.StatusAccepted},
				{name: "POST with session via MessageHandler", handler: sseServer.MessageHandler(), method: http.MethodPost, path: messagePath, body: ping, wantAllowed: http.StatusAccepted},
			}
			for _, tc := range requests {
				t.Run(tc.name, func(t *testing.T) {
					assert.Equal(t, http.StatusForbidden, serve(t, tc.handler, tc.method, tc.path, "evil.com", tc.body))
					if tc.wantAllowed != 0 {
						assert.Equal(t, tc.wantAllowed, serve(t, tc.handler, tc.method, tc.path, tt.allowedHost, tc.body),
							"the same request with an allowed Host must succeed, so the 403 comes from the Host check")
					}
				})
			}
		})
	}
}
