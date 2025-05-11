package server

import (
	"testing"
)

func TestOriginValidation(t *testing.T) {
	tests := []struct {
		name      string
		origin    string
		allowlist []string
		expected  bool
	}{
		{"Empty origin", "", []string{"https://example.com"}, false},
		{"Exact match", "https://example.com", []string{"https://example.com"}, true},
		{"No match", "https://evil.com", []string{"https://example.com"}, false},
		{"Subdomain wildcard", "https://sub.example.com", []string{"*.example.com"}, true},
		{"Subdomain wildcard - multiple levels", "https://a.b.example.com", []string{"*.example.com"}, true},
		{"Subdomain wildcard - no match", "https://examplefake.com", []string{"*.example.com"}, false},
		{"Localhost allowed", "http://localhost:3000", []string{}, true},
		{"127.0.0.1 allowed", "http://127.0.0.1:8080", []string{}, true},
		{"Multiple allowlist entries", "https://api.example.com", []string{"https://app.example.com", "https://api.example.com"}, true},
		{"Empty allowlist", "https://example.com", []string{}, true}, // Should allow all when no allowlist is configured
		{"Invalid URL", "://invalid-url", []string{}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := &StreamableHTTPServer{originAllowlist: tc.allowlist}
			result := server.isValidOrigin(tc.origin)
			if result != tc.expected {
				t.Errorf("isValidOrigin(%q) with allowlist %v = %v; want %v",
					tc.origin, tc.allowlist, result, tc.expected)
			}
		})
	}
}

func TestWithOriginAllowlist(t *testing.T) {
	// Create a test server with an allowlist
	allowlist := []string{"https://example.com", "*.trusted-domain.com"}
	mcpServer := NewMCPServer("test-server", "1.0.0")
	server := NewStreamableHTTPServer(mcpServer, WithOriginAllowlist(allowlist))

	// Verify the allowlist was set correctly
	if len(server.originAllowlist) != len(allowlist) {
		t.Errorf("Expected allowlist length %d, got %d", len(allowlist), len(server.originAllowlist))
	}

	// Check that the values match
	for i, origin := range allowlist {
		if server.originAllowlist[i] != origin {
			t.Errorf("Expected allowlist[%d] = %q, got %q", i, origin, server.originAllowlist[i])
		}
	}

	// Test that the validation works with the configured allowlist
	validOrigins := []string{
		"https://example.com",
		"https://sub.trusted-domain.com",
		"http://localhost:3000",
	}

	invalidOrigins := []string{
		"https://attacker.com",
		"https://trusted-domain.com", // This doesn't match *.trusted-domain.com (needs a subdomain)
		"https://fake-example.com",
	}

	for _, origin := range validOrigins {
		if !server.isValidOrigin(origin) {
			t.Errorf("Expected origin %q to be valid, but it was rejected", origin)
		}
	}

	for _, origin := range invalidOrigins {
		if server.isValidOrigin(origin) {
			t.Errorf("Expected origin %q to be invalid, but it was accepted", origin)
		}
	}
}
