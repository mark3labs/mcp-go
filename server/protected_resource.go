package server

import (
	"encoding/json"
	"net/http"
)

// ProtectedResourceMetadata represents OAuth 2.0 Protected Resource Metadata
// as defined in RFC 9728 (https://datatracker.ietf.org/doc/html/rfc9728).
//
// MCP servers MUST implement this to indicate the locations of authorization servers.
type ProtectedResourceMetadata struct {
	// Resource is the protected resource's resource identifier. Required.
	Resource string `json:"resource"`

	// AuthorizationServers is a list of OAuth authorization server issuer identifiers
	// that can be used with this protected resource.
	AuthorizationServers []string `json:"authorization_servers,omitempty"`

	// ScopesSupported is a list of scope values used in authorization requests
	// to request access to this protected resource.
	ScopesSupported []string `json:"scopes_supported,omitempty"`

	// BearerMethodsSupported is a list of the supported methods of sending
	// an OAuth 2.0 bearer token to the protected resource.
	BearerMethodsSupported []string `json:"bearer_methods_supported,omitempty"`

	// ResourceName is a human-readable name of the protected resource.
	ResourceName string `json:"resource_name,omitempty"`

	// ResourceDocumentation is a URL of a page containing human-readable
	// information for developers using the protected resource.
	ResourceDocumentation string `json:"resource_documentation,omitempty"`

	// JwksURI is the URL of the protected resource's JWK Set document.
	JwksURI string `json:"jwks_uri,omitempty"`
}

// ProtectedResourceMetadataHandler returns an http.Handler that serves
// OAuth 2.0 Protected Resource Metadata (RFC 9728) at the well-known endpoint.
//
// This handler supports CORS for cross-origin client discovery, and responds
// to GET and OPTIONS requests. All other methods receive 405 Method Not Allowed.
func ProtectedResourceMetadataHandler(metadata *ProtectedResourceMetadata) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(metadata); err != nil {
			http.Error(w, "Failed to encode metadata", http.StatusInternalServerError)
			return
		}
	})
}
