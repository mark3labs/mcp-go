package transport_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/client/transport"
)

type authorizationFixture struct {
	handler       *transport.OAuthHandler
	config        transport.OAuthConfig
	server        *httptest.Server
	tokenRequests [2]atomic.Int32
	tokenData     chan authorizationTokenRequest
	tokenError    atomic.Bool
}

type authorizationTokenRequest struct {
	data     url.Values
	username string
	password string
	basic    bool
}

func newAuthorizationFixture(t *testing.T, aSupportsISS, bSupportsISS bool) *authorizationFixture {
	t.Helper()
	f := &authorizationFixture{tokenData: make(chan authorizationTokenRequest, 4)}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource/mcp":
			_ = json.NewEncoder(w).Encode(transport.OAuthProtectedResource{
				Resource: f.server.URL, AuthorizationServers: []string{f.server.URL + "/a"},
			})
		case "/prm/b":
			_ = json.NewEncoder(w).Encode(transport.OAuthProtectedResource{
				Resource: f.server.URL + "/mcp", AuthorizationServers: []string{f.server.URL + "/b"},
			})
		case "/.well-known/oauth-authorization-server/a", "/.well-known/oauth-authorization-server/b":
			issuer, supported, authMethod := f.server.URL+"/a", aSupportsISS, "client_secret_basic"
			if r.URL.Path == "/.well-known/oauth-authorization-server/b" {
				issuer, supported, authMethod = f.server.URL+"/b", bSupportsISS, "client_secret_post"
			}
			_ = json.NewEncoder(w).Encode(transport.AuthServerMetadata{
				Issuer: issuer, AuthorizationEndpoint: issuer + "/authorize", TokenEndpoint: issuer + "/token",
				AuthorizationResponseIssParameterSupported: supported,
				TokenEndpointAuthMethodsSupported:          []string{authMethod},
			})
		case "/a/token", "/b/token":
			index := 0
			if r.URL.Path == "/b/token" {
				index = 1
			}
			f.tokenRequests[index].Add(1)
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			username, password, basic := r.BasicAuth()
			f.tokenData <- authorizationTokenRequest{data: r.PostForm, username: username, password: password, basic: basic}
			if f.tokenError.Load() {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(transport.OAuthError{ErrorCode: "invalid_grant", ErrorDescription: "A rejected the code"})
				return
			}
			_ = json.NewEncoder(w).Encode(transport.Token{AccessToken: "token", TokenType: "Bearer"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.server.Close)
	f.config = transport.OAuthConfig{
		ClientID: "client", ClientSecret: "secret-a", RedirectURI: "http://localhost:8085/callback", PKCEEnabled: true,
		TokenStore: transport.NewMemoryTokenStore(),
	}
	f.handler = transport.NewOAuthHandler(f.config)
	f.handler.SetBaseURL(f.server.URL + "/mcp")
	authURL, err := f.handler.GetAuthorizationURL(t.Context(), "state-a", "challenge")
	require.NoError(t, err)
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	assert.Equal(t, "/a/authorize", parsed.Path)
	assert.Equal(t, f.server.URL, parsed.Query().Get("resource"))
	return f
}

func (f *authorizationFixture) replaceMetadata(t *testing.T, via401 bool) {
	t.Helper()
	if via401 {
		f.handler.HandleUnauthorizedResponse(&http.Response{StatusCode: http.StatusUnauthorized, Header: http.Header{
			"Www-Authenticate": {fmt.Sprintf(`Bearer resource_metadata="%s/prm/b"`, f.server.URL)},
		}})
	} else {
		f.handler.SetProtectedResourceMetadataURL(f.server.URL + "/prm/b")
	}
	metadata, err := f.handler.GetServerMetadata(t.Context())
	require.NoError(t, err)
	require.Equal(t, f.server.URL+"/b", metadata.Issuer)
}

// A changed issuer invalidates the transaction without sending its code to
// either endpoint. The original issuer requirement is still checked first.
func TestOAuthHandler_AuthorizationTransactionRejectsChangedIssuer(t *testing.T) {
	tests := []struct {
		name          string
		aSupportsISS  bool
		bSupportsISS  bool
		responseISS   string
		legacy        bool
		wantIssuerErr bool
	}{
		{name: "valid original issuer", aSupportsISS: true, bSupportsISS: true, responseISS: "/a"},
		{name: "replacement issuer rejected", aSupportsISS: true, bSupportsISS: true, responseISS: "/b", wantIssuerErr: true},
		{name: "original requirement retained", aSupportsISS: true, wantIssuerErr: true},
		{name: "optional issuer stays optional", bSupportsISS: true},
		{name: "legacy exchange stays at original endpoint", legacy: true},
	}
	for _, via401 := range []bool{false, true} {
		for _, tt := range tests {
			t.Run(fmt.Sprintf("401=%t/%s", via401, tt.name), func(t *testing.T) {
				f := newAuthorizationFixture(t, tt.aSupportsISS, tt.bSupportsISS)
				f.replaceMetadata(t, via401)
				query := url.Values{"code": {"code-a"}, "state": {"state-a"}}
				if tt.responseISS != "" {
					query.Set("iss", f.server.URL+tt.responseISS)
				}
				var err error
				if tt.legacy {
					err = f.handler.ProcessAuthorizationResponse(t.Context(), "code-a", "state-a", "verifier-a")
				} else {
					err = f.handler.ProcessAuthorizationCallback(t.Context(), query, "verifier-a")
				}
				if tt.wantIssuerErr {
					require.ErrorIs(t, err, transport.ErrIssuerMismatch)
					assert.Zero(t, f.tokenRequests[0].Load())
				} else {
					require.ErrorIs(t, err, transport.ErrAuthorizationServerChanged)
					assert.Zero(t, f.tokenRequests[0].Load(), "invalidated flow must not exchange its code")
				}
				assert.Zero(t, f.tokenRequests[1].Load(), "code must never be sent to B")
			})
		}
	}
}

func TestOAuthHandler_AuthorizationTransactionCopiesMetadata(t *testing.T) {
	for _, changeIssuer := range []bool{false, true} {
		t.Run(fmt.Sprintf("changeIssuer=%t", changeIssuer), func(t *testing.T) {
			f := newAuthorizationFixture(t, true, true)
			metadata, err := f.handler.GetServerMetadata(t.Context())
			require.NoError(t, err)
			if changeIssuer {
				metadata.Issuer = f.server.URL + "/b"
			}
			metadata.TokenEndpoint = f.server.URL + "/b/token"
			metadata.AuthorizationResponseIssParameterSupported = false
			metadata.TokenEndpointAuthMethodsSupported[0] = "client_secret_post"
			err = f.handler.ProcessAuthorizationCallback(t.Context(), url.Values{
				"code": {"code-a"}, "state": {"state-a"}, "iss": {f.server.URL + "/a"},
			}, "verifier-a")
			if changeIssuer {
				require.ErrorIs(t, err, transport.ErrAuthorizationServerChanged)
				assert.Zero(t, f.tokenRequests[0].Load())
				assert.Zero(t, f.tokenRequests[1].Load())
				return
			}
			assert.NoError(t, err)
			require.Equal(t, int32(1), f.tokenRequests[0].Load())
			assert.Zero(t, f.tokenRequests[1].Load())
			assert.True(t, (<-f.tokenData).basic, "auth method must also be copied")
		})
	}
}

func TestOAuthHandler_AuthorizationCallbackStateOnlyReconstruction(t *testing.T) {
	f := newAuthorizationFixture(t, false, false)
	f.replaceMetadata(t, false)
	callback := transport.NewOAuthHandler(f.config)
	callback.SetBaseURL(f.server.URL + "/mcp")
	callback.SetProtectedResourceMetadataURL(f.server.URL + "/prm/b")
	callback.SetExpectedState("state-a")
	err := callback.ProcessAuthorizationCallback(t.Context(), url.Values{
		"code": {"code-a"}, "state": {"state-a"},
	}, "verifier-a")
	assert.Error(t, err, "state alone cannot bind the original authorization server")
	assert.Zero(t, f.tokenRequests[0].Load())
	assert.Zero(t, f.tokenRequests[1].Load())
}

func TestOAuthHandler_AuthorizationTransactionCrossRequest(t *testing.T) {
	tests := []struct {
		name          string
		wrongIssuer   bool
		tokenError    bool
		responseError bool
	}{
		{name: "success"},
		{name: "issuer mismatch", wrongIssuer: true},
		{name: "token endpoint error", tokenError: true},
		{name: "authorization error", responseError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newAuthorizationFixture(t, true, true)
			transaction, ok := f.handler.GetAuthorizationTransaction()
			require.True(t, ok)
			f.replaceMetadata(t, true)
			// The original handler cannot migrate configured credentials to B.
			_, err := f.handler.GetAuthorizationURL(t.Context(), "state-b", "challenge-b")
			require.ErrorIs(t, err, transport.ErrAuthorizationServerChanged)
			config := f.config
			config.RedirectURI, config.PKCEEnabled = "http://localhost/other", false
			callback := transport.NewOAuthHandler(config)
			callback.SetBaseURL(f.server.URL + "/mcp")
			require.NoError(t, callback.SetAuthorizationTransaction(transaction))
			callback.SetExpectedState("state-a") // Keeping the same state preserves its binding.
			query := url.Values{"code": {"code-a"}, "state": {"state-a"}, "iss": {f.server.URL + "/a"}}
			if tt.wrongIssuer {
				query.Set("iss", f.server.URL+"/b")
			}
			if tt.responseError {
				query.Del("code")
				query.Set("error", "access_denied")
			}
			f.tokenError.Store(tt.tokenError)
			err = callback.ProcessAuthorizationCallback(t.Context(), query, "verifier-a")
			switch {
			case tt.wrongIssuer:
				require.ErrorIs(t, err, transport.ErrIssuerMismatch)
			case tt.tokenError:
				require.ErrorContains(t, err, "A rejected the code")
			case tt.responseError:
				require.ErrorContains(t, err, "access_denied")
			default:
				require.NoError(t, err)
			}
			_, ok = callback.GetAuthorizationTransaction()
			assert.False(t, ok, "state-valid responses consume the restored transaction")
			assert.Empty(t, callback.GetExpectedState())
			require.Error(t, callback.ProcessAuthorizationCallback(t.Context(), query, "verifier-a"))
			assert.Zero(t, f.tokenRequests[1].Load())
			if tt.wrongIssuer || tt.responseError {
				assert.Zero(t, f.tokenRequests[0].Load())
				return
			}
			require.Equal(t, int32(1), f.tokenRequests[0].Load())
			request := <-f.tokenData
			assert.Equal(t, f.server.URL, request.data.Get("resource"))
			assert.Equal(t, "http://localhost:8085/callback", request.data.Get("redirect_uri"))
			assert.Equal(t, "verifier-a", request.data.Get("code_verifier"))
			assert.True(t, request.basic)
			assert.Equal(t, "client", request.username)
			assert.Equal(t, "secret-a", request.password)
		})
	}
}

