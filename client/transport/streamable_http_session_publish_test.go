package transport

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
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
// publication ordering from issue #1202. The ID must not remain published;
// cleanup sends one DELETE for legacy sessions and none for modern protocols.
func TestPublishSessionIDAfterCloseTerminatesSession(t *testing.T) {
	tests := []struct {
		name            string
		protocolVersion string
		deleted         []string
	}{
		{name: "legacy", deleted: []string{"sess-late"}},
		{name: "modern", protocolVersion: mcp.ProtocolVersion20260728},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rec := newSessionPublishTransport(t)
			if tt.protocolVersion != "" {
				c.SetProtocolVersion(tt.protocolVersion)
			}

			require.NoError(t, c.Close())
			assert.Empty(t, rec.deletedSessions(), "Close with no session ID must not send DELETE")

			c.publishSessionID("sess-late")

			assert.Equal(t, "", c.sessionID.Load().(string),
				"session ID must not remain published on a closed transport")
			assert.Equal(t, tt.deleted, rec.deletedSessions(),
				"cleanup must follow the negotiated protocol")
		})
	}
}

// TestPublishSessionIDBeforeCloseTerminatesOnce covers publication before
// Close: legacy cleanup happens once, while modern sessions never send DELETE.
func TestPublishSessionIDBeforeCloseTerminatesOnce(t *testing.T) {
	tests := []struct {
		name            string
		protocolVersion string
		deleted         []string
	}{
		{name: "legacy", deleted: []string{"sess-early"}},
		{name: "modern", protocolVersion: mcp.ProtocolVersion20260728},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rec := newSessionPublishTransport(t)
			if tt.protocolVersion != "" {
				c.SetProtocolVersion(tt.protocolVersion)
			}

			c.publishSessionID("sess-early")
			assert.Equal(t, "sess-early", c.sessionID.Load().(string))
			assert.Empty(t, rec.deletedSessions(), "publishing on an open transport must not send DELETE")

			require.NoError(t, c.Close())
			assert.Equal(t, "", c.sessionID.Load().(string))
			assert.Equal(t, tt.deleted, rec.deletedSessions())

			require.NoError(t, c.Close())
			assert.Equal(t, tt.deleted, rec.deletedSessions(),
				"second Close must not send another DELETE")
		})
	}
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
