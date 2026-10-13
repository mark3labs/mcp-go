package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// refreshServer serves authorization server metadata and a token endpoint
// that answers refresh requests with token, and counts them.
type refreshServer struct {
	*httptest.Server
	refreshes atomic.Int32
}

func newRefreshServer(t *testing.T, token func(w http.ResponseWriter, refreshToken string)) *refreshServer {
	t.Helper()
	s := &refreshServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                 s.URL,
				"authorization_endpoint": s.URL + "/authorize",
				"token_endpoint":         s.URL + "/token",
			})
		case "/token":
			s.refreshes.Add(1)
			_ = r.ParseForm()
			token(w, r.FormValue("refresh_token"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// singleUseTokens answers like a server whose refresh tokens are good for one
// use: it spends a refresh token as soon as the request arrives, answers
// after delay, and rejects a spent one.
func singleUseTokens(delay time.Duration) func(http.ResponseWriter, string) {
	var mu sync.Mutex
	spent := map[string]bool{}
	return func(w http.ResponseWriter, refreshToken string) {
		mu.Lock()
		reused := spent[refreshToken]
		spent[refreshToken] = true
		mu.Unlock()
		time.Sleep(delay)
		if reused {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"token_type":    "bearer",
			"expires_in":    3600,
			"refresh_token": "next-" + refreshToken,
		})
	}
}

// newRefreshTestHandler returns a handler whose stored access token has
// expired and whose refresh token is "single-use".
func newRefreshTestHandler(t *testing.T, server *refreshServer) (*OAuthHandler, *MemoryTokenStore) {
	t.Helper()
	store := NewMemoryTokenStore()
	require.NoError(t, store.SaveToken(t.Context(), &Token{
		AccessToken:  "old-access",
		TokenType:    "Bearer",
		RefreshToken: "single-use",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}))
	return NewOAuthHandler(OAuthConfig{
		ClientID:              "client",
		TokenStore:            store,
		AuthServerMetadataURL: server.URL + "/.well-known/oauth-authorization-server",
	}), store
}

// Callers that find the access token expired at the same time share one
// refresh. With a refresh token that is good for one use, as GitHub's are, a
// refresh per caller fails for all but the first, and those callers were sent
// back to authorize although a new token was on its way.
func TestOAuthHandler_ConcurrentCallersRefreshOnce(t *testing.T) {
	// The token endpoint answers slowly, so that every caller finds the token
	// expired before the first refresh completes.
	server := newRefreshServer(t, singleUseTokens(100*time.Millisecond))
	handler, store := newRefreshTestHandler(t, server)

	const callers = 8
	headers := make([]string, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			headers[i], errs[i] = handler.GetAuthorizationHeader(t.Context())
		})
	}
	wg.Wait()

	for i := range callers {
		require.NoError(t, errs[i], "caller %d", i)
		assert.Equal(t, "Bearer new-access", headers[i], "caller %d", i)
	}
	assert.Equal(t, int32(1), server.refreshes.Load(), "refresh requests")
	saved, err := store.GetToken(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "next-single-use", saved.RefreshToken)
}

// A refresh doesn't end with the caller that started it. Otherwise, once the
// server had spent the refresh token, the next caller would send it again and
// be refused.
func TestOAuthHandler_RefreshOutlivesTheCallerThatStartedIt(t *testing.T) {
	server := newRefreshServer(t, singleUseTokens(200*time.Millisecond))
	handler, _ := newRefreshTestHandler(t, server)

	impatient, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := handler.GetAuthorizationHeader(impatient)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	header, err := handler.GetAuthorizationHeader(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "Bearer new-access", header)
	assert.Equal(t, int32(1), server.refreshes.Load(), "refresh requests")
}

// Callers waiting on a refresh that fails all get the failure from it, rather
// than each sending a refresh of its own in turn. The next call tries again.
func TestOAuthHandler_CallersShareAFailedRefresh(t *testing.T) {
	server := newRefreshServer(t, func(w http.ResponseWriter, _ string) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusInternalServerError)
	})
	handler, _ := newRefreshTestHandler(t, server)

	const callers = 5
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			_, errs[i] = handler.GetAuthorizationHeader(t.Context())
		})
	}
	wg.Wait()
	for i := range callers {
		assert.ErrorIs(t, errs[i], ErrOAuthAuthorizationRequired, "caller %d", i)
	}
	assert.Equal(t, int32(1), server.refreshes.Load(), "refresh requests")

	_, err := handler.GetAuthorizationHeader(t.Context())
	assert.ErrorIs(t, err, ErrOAuthAuthorizationRequired)
	assert.Equal(t, int32(2), server.refreshes.Load(), "refresh requests after another call")
}

// A caller waiting for another's refresh gives up when its context ends.
func TestOAuthHandler_RefreshWaitHonorsContext(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	server := newRefreshServer(t, func(w http.ResponseWriter, _ string) {
		entered <- struct{}{}
		<-release
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new-access", "token_type": "bearer"})
	})
	handler, _ := newRefreshTestHandler(t, server)

	// The first caller's refresh hangs until the test ends.
	go func() { _, _ = handler.GetAuthorizationHeader(t.Context()) }()
	<-entered

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := handler.GetAuthorizationHeader(ctx)
		done <- err
	}()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting caller ignored its context")
	}
}

type staleReadKey struct{}

// staleReadStore delays the first read made with a staleReadKey context:
// it reads the token, then waits for resume before returning it, by which
// time the stored token may have changed.
type staleReadStore struct {
	TokenStore
	once   sync.Once
	taken  chan struct{}
	resume chan struct{}
}

func (s *staleReadStore) GetToken(ctx context.Context) (*Token, error) {
	if ctx.Value(staleReadKey{}) != nil {
		var token *Token
		var err error
		delayed := false
		s.once.Do(func() {
			token, err = s.TokenStore.GetToken(ctx)
			close(s.taken)
			<-s.resume
			delayed = true
		})
		if delayed {
			return token, err
		}
	}
	return s.TokenStore.GetToken(ctx)
}

// A caller that read the expired token before another caller's refresh
// finished uses the new token instead of sending the spent refresh token.
func TestOAuthHandler_RefreshAfterAStaleRead(t *testing.T) {
	server := newRefreshServer(t, singleUseTokens(0))
	handler, store := newRefreshTestHandler(t, server)
	stale := &staleReadStore{TokenStore: store, taken: make(chan struct{}), resume: make(chan struct{})}
	handler.config.TokenStore = stale

	done := make(chan error, 1)
	var header string
	go func() {
		var err error
		header, err = handler.GetAuthorizationHeader(context.WithValue(t.Context(), staleReadKey{}, true))
		done <- err
	}()
	<-stale.taken

	// Another caller refreshes while the first still holds the old token.
	_, err := handler.GetAuthorizationHeader(t.Context())
	require.NoError(t, err)
	close(stale.resume)

	require.NoError(t, <-done)
	assert.Equal(t, "Bearer new-access", header)
	assert.Equal(t, int32(1), server.refreshes.Load(), "refresh requests")
}