func TestOAuthHandler_AuthorizationTransactionStateAndConsumption(t *testing.T) {
	f := newAuthorizationFixture(t, true, true)
	require.Error(t, f.handler.SetAuthorizationTransaction(transport.AuthorizationTransaction{}))
	assert.Equal(t, "state-a", f.handler.GetExpectedState())
	query := url.Values{"code": {"code-a"}, "state": {"wrong"}, "iss": {f.server.URL + "/a"}}
	require.ErrorIs(t, f.handler.ProcessAuthorizationCallback(t.Context(), query, "verifier-a"), transport.ErrInvalidState)
	_, ok := f.handler.GetAuthorizationTransaction()
	require.True(t, ok, "a wrong state must not consume a pending transaction")
	query.Set("state", "state-a")
	query.Add("iss", f.server.URL+"/b")
	require.ErrorContains(t, f.handler.ProcessAuthorizationCallback(t.Context(), query, "verifier-a"), "repeats")
	_, ok = f.handler.GetAuthorizationTransaction()
	require.True(t, ok, "duplicate parameters must not consume a pending transaction")
	query.Set("iss", f.server.URL+"/a")
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		wg.Go(func() { errors <- f.handler.ProcessAuthorizationCallback(t.Context(), query, "verifier-a") })
	}
	wg.Wait()
	close(errors)
	successes := 0
	for err := range errors {
		if err == nil {
			successes++
		}
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, int32(1), f.tokenRequests[0].Load())
	assert.Zero(t, f.tokenRequests[1].Load())
	_, ok = f.handler.GetAuthorizationTransaction()
	assert.False(t, ok)
}

func TestOAuthHandler_AuthorizationTransactionStateReset(t *testing.T) {
	f := newAuthorizationFixture(t, false, false)
	f.handler.SetExpectedState("replacement-state")
	_, ok := f.handler.GetAuthorizationTransaction()
	assert.False(t, ok, "a replacement state must discard its previous binding")
	require.Error(t, f.handler.ProcessAuthorizationCallback(t.Context(), url.Values{
		"code": {"code-a"}, "state": {"replacement-state"},
	}, "verifier-a"))
	assert.Zero(t, f.tokenRequests[0].Load())
	assert.Zero(t, f.tokenRequests[1].Load())
}
