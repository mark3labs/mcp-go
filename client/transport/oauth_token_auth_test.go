package transport

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tokenAuthServer is an authorization server that lists supported as its
// token endpoint authentication methods, answers registration with
// registration, and records every token request.
type tokenAuthServer struct {
	supported    []string
	registration map[string]any

	mu             sync.Mutex
	authorizations []string
	forms          []url.Values
}

func (s *tokenAuthServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(AuthServerMetadata{
				Issuer:                            server.URL,
				AuthorizationEndpoint:             server.URL + "/authorize",
				TokenEndpoint:                     server.URL + "/token",
				RegistrationEndpoint:              server.URL + "/register",
				TokenEndpointAuthMethodsSupported: s.supported,
			})
		case "/register":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(s.registration)
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parsing token request: %v", err)
			}
			s.mu.Lock()
			s.authorizations = append(s.authorizations, r.Header.Get("Authorization"))
			s.forms = append(s.forms, r.PostForm)
			s.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access-token",
				"token_type":    "Bearer",
				"expires_in":    3600,
				"refresh_token": "refresh-token",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// lastTokenRequest returns the Basic credentials, if any, and the form of the
// most recent token request.
func (s *tokenAuthServer) lastTokenRequest(t *testing.T) (user, password string, basic bool, form url.Values) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.NotEmpty(t, s.forms, "no token request was made")
	last := len(s.forms) - 1
	request := http.Request{Header: http.Header{"Authorization": {s.authorizations[last]}}}
	user, password, basic = request.BasicAuth()
	return user, password, basic, s.forms[last]
}

func exchangeCode(t *testing.T, handler *OAuthHandler) {
	t.Helper()
	handler.SetExpectedState("state")
	require.NoError(t, handler.ProcessAuthorizationResponse(t.Context(), "code", "state", "verifier"))
}

// The client authenticates at the token endpoint with a method the server
// supports (RFC 6749 §2.3.1). A server that only lists client_secret_basic
// rejects credentials sent in the request body.
func TestOAuthHandler_TokenEndpointAuthMethod(t *testing.T) {
	tests := []struct {
		name      string
		supported []string
		secret    string
		wantBasic bool
	}{
		{name: "only client_secret_basic", supported: []string{"client_secret_basic"}, secret: "secret", wantBasic: true},
		{name: "client_secret_basic and client_secret_post", supported: []string{"client_secret_basic", "client_secret_post"}, secret: "secret"},
		{name: "only client_secret_post", supported: []string{"client_secret_post"}, secret: "secret"},
		{name: "methods not listed", secret: "secret"},
		{name: "public client", supported: []string{"client_secret_basic"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			as := &tokenAuthServer{supported: tt.supported}
			server := as.start(t)
			handler := NewOAuthHandler(OAuthConfig{
				ClientID:              "client",
				ClientSecret:          tt.secret,
				RedirectURI:           "http://localhost:8085/callback",
				TokenStore:            NewMemoryTokenStore(),
				AuthServerMetadataURL: server.URL + "/.well-known/oauth-authorization-server",
				PKCEEnabled:           true,
			})

			exchangeCode(t, handler)
			user, password, basic, form := as.lastTokenRequest(t)
			assert.Equal(t, tt.wantBasic, basic)
			if tt.wantBasic {
				assert.Equal(t, "client", user)
				assert.Equal(t, tt.secret, password)
				assert.Empty(t, form.Get("client_id"), "credentials go in one place only")
				assert.Empty(t, form.Get("client_secret"))
			} else {
				assert.Equal(t, "client", form.Get("client_id"))
				assert.Equal(t, tt.secret, form.Get("client_secret"))
			}
			assert.Equal(t, "code", form.Get("code"))

			// A refresh authenticates the same way.
			_, err := handler.RefreshToken(t.Context(), "refresh-token")
			require.NoError(t, err)
			_, _, refreshBasic, refreshForm := as.lastTokenRequest(t)
			assert.Equal(t, tt.wantBasic, refreshBasic)
			assert.Equal(t, form.Get("client_id"), refreshForm.Get("client_id"))
			assert.Equal(t, form.Get("client_secret"), refreshForm.Get("client_secret"))
			assert.Equal(t, "refresh-token", refreshForm.Get("refresh_token"))
		})
	}
}

