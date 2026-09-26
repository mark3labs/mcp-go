package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Dynamic client registration must declare an application_type (SEP-837). An
// OIDC authorization server otherwise assumes "web" and may reject a loopback
// redirect URI.
func TestOAuthHandler_RegisterClientSendsApplicationType(t *testing.T) {
	tests := []struct {
		redirectURI string
		want        string
	}{
		{redirectURI: "http://localhost:8085/callback", want: "native"},
		{redirectURI: "http://127.0.0.1:8085/callback", want: "native"},
		{redirectURI: "http://[::1]:8085/callback", want: "native"},
		{redirectURI: "com.example.app:/oauth/callback", want: "native"},
		{redirectURI: "https://app.example.com/callback", want: "web"},
		// OIDC doesn't allow a native client an https loopback redirect URI.
		{redirectURI: "https://localhost:8085/callback", want: "web"},
		{redirectURI: "http://app.example.com/callback", want: "web"},
	}
	for _, tt := range tests {
		t.Run(tt.redirectURI, func(t *testing.T) {
			var registration map[string]any
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/.well-known/oauth-authorization-server":
					_ = json.NewEncoder(w).Encode(AuthServerMetadata{
						Issuer:                server.URL,
						AuthorizationEndpoint: server.URL + "/authorize",
						TokenEndpoint:         server.URL + "/token",
						RegistrationEndpoint:  server.URL + "/register",
					})
				case "/register":
					if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
						t.Errorf("decoding registration request: %v", err)
					}
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "registered-client"})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			handler := NewOAuthHandler(OAuthConfig{
				RedirectURI:           tt.redirectURI,
				TokenStore:            NewMemoryTokenStore(),
				AuthServerMetadataURL: server.URL + "/.well-known/oauth-authorization-server",
				PKCEEnabled:           true,
			})
			require.NoError(t, handler.RegisterClient(t.Context(), "test-client"))

			assert.Equal(t, tt.want, registration["application_type"])
			assert.Equal(t, "registered-client", handler.GetClientID())
		})
	}
}
