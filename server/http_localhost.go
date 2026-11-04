package server

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// isLoopbackHost reports whether addr refers to a loopback interface. addr
// may be a bare host ("localhost", "127.0.0.1", "::1", "[::1]") or a
// host:port pair ("localhost:3000", "127.0.0.1:3000", "[::1]:3000").
//
// It is used to implement DNS rebinding protection: a request that arrives
// on a loopback connection must also carry a loopback Host header, otherwise
// a malicious website could rebind its own domain to 127.0.0.1 and drive a
// victim's browser to interact with a local MCP server.
func isLoopbackHost(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// addr might be a bare host without a port.
		host = strings.Trim(addr, "[]")
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return ip.IsLoopback()
}

// splitAuthority splits a Host header value or allowlist entry into a
// lowercased host and an optional port. IPv6 literals are returned without
// brackets, so "[fe80::1]", "fe80::1" and "[fe80::1]:8080" all yield the host
// "fe80::1".
func splitAuthority(authority string) (host, port string) {
	authority = strings.ToLower(strings.TrimSpace(authority))
	host, port, err := net.SplitHostPort(authority)
	if err != nil {
		return strings.Trim(authority, "[]"), ""
	}
	return host, port
}

// hostAllowed reports whether reqHost, a request's Host header, matches one
// of allowedHosts. Matching is case-insensitive. An entry without a port
// ("mcp.example.com") matches that host on any port. An entry with a port
// ("mcp.example.com:9090") matches only that exact authority; because
// browsers omit default ports from the Host header, a Host without a port
// also matches entries for port 80 or 443. Empty entries never match.
func hostAllowed(reqHost string, allowedHosts []string) bool {
	reqName, reqPort := splitAuthority(reqHost)
	if reqName == "" {
		return false
	}
	for _, entry := range allowedHosts {
		name, port := splitAuthority(entry)
		if name == "" || name != reqName {
			continue
		}
		switch {
		case port == "", port == reqPort:
			return true
		case reqPort == "" && (port == "80" || port == "443"):
			return true
		}
	}
	return false
}

// rejectDNSRebinding applies DNS rebinding protection to r. If the request's
// Host header is a loopback value or matches allowedHosts, r is accepted.
// Otherwise r is rejected with 403 Forbidden, and true is returned, when
// either an allowlist is configured (it is then enforced for every request,
// regardless of the connection's local address) or the connection's local
// address (http.LocalAddrContextKey) is a loopback address.
//
// With no allowlist configured, requests arriving via non-loopback addresses
// are never rejected. DNS rebinding can also target a server reachable at a
// LAN or otherwise routable address, so servers bound to a non-loopback
// interface should configure an allowlist (WithAllowedHosts /
// WithSSEAllowedHosts) to be protected.
//
// See https://modelcontextprotocol.io/specification/2025-11-25/basic/security_best_practices#local-mcp-server-compromise
func rejectDNSRebinding(w http.ResponseWriter, r *http.Request, allowedHosts []string) bool {
	if isLoopbackHost(r.Host) || hostAllowed(r.Host, allowedHosts) {
		return false
	}
	if len(allowedHosts) == 0 {
		localAddr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
		if !ok || localAddr == nil || !isLoopbackHost(localAddr.String()) {
			return false
		}
	}
	http.Error(w, fmt.Sprintf("Forbidden: invalid Host header %q", r.Host), http.StatusForbidden)
	return true
}
