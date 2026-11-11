package transport

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deleteRecorder is a test server that records the session IDs of the
// DELETE requests it receives.
type deleteRecorder struct {
	mu      sync.Mutex
	deleted []string
}

func (r *deleteRecorder) handler(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodDelete {
		r.mu.Lock()
		r.deleted = append(r.deleted, req.Header.Get(HeaderKeySessionID))
		r.mu.Unlock()
	}
	w.WriteHeader(http.StatusOK)
}

func (r *deleteRecorder) deletedSessions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.deleted...)
}

func newSessionPublishTransport(t *testing.T) (*StreamableHTTP, *deleteRecorder) {
	t.Helper()
	rec := &deleteRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(rec.handler))
	t.Cleanup(srv.Close)
	c, err := NewStreamableHTTP(srv.URL)
	require.NoError(t, err)
	return c, rec
}

// TestPublishSessionIDAfterCloseTerminatesSession covers the late
// publication ordering from issue #1202: Close observes an empty session
// ID, then an initialize response's session ID is published. The ID must
// not stay published on the closed transport, and terminating it must be
// attempted exactly once.
func TestPublishSessionIDAfterCloseTerminatesSession(t *testing.T) {
	c, rec := newSessionPublishTransport(t)

	require.NoError(t, c.Close())
	assert.Empty(t, rec.deletedSessions(), "Close with no session ID must not send DELETE")

	c.publishSessionID("sess-late")

	assert.Equal(t, "", c.sessionID.Load().(string),
		"session ID must not remain published on a closed transport")
	assert.Equal(t, []string{"sess-late"}, rec.deletedSessions(),
		"late-published session ID must be terminated exactly once")
}

// TestPublishSessionIDBeforeCloseTerminatesOnce covers the ordering where
// the session ID is published before Close: Close terminates it, and a
// second Close does not terminate it again.
func TestPublishSessionIDBeforeCloseTerminatesOnce(t *testing.T) {
	c, rec := newSessionPublishTransport(t)

	c.publishSessionID("sess-early")
	assert.Equal(t, "sess-early", c.sessionID.Load().(string))
	assert.Empty(t, rec.deletedSessions(), "publishing on an open transport must not send DELETE")

	require.NoError(t, c.Close())
	assert.Equal(t, "", c.sessionID.Load().(string))
	assert.Equal(t, []string{"sess-early"}, rec.deletedSessions())

	require.NoError(t, c.Close())
	assert.Equal(t, []string{"sess-early"}, rec.deletedSessions(),
		"second Close must not send another DELETE")
}

// TestPublishSessionIDEmptyIsIgnored ensures an empty ID never publishes
// and never triggers a DELETE, before or after Close.
func TestPublishSessionIDEmptyIsIgnored(t *testing.T) {
	c, rec := newSessionPublishTransport(t)

	c.publishSessionID("")
	require.NoError(t, c.Close())
	c.publishSessionID("")

	assert.Equal(t, "", c.sessionID.Load().(string))
	assert.Empty(t, rec.deletedSessions())
}
