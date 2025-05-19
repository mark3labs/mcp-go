package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOriginHeaderValidation(t *testing.T) {
	// Create a simple MCP server
	mcpServer := NewMCPServer("test-server", "1.0.0")

	// Create a Streamable HTTP server with an origin allowlist
	allowlist := []string{"https://example.com", "*.trusted-domain.com"}
	streamableServer := NewStreamableHTTPServer(mcpServer, WithOriginAllowlist(allowlist))

	// Create a test HTTP server
	server := httptest.NewServer(streamableServer)
	defer server.Close()

	// Test cases
	testCases := []struct {
		name           string
		origin         string
		expectedStatus int
	}{
		{"Valid origin - exact match", "https://example.com", http.StatusOK},
		{"Valid origin - wildcard match", "https://api.trusted-domain.com", http.StatusOK},
		{"Valid origin - localhost", "http://localhost:3000", http.StatusOK},
		{"Invalid origin", "https://attacker.com", http.StatusForbidden},
		{"No origin header", "", http.StatusOK}, // No origin header should be allowed
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a JSON-RPC request
			requestBody := `{"jsonrpc":"2.0","method":"initialize","id":1,"params":{}}`

			// Create an HTTP request
			req, err := http.NewRequest("POST", server.URL+"/mcp", strings.NewReader(requestBody))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			// Set headers
			req.Header.Set("Content-Type", "application/json")
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}

			// Send the request
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Failed to send request: %v", err)
			}
			defer resp.Body.Close()

			// Check the status code
			if resp.StatusCode != tc.expectedStatus {
				t.Errorf("Expected status code %d, got %d", tc.expectedStatus, resp.StatusCode)
			}
		})
	}
}
