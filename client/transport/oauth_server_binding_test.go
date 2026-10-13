package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type bindingServer struct {
	*httptest.Server
	id, secret                  string
	registrations               atomic.Int32
	tokens                      atomic.Int32
	mu                          sync.Mutex
	names                       []string
	forms                       []url.Values
	registrationGate, tokenGate chan struct{}
	entered                     chan struct{}
	fail                        atomic.Bool
}

func newBindingServer(t *testing.T, id, secret string) *bindingServer {
	t.Helper()
	s := &bindingServer{id: id, secret: secret}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(AuthServerMetadata{Issuer: s.URL, AuthorizationEndpoint: s.URL + "/authorize", TokenEndpoint: s.URL + "/token", RegistrationEndpoint: s.URL + "/register"})
		case "/register":
			s.registrations.Add(1)
			var body struct {
				Name string `json:"client_name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			s.names = append(s.names, body.Name)
			s.mu.Unlock()
			if s.registrationGate != nil {
				s.entered <- struct{}{}
				<-s.registrationGate
			}
			if s.fail.Load() {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"client_id": s.id, "client_secret": s.secret, "token_endpoint_auth_method": "none"})
		case "/token":
			s.tokens.Add(1)
			_ = r.ParseForm()
			s.mu.Lock()
			s.forms = append(s.forms, r.PostForm)
			s.mu.Unlock()
			if s.tokenGate != nil {
				s.entered <- struct{}{}
				<-s.tokenGate
			}
			_ = json.NewEncoder(w).Encode(Token{Issuer: "spoofed", AccessToken: s.id + "-access", RefreshToken: s.id + "-refresh", ExpiresIn: 3600})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// PRM stays on one resource origin while the authorization server migrates.
func bindingResource(t *testing.T, a, b *bindingServer) (string, func()) {
	t.Helper()
	var migrated atomic.Bool
	var resource *httptest.Server
	resource = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issuer := a.URL
		if migrated.Load() {
			issuer = b.URL
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"resource": resource.URL, "authorization_servers": []string{issuer}})
	}))
	t.Cleanup(resource.Close)
	return resource.URL + "/.well-known/oauth-protected-resource", func() { migrated.Store(true) }
}

func TestOAuthBindingDCRMigration(t *testing.T) {
	a, b := newBindingServer(t, "a", "secret-a"), newBindingServer(t, "b", "")
	prm, migrate := bindingResource(t, a, b)
	h := NewOAuthHandler(OAuthConfig{RedirectURI: prm + "/callback", ProtectedResourceMetadataURL: prm, TokenEndpointAuthMethod: clientSecretBasic})
	require.NoError(t, h.RegisterClient(t.Context(), "original name"))
	_, err := h.GetAuthorizationURL(t.Context(), "old-state", "")
	require.NoError(t, err)
	migrate()
	h.SetProtectedResourceMetadataURL(prm)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			u, err := h.GetAuthorizationURL(t.Context(), "new-state", "")
			assert.NoError(t, err)
			parsed, err := url.Parse(u)
			assert.NoError(t, err)
			assert.Equal(t, "b", parsed.Query().Get("client_id"))
		})
	}
	wg.Wait()
	assert.EqualValues(t, 1, b.registrations.Load())
	assert.Equal(t, []string{"original name"}, b.names)
	assert.Empty(t, h.GetClientSecret())
	assert.Equal(t, clientAuthNone, h.GetTokenEndpointAuthMethod())
	assert.Equal(t, b.URL, h.GetCredentialsIssuer())
	exchangeCode(t, h)
	token, err := h.config.TokenStore.GetToken(t.Context())
	require.NoError(t, err)
	assert.Equal(t, b.URL, token.Issuer)
	assert.Equal(t, "b", b.forms[0].Get("client_id"))
	assert.Empty(t, b.forms[0].Get("client_secret"))
	assert.Zero(t, a.tokens.Load())
}

func TestOAuthBindingConfiguredAndTokens(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{true: "first use", false: "restored"}[legacy], func(t *testing.T) {
			a, b := newBindingServer(t, "a", "a-secret"), newBindingServer(t, "b", "")
			prm, migrate := bindingResource(t, a, b)
			issuer := a.URL
			if legacy {
				issuer = ""
			}
			h := NewOAuthHandler(OAuthConfig{ClientID: "configured", ClientSecret: "secret", CredentialsIssuer: issuer, RedirectURI: prm + "/callback", ProtectedResourceMetadataURL: prm})
			_, err := h.GetAuthorizationURL(t.Context(), "state", "")
			require.NoError(t, err)
			assert.Equal(t, a.URL, h.GetCredentialsIssuer())
			require.NoError(t, h.config.TokenStore.SaveToken(t.Context(), &Token{Issuer: a.URL, AccessToken: "valid", RefreshToken: "refresh"}))
			migrate()
			h.SetProtectedResourceMetadataURL(prm)
			_, err = h.GetAuthorizationURL(t.Context(), "new", "")
			require.ErrorIs(t, err, ErrAuthorizationServerChanged)
			err = h.ProcessAuthorizationResponse(t.Context(), "code", "state", "")
			require.ErrorIs(t, err, ErrAuthorizationServerChanged)
			_, err = h.GetAuthorizationHeader(t.Context())
			require.ErrorIs(t, err, ErrAuthorizationServerChanged)
			_, err = h.RefreshToken(t.Context(), "refresh")
			require.ErrorIs(t, err, ErrAuthorizationServerChanged)
			require.NoError(t, h.config.TokenStore.SaveToken(t.Context(), &Token{Issuer: a.URL, AccessToken: "expired", RefreshToken: "refresh", ExpiresAt: time.Now().Add(-time.Minute)}))
			_, err = h.GetAuthorizationHeader(t.Context())
			require.ErrorIs(t, err, ErrAuthorizationServerChanged)
			require.ErrorIs(t, err, ErrOAuthAuthorizationRequired)
			assert.Zero(t, b.tokens.Load())
			assert.Zero(t, b.registrations.Load())
			require.NoError(t, h.RegisterClient(t.Context(), "replacement"))
			assert.Equal(t, b.URL, h.GetCredentialsIssuer())
		})
	}
}

func TestOAuthBindingUnknownProvenance(t *testing.T) {
	s := newBindingServer(t, "a", "")
	h := NewOAuthHandler(OAuthConfig{ClientID: "client", AuthServerMetadataURL: s.URL + "/.well-known/oauth-authorization-server"})
	_, err := h.GetAuthorizationURL(t.Context(), "state", "")
	require.NoError(t, err)
	_, err = h.RefreshToken(t.Context(), "unknown")
	require.ErrorIs(t, err, ErrOAuthAuthorizationRequired)
	require.NoError(t, h.config.TokenStore.SaveToken(t.Context(), &Token{AccessToken: "old", RefreshToken: "old", ExpiresAt: time.Now().Add(-time.Minute)}))
	_, err = h.GetAuthorizationHeader(t.Context())
	require.ErrorIs(t, err, ErrOAuthAuthorizationRequired)
	assert.Zero(t, s.tokens.Load())
	exchangeCode(t, h)
	token, err := h.config.TokenStore.GetToken(t.Context())
	require.NoError(t, err)
	data, err := json.Marshal(token)
	require.NoError(t, err)
	var restored Token
	require.NoError(t, json.Unmarshal(data, &restored))
	assert.Equal(t, s.URL, restored.Issuer)
	_, err = h.RefreshToken(t.Context(), token.RefreshToken)
	require.NoError(t, err)
}

func TestOAuthBindingStaleResponses(t *testing.T) {
	for _, operation := range []string{"registration", "exchange", "refresh"} {
		t.Run(operation, func(t *testing.T) {
			a, b := newBindingServer(t, "a", "secret"), newBindingServer(t, "b", "")
			prm, migrate := bindingResource(t, a, b)
			h := NewOAuthHandler(OAuthConfig{RedirectURI: prm + "/callback", ProtectedResourceMetadataURL: prm})
			if operation != "registration" {
				require.NoError(t, h.RegisterClient(t.Context(), "name"))
			}
			gate := make(chan struct{})
			a.entered = make(chan struct{}, 1)
			if operation == "registration" {
				a.registrationGate = gate
			} else {
				a.tokenGate = gate
			}
			done := make(chan error, 1)
			switch operation {
			case "registration":
				go func() { done <- h.RegisterClient(t.Context(), "name") }()
			case "exchange":
				_, err := h.GetAuthorizationURL(t.Context(), "state", "")
				require.NoError(t, err)
				go func() { done <- h.ProcessAuthorizationResponse(t.Context(), "code", "state", "") }()
			case "refresh":
				require.NoError(t, h.config.TokenStore.SaveToken(t.Context(), &Token{Issuer: a.URL, RefreshToken: "a-refresh"}))
				go func() { _, err := h.RefreshToken(t.Context(), "a-refresh"); done <- err }()
			}
			select {
			case <-a.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("request not sent")
			}
			migrate()
			h.SetProtectedResourceMetadataURL(prm)
			_, err := h.getServerMetadata(t.Context())
			require.NoError(t, err)
			if operation != "registration" {
				require.NoError(t, h.RegisterClient(t.Context(), "new name"))
				exchangeCode(t, h)
			}
			close(gate)
			require.ErrorIs(t, <-done, ErrAuthorizationServerChanged)
			require.NoError(t, h.RegisterClient(t.Context(), "new name"))
			exchangeCode(t, h)
			token, err := h.config.TokenStore.GetToken(t.Context())
			require.NoError(t, err)
			assert.Equal(t, b.URL, token.Issuer)
			assert.Equal(t, "b", h.GetClientID())
		})
	}
}

func TestOAuthBindingMigrationRetryAndCancellation(t *testing.T) {
	a, b := newBindingServer(t, "a", "secret"), newBindingServer(t, "b", "")
	prm, migrate := bindingResource(t, a, b)
	h := NewOAuthHandler(OAuthConfig{RedirectURI: prm + "/callback", ProtectedResourceMetadataURL: prm})
	require.NoError(t, h.RegisterClient(t.Context(), "name"))
	migrate()
	h.SetProtectedResourceMetadataURL(prm)
	b.fail.Store(true)
	_, err := h.GetAuthorizationURL(t.Context(), "state", "")
	require.Error(t, err)
	assert.Empty(t, h.GetClientID())
	assert.Empty(t, h.GetClientSecret())
	b.fail.Store(false)
	b.registrationGate = make(chan struct{})
	b.entered = make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := h.GetAuthorizationURL(ctx, "state", ""); done <- err }()
	<-b.entered
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	close(b.registrationGate)
	_, err = h.GetAuthorizationURL(t.Context(), "state", "")
	require.NoError(t, err)
	assert.EqualValues(t, 2, b.registrations.Load())
}

func TestOAuthBindingExactIssuerAndMissingIssuer(t *testing.T) {
	for _, issuer := range []string{"", "https://issuer.example/tenant", "https://issuer.example/"} {
		t.Run(issuer, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(AuthServerMetadata{Issuer: issuer, AuthorizationEndpoint: "https://issuer.example/authorize", TokenEndpoint: "https://issuer.example/token"})
			}))
			t.Cleanup(s.Close)
			h := NewOAuthHandler(OAuthConfig{ClientID: "client", CredentialsIssuer: "https://issuer.example", AuthServerMetadataURL: s.URL, SkipIssuerMetadataValidation: true})
			_, err := h.GetAuthorizationURL(t.Context(), "state", "")
			if issuer == "" {
				require.ErrorContains(t, err, "missing issuer")
			} else {
				require.ErrorIs(t, err, ErrAuthorizationServerChanged)
			}
			assert.Empty(t, h.expectedState)
		})
	}
}

func TestOAuthBindingSameIssuerAndOldFlow(t *testing.T) {
	a, b := newBindingServer(t, "a", "secret"), newBindingServer(t, "b", "")
	prm, migrate := bindingResource(t, a, b)
	h := NewOAuthHandler(OAuthConfig{RedirectURI: prm + "/callback", ProtectedResourceMetadataURL: prm})
	require.NoError(t, h.RegisterClient(t.Context(), "name"))
	_, err := h.GetAuthorizationURL(t.Context(), "old", "")
	require.NoError(t, err)
	h.SetProtectedResourceMetadataURL(prm)
	_, err = h.GetAuthorizationURL(t.Context(), "old", "")
	require.NoError(t, err)
	assert.EqualValues(t, 1, a.registrations.Load())
	migrate()
	h.SetProtectedResourceMetadataURL(prm)
	require.NoError(t, h.RegisterClient(t.Context(), "new"))
	err = h.ProcessAuthorizationResponse(t.Context(), "old-code", "old", "")
	require.ErrorIs(t, err, ErrAuthorizationServerChanged)
	assert.Zero(t, b.tokens.Load())
}

func TestOAuthBindingRestartAndDirectExchange(t *testing.T) {
	a, b := newBindingServer(t, "a", "secret"), newBindingServer(t, "b", "")
	prm, migrate := bindingResource(t, a, b)
	h := NewOAuthHandler(OAuthConfig{RedirectURI: prm + "/callback", ProtectedResourceMetadataURL: prm})
	require.NoError(t, h.RegisterClient(t.Context(), "name"))
	exchangeCode(t, h)
	saved, err := h.config.TokenStore.GetToken(t.Context())
	require.NoError(t, err)
	store := NewMemoryTokenStore()
	require.NoError(t, store.SaveToken(t.Context(), saved))
	restored := NewOAuthHandler(OAuthConfig{
		ClientID: h.GetClientID(), ClientSecret: h.GetClientSecret(),
		CredentialsIssuer: h.GetCredentialsIssuer(), TokenEndpointAuthMethod: h.GetTokenEndpointAuthMethod(),
		TokenStore: store, RedirectURI: prm + "/callback", ProtectedResourceMetadataURL: prm,
	})
	migrate()
	restored.SetExpectedState("direct")
	err = restored.ProcessAuthorizationResponse(t.Context(), "code", "direct", "")
	require.ErrorIs(t, err, ErrAuthorizationServerChanged)
	_, err = restored.GetAuthorizationURL(t.Context(), "state", "")
	require.ErrorIs(t, err, ErrAuthorizationServerChanged)
	assert.Zero(t, b.registrations.Load())
	assert.Zero(t, b.tokens.Load())

	legacy := NewOAuthHandler(OAuthConfig{ClientID: "legacy", AuthServerMetadataURL: a.URL + "/.well-known/oauth-authorization-server"})
	legacy.SetExpectedState("direct")
	require.NoError(t, legacy.ProcessAuthorizationResponse(t.Context(), "code", "direct", ""))
	assert.Equal(t, a.URL, legacy.GetCredentialsIssuer())
}

func TestOAuthBindingEmptyRegistrationID(t *testing.T) {
	s := newBindingServer(t, "", "secret")
	h := NewOAuthHandler(OAuthConfig{AuthServerMetadataURL: s.URL + "/.well-known/oauth-authorization-server"})
	require.ErrorContains(t, h.RegisterClient(t.Context(), "name"), "missing client_id")
	assert.Empty(t, h.GetCredentialsIssuer())
	assert.Empty(t, h.GetClientSecret())
}
