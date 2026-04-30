package server

import (
	"encoding/json"
	"net/http"
)

// OAuthProtectedResourceMetadata holds OAuth 2.0 Protected Resource Metadata
// as defined in RFC 9728. Served at /.well-known/oauth-protected-resource.
type OAuthProtectedResourceMetadata struct {
	// Resource is the protected resource's identifier URI (REQUIRED).
	Resource string `json:"resource"`

	// AuthorizationServers lists authorization server issuer URLs (RECOMMENDED).
	AuthorizationServers []string `json:"authorization_servers,omitempty"`

	// ScopesSupported lists OAuth scopes supported by this resource (OPTIONAL).
	ScopesSupported []string `json:"scopes_supported,omitempty"`

	// BearerMethodsSupported lists supported Bearer token presentation methods (OPTIONAL).
	BearerMethodsSupported []string `json:"bearer_methods_supported,omitempty"`

	// ResourceName is a human-readable name for the resource (OPTIONAL).
	ResourceName *string `json:"resource_name,omitempty"`

	// ResourceDocumentation is a URL pointing to documentation for this resource (OPTIONAL).
	ResourceDocumentation *string `json:"resource_documentation,omitempty"`

	// JwksURI is the URL of the JSON Web Key Set for verifying resource responses (OPTIONAL).
	JwksURI *string `json:"jwks_uri,omitempty"`

	// ResourceSigningAlgsSupported lists supported JWS signing algorithms (OPTIONAL).
	ResourceSigningAlgsSupported []string `json:"resource_signing_alg_values_supported,omitempty"`

	// ResourcePolicyURI is the URL of the resource's privacy policy (OPTIONAL).
	ResourcePolicyURI *string `json:"resource_policy_uri,omitempty"`

	// ResourceTosURI is the URL of the resource's terms of service (OPTIONAL).
	ResourceTosURI *string `json:"resource_tos_uri,omitempty"`

	// TLSClientCertificateBoundAccessTokens indicates support for mTLS-bound tokens (OPTIONAL).
	TLSClientCertificateBoundAccessTokens *bool `json:"tls_client_certificate_bound_access_tokens,omitempty"`

	// AuthorizationDetailsTypesSupported lists supported authorization_details types (OPTIONAL).
	AuthorizationDetailsTypesSupported []string `json:"authorization_details_types_supported,omitempty"`

	// DPoPSigningAlgsSupported lists JWS algorithms supported for DPoP proofs (OPTIONAL).
	DPoPSigningAlgsSupported []string `json:"dpop_signing_alg_values_supported,omitempty"`

	// DPoPBoundAccessTokensRequired indicates whether DPoP-bound tokens are required (OPTIONAL).
	DPoPBoundAccessTokensRequired *bool `json:"dpop_bound_access_tokens_required,omitempty"`
}

// NewProtectedResourceMetadataHandler returns an http.Handler that serves RFC 9728
// OAuth 2.0 Protected Resource Metadata at /.well-known/oauth-protected-resource.
//
// Use this for custom routing setups where you manage your own http.ServeMux:
//
//	mux.Handle("/.well-known/oauth-protected-resource",
//	    server.NewProtectedResourceMetadataHandler(meta))
func NewProtectedResourceMetadataHandler(metadata OAuthProtectedResourceMetadata) http.Handler {
	if metadata.Resource == "" {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "protected resource metadata: Resource field is required", http.StatusInternalServerError)
		})
	}

	// Pre-marshal once at construction time; the struct only contains
	// JSON-serialisable types so this should never fail.
	data, err := json.Marshal(metadata)
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "failed to marshal protected resource metadata", http.StatusInternalServerError)
		})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodOptions {
			w.Header().Set("Allow", "GET, OPTIONS")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})
}

// WithProtectedResourceMetadata registers the /.well-known/oauth-protected-resource endpoint
// when Start creates its internal mux. Has no effect when WithStreamableHTTPServer is used;
// in that case register the handler manually via NewProtectedResourceMetadataHandler.
func WithProtectedResourceMetadata(metadata OAuthProtectedResourceMetadata) StreamableHTTPOption {
	return func(s *StreamableHTTPServer) {
		s.protectedResourceMetadata = &metadata
	}
}

// WithSSEProtectedResourceMetadata registers the /.well-known/oauth-protected-resource endpoint
// when the SSE server's Start creates its internal http.Server. Has no effect when
// WithHTTPServer is used; register the handler manually in that case.
func WithSSEProtectedResourceMetadata(metadata OAuthProtectedResourceMetadata) SSEOption {
	return func(s *SSEServer) {
		s.protectedResourceMetadata = &metadata
	}
}