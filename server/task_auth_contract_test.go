package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTaskAccessControlContract verifies the bearer-token access model.
func TestTaskAccessControlContract(t *testing.T) {
	newServer := func() *MCPServer {
		return NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))
	}

	// sessionCtx returns a context.Context with the given session ID injected,
	// registering the session with s so notifications work.
	sessionCtx := func(t *testing.T, s *MCPServer, id string) context.Context {
		t.Helper()
		sess := fakeSess{sessionID: id, notifyChan: make(chan mcp.JSONRPCNotification, 4)}
		err := s.RegisterSession(t.Context(), sess)
		require.NoError(t, err)
		t.Cleanup(func() { s.UnregisterSession(t.Context(), id) })
		return s.WithContext(t.Context(), sess)
	}

	t.Run("session match — allowed", func(t *testing.T) {
		s := newServer()
		ctx := sessionCtx(t, s, "session-A")

		entry, err := s.createTask(ctx, "task-match", "tool", nil, nil)
		require.NoError(t, err)

		_, _, err = s.getTask(ctx, entry.task.TaskId)
		assert.NoError(t, err, "same session should be allowed to access its own task")
	})

	t.Run("session mismatch — denied", func(t *testing.T) {
		s := newServer()
		ctxA := sessionCtx(t, s, "session-A")
		ctxB := sessionCtx(t, s, "session-B")

		entry, err := s.createTask(ctxA, "task-mismatch", "tool", nil, nil)
		require.NoError(t, err)

		_, _, err = s.getTask(ctxB, entry.task.TaskId)
		assert.EqualError(t, err, "task not found",
			"different session should be denied — returned as not-found to avoid enumeration")
	})

	t.Run("task has no session — bearer-token, any caller allowed", func(t *testing.T) {
		s := newServer()
		// Create task with no session in context → server-global task.
		entry, err := s.createTask(t.Context(), "task-global", "tool", nil, nil)
		require.NoError(t, err)
		assert.Empty(t, entry.sessionID, "task created without a session should have empty sessionID")

		// A session-bearing request can still access it via task ID alone.
		ctxB := sessionCtx(t, s, "session-B")
		_, _, err = s.getTask(ctxB, entry.task.TaskId)
		assert.NoError(t, err,
			"session-less task is bearer-token accessible: any caller who knows the ID may access it")
	})

	t.Run("request has no session — bearer-token, task's session not enforced", func(t *testing.T) {
		s := newServer()
		ctxA := sessionCtx(t, s, "session-A")

		entry, err := s.createTask(ctxA, "task-sessioned", "tool", nil, nil)
		require.NoError(t, err)
		assert.Equal(t, "session-A", entry.sessionID)

		// A session-less request (stdio, in-process) can access any task by ID.
		_, _, err = s.getTask(t.Context(), entry.task.TaskId)
		assert.NoError(t, err,
			"session-less request uses bearer-token model: task ID possession grants access")
	})

	t.Run("getTaskEntry applies the same policy as getTask", func(t *testing.T) {
		s := newServer()
		ctxA := sessionCtx(t, s, "session-A")
		ctxB := sessionCtx(t, s, "session-B")

		entry, err := s.createTask(ctxA, "task-entry-policy", "tool", nil, nil)
		require.NoError(t, err)

		_, err = s.getTaskEntry(ctxA, entry.task.TaskId)
		assert.NoError(t, err, "owner session: allowed")

		_, err = s.getTaskEntry(ctxB, entry.task.TaskId)
		assert.EqualError(t, err, "task not found", "different session: denied")
	})
}

// userKey is a context key used by the auth middleware tests below.
type userKey struct{}