// The method in the registration response is the one the server actually
// registered (RFC 7591 §3.2.1), and it wins over the metadata's list.
func TestOAuthHandler_TokenEndpointAuthMethodFromRegistration(t *testing.T) {
	tests := []struct {
		method    string
		supported []string // what the metadata alone would lead to differs
		wantBasic bool
		wantBody  string // the client_secret expected in the body
	}{
		{method: "client_secret_basic", supported: []string{"client_secret_post"}, wantBasic: true},
		{method: "client_secret_post", supported: []string{"client_secret_basic"}, wantBody: "registered-secret"},
		{method: "none", supported: []string{"client_secret_basic"}},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			as := &tokenAuthServer{
				supported: tt.supported,
				registration: map[string]any{
					"client_id":                  "registered",
					"client_secret":              "registered-secret",
					"token_endpoint_auth_method": tt.method,
				},
			}
			server := as.start(t)
			handler := NewOAuthHandler(OAuthConfig{
				RedirectURI:           "http://localhost:8085/callback",
				TokenStore:            NewMemoryTokenStore(),
				AuthServerMetadataURL: server.URL + "/.well-known/oauth-authorization-server",
				PKCEEnabled:           true,
			})
			require.NoError(t, handler.RegisterClient(t.Context(), "test-client"))
			assert.Equal(t, tt.method, handler.GetTokenEndpointAuthMethod())

			exchangeCode(t, handler)
			user, password, basic, form := as.lastTokenRequest(t)
			assert.Equal(t, tt.wantBasic, basic)
			if basic {
				assert.Equal(t, "registered", user)
				assert.Equal(t, "registered-secret", password)
			} else {
				assert.Equal(t, "registered", form.Get("client_id"))
			}
			assert.Equal(t, tt.wantBody, form.Get("client_secret"))
		})
	}
}

// OAuthConfig.TokenEndpointAuthMethod settles it for pre-registered
// credentials when the server supports more than one method. Ory Hydra, for
// one, lists both but accepts only the method a client was registered with.
func TestOAuthHandler_ConfiguredTokenEndpointAuthMethod(t *testing.T) {
	tests := []struct {
		method    string
		supported []string
		wantBasic bool
	}{
		{method: "client_secret_basic", supported: []string{"client_secret_basic", "client_secret_post"}, wantBasic: true},
		{method: "client_secret_post", supported: []string{"client_secret_basic"}},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			as := &tokenAuthServer{supported: tt.supported}
			server := as.start(t)
			handler := NewOAuthHandler(OAuthConfig{
				ClientID:                "client",
				ClientSecret:            "secret",
				TokenEndpointAuthMethod: tt.method,
				RedirectURI:             "http://localhost:8085/callback",
				TokenStore:              NewMemoryTokenStore(),
				AuthServerMetadataURL:   server.URL + "/.well-known/oauth-authorization-server",
				PKCEEnabled:             true,
			})
			assert.Equal(t, tt.method, handler.GetTokenEndpointAuthMethod())

			exchangeCode(t, handler)
			_, _, basic, form := as.lastTokenRequest(t)
			assert.Equal(t, tt.wantBasic, basic)
			assert.Equal(t, !tt.wantBasic, form.Get("client_secret") == "secret")
		})
	}
}

// RFC 6749 §2.3.1 form-encodes the client ID and secret before they become
// the Basic user name and password.
func TestOAuthHandler_BasicCredentialsAreFormEncoded(t *testing.T) {
	as := &tokenAuthServer{supported: []string{"client_secret_basic"}}
	server := as.start(t)
	handler := NewOAuthHandler(OAuthConfig{
		ClientID:              "my client",
		ClientSecret:          "s3cr+t/=:",
		RedirectURI:           "http://localhost:8085/callback",
		TokenStore:            NewMemoryTokenStore(),
		AuthServerMetadataURL: server.URL + "/.well-known/oauth-authorization-server",
		PKCEEnabled:           true,
	})

	exchangeCode(t, handler)
	as.mu.Lock()
	defer as.mu.Unlock()
	require.Len(t, as.authorizations, 1)
	assert.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("my+client:s3cr%2Bt%2F%3D%3A")), as.authorizations[0])
}
