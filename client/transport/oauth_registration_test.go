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
		{redirectURI: "HTTP://localhost:8085/CallBack", want: "native"},
		{redirectURI: "http://LOCALHOST:8085/CallBack", want: "native"},
		{redirectURI: "http://LocalHost:8085/CallBack", want: "native"},
		{redirectURI: "HtTp://LoCaLhOsT:8085/CallBack?State=AbC", want: "native"},
		{redirectURI: "http://127.0.0.1:8085/callback", want: "native"},
		{redirectURI: "HTTP://127.0.0.1:8085/CallBack", want: "native"},
		{redirectURI: "http://[::1]:8085/callback", want: "native"},
		{redirectURI: "HtTp://[::1]:8085/CallBack", want: "native"},
		{redirectURI: "com.example.app:/oauth/callback", want: "native"},
		{redirectURI: "COM.Example.App:/OAuth/CallBack", want: "native"},
		{redirectURI: "https://app.example.com/callback", want: "web"},
		{redirectURI: "HTTPS://app.example.com/CallBack", want: "web"},
		{redirectURI: "HtTpS://App.Example.com/CallBack?State=AbC", want: "web"},
		// OIDC doesn't allow a native client an https loopback redirect URI.
		{redirectURI: "https://localhost:8085/callback", want: "web"},
		{redirectURI: "HTTPS://LOCALHOST:8085/CallBack", want: "web"},
		{redirectURI: "http://app.example.com/callback", want: "web"},
		{redirectURI: "HtTp://App.Example.com/CallBack", want: "web"},
		{redirectURI: "http://LOCALHOST.example.com:8085/CallBack", want: "web"},
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
			assert.Equal(t, []any{tt.redirectURI}, registration["redirect_uris"])
			assert.Equal(t, "registered-client", handler.GetClientID())
		})
	}
}