// TestTaskAccessControl_HTTPMiddleware demonstrates how an HTTP middleware can
// enforce application-level authorization before requests reach the MCP server.
func TestTaskAccessControl_HTTPMiddleware(t *testing.T) {
	const validToken = "secret-token"

	mcpServer := NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))

	// authMiddleware validates the Authorization header. Unauthorized requests
	// are rejected before they reach HandleMessage.
	authMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if token != validToken {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	srv := httptest.NewServer(authMiddleware(NewStreamableHTTPServer(mcpServer)))
	t.Cleanup(srv.Close)

	doRequest := func(t *testing.T, token string) int {
		t.Helper()
		body, _ := json.Marshal(legacyInitRequest)
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL, strings.NewReader(string(body)))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		return resp.StatusCode
	}

	t.Run("valid token — request reaches MCP server", func(t *testing.T) {
		status := doRequest(t, validToken)
		assert.Equal(t, http.StatusOK, status,
			"valid token should pass middleware and receive an MCP response")
	})

	t.Run("missing token — rejected by middleware before reaching MCP server", func(t *testing.T) {
		status := doRequest(t, "")
		assert.Equal(t, http.StatusUnauthorized, status,
			"missing token should be rejected before reaching the MCP server")
	})

	t.Run("wrong token — rejected by middleware before reaching MCP server", func(t *testing.T) {
		status := doRequest(t, "wrong-token")
		assert.Equal(t, http.StatusUnauthorized, status,
			"invalid token should be rejected before reaching the MCP server")
	})
}

// TestTaskAccessControl_OnBeforeHookExtensionPoint demonstrates how an
// OnBefore* hook reads application identity from ctx to enforce per-user
// or per-tenant task authorization.
func TestTaskAccessControl_OnBeforeHookExtensionPoint(t *testing.T) {
	hooks := &Hooks{}

	var observedUser string
	hooks.AddBeforeGetTask(func(ctx context.Context, id any, req *mcp.GetTaskRequest) {
		// Read the identity attached by any ctx layer.
		if user, ok := ctx.Value(userKey{}).(string); ok {
			observedUser = user
		}
	})

	s := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
		WithHooks(hooks),
	)

	entry, err := s.createTask(t.Context(), "task-hook-ext", "tool", nil, nil)
	require.NoError(t, err)

	// Simulate transport middleware having attached "alice" to the context.
	ctxWithUser := context.WithValue(t.Context(), userKey{}, "alice")
	s.HandleMessage(ctxWithUser, []byte(
		`{"jsonrpc":"2.0","id":1,"method":"tasks/get","params":{"taskId":"`+entry.task.TaskId+`"}}`,
	))

	assert.Equal(t, "alice", observedUser,
		"hook should receive the identity attached by middleware — application can enforce authz here")
}

// TestTaskAccessControl_HTTPMiddlewareIdentityFlowsToHook demonstrates the
// complete end-to-end flow: HTTP middleware attaches identity to ctx →
// HandleMessage carries it through → OnBefore* hook reads it for per-request authz.
func TestTaskAccessControl_HTTPMiddlewareIdentityFlowsToHook(t *testing.T) {
	hooks := &Hooks{}

	var hookUser string
	hooks.AddBeforeGetTask(func(ctx context.Context, id any, req *mcp.GetTaskRequest) {
		if user, ok := ctx.Value(userKey{}).(string); ok {
			hookUser = user
		}
	})

	mcpServer := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
		WithHooks(hooks),
	)

	// Create a task without a session, bearer-token model, any caller can access it.
	entry, err := mcpServer.createTask(t.Context(), "task-flow", "tool", nil, nil)
	require.NoError(t, err)

	// Middleware attaches the bearer token as the caller's identity.
	authMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			ctx := context.WithValue(r.Context(), userKey{}, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	srv := httptest.NewServer(authMiddleware(NewStreamableHTTPServer(mcpServer)))
	t.Cleanup(srv.Close)

	// Use the legacy initialize handshake to get a valid session, then issue
	// tasks/get using that session ID.
	sessionID := initStatefulSession(t, srv.URL)

	body := `{"jsonrpc":"2.0","id":2,"method":"tasks/get","params":{"taskId":"` + entry.task.TaskId + `"}}`
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer bob")
	req.Header.Set(HeaderKeySessionID, sessionID)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "bob", hookUser,
		"identity set by HTTP middleware should flow through HandleMessage to the OnBefore* hook")
}
