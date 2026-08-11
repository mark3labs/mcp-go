package server

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listenSession records the notifications it is sent and tracks the
// subscription filter the server established.
type listenSession struct {
	clientInfoStore
	notify chan mcp.JSONRPCNotification

	filter mcp.SubscriptionFilter
	active bool

	subscribed []string
}

func newListenSession() *listenSession {
	return &listenSession{notify: make(chan mcp.JSONRPCNotification, 32)}
}

func (s *listenSession) SessionID() string { return "listen" }
func (s *listenSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return s.notify
}
func (s *listenSession) Initialize()       {}
func (s *listenSession) Initialized() bool { return true }

func (s *listenSession) SetSubscriptionFilter(filter mcp.SubscriptionFilter) {
	s.filter = filter
	s.active = !filter.IsEmpty()
}

func (s *listenSession) SubscriptionFilter() (mcp.SubscriptionFilter, bool) {
	return s.filter, s.active
}

func (s *listenSession) SubscribeToResource(uri string) {
	s.subscribed = append(s.subscribed, uri)
}
func (s *listenSession) UnsubscribeFromResource(uri string) {
	for i, existing := range s.subscribed {
		if existing == uri {
			s.subscribed = append(s.subscribed[:i], s.subscribed[i+1:]...)
			return
		}
	}
}
func (s *listenSession) SubscribedResources() []string { return s.subscribed }
func (s *listenSession) IsSubscribedToResource(uri string) bool {
	return slices.Contains(s.subscribed, uri)
}

var (
	_ ClientSession                    = (*listenSession)(nil)
	_ SessionWithSubscriptionFilter    = (*listenSession)(nil)
	_ SessionWithResourceSubscriptions = (*listenSession)(nil)
)

// listen runs subscriptions/listen in the background and returns a function
// that closes the stream and yields the result.
func listen(
	t *testing.T,
	srv *MCPServer,
	session *listenSession,
	filter mcp.SubscriptionFilter,
) (stop func() *mcp.SubscriptionsListenResult) {
	t.Helper()

	require.NoError(t, srv.RegisterSession(t.Context(), session))

	ctx, cancel := context.WithCancel(srv.WithContext(t.Context(), session))
	ctx = WithRequestProtocolInfo(ctx, &RequestProtocolInfo{
		Modern:          true,
		ProtocolVersion: mcp.ProtocolVersion20260728,
	})

	type outcome struct {
		result *mcp.SubscriptionsListenResult
		err    *requestError
	}
	done := make(chan outcome, 1)

	go func() {
		result, err := srv.handleSubscriptionsListen(ctx, 7, mcp.SubscriptionsListenRequest{
			Params: mcp.SubscriptionsListenParams{Notifications: filter},
		})
		done <- outcome{result, err}
	}()

	// Wait for the acknowledgement, which the server sends once the
	// subscription is established.
	select {
	case notification := <-session.notify:
		require.Equal(t, mcp.MethodNotificationSubscriptionsAcknowledged, notification.Method)
		assert.Equal(t, 7, notification.Params.Meta[mcp.MetaKeySubscriptionID],
			"the acknowledgement must identify the stream it belongs to")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subscriptions/acknowledged")
	}

	return func() *mcp.SubscriptionsListenResult {
		cancel()
		select {
		case got := <-done:
			require.Nil(t, got.err)
			return got.result
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for subscriptions/listen to close")
			return nil
		}
	}
}

func TestSubscriptionsListen_AcknowledgesAndCloses(t *testing.T) {
	srv := NewMCPServer("listen-test", "1.0.0", WithToolCapabilities(true))
	session := newListenSession()

	stop := listen(t, srv, session, mcp.SubscriptionFilter{ToolsListChanged: true})
	result := stop()

	// The result identifies the stream it closes.
	require.NotNil(t, result)
	assert.Equal(t, 7, result.Meta.SubscriptionID())
}

func TestSubscriptionsListen_FilterIsIntersectedWithCapabilities(t *testing.T) {
	// The server offers tools with listChanged, but no prompts at all.
	srv := NewMCPServer("listen-test", "1.0.0", WithToolCapabilities(true))
	session := newListenSession()

	stop := listen(t, srv, session, mcp.SubscriptionFilter{
		ToolsListChanged:   true,
		PromptsListChanged: true,
	})
	defer stop()

	filter, active := session.SubscriptionFilter()
	require.True(t, active)
	assert.True(t, filter.ToolsListChanged)
	assert.False(t, filter.PromptsListChanged,
		"a server must not establish a subscription it cannot serve")
}

func TestSubscriptionsListen_OnlyOptedInNotificationsAreDelivered(t *testing.T) {
	srv := NewMCPServer("listen-test", "1.0.0",
		WithToolCapabilities(true),
		WithPromptCapabilities(true),
	)
	session := newListenSession()

	stop := listen(t, srv, session, mcp.SubscriptionFilter{ToolsListChanged: true})
	defer stop()

	// The client opted in to tool list changes only.
	srv.sendNotificationToAllClients(mcp.JSONRPCNotification{
		JSONRPC:      mcp.JSONRPC_VERSION,
		Notification: mcp.Notification{Method: mcp.MethodNotificationPromptsListChanged},
	})
	srv.sendNotificationToAllClients(mcp.JSONRPCNotification{
		JSONRPC:      mcp.JSONRPC_VERSION,
		Notification: mcp.Notification{Method: mcp.MethodNotificationToolsListChanged},
	})

	select {
	case notification := <-session.notify:
		assert.Equal(t, mcp.MethodNotificationToolsListChanged, notification.Method,
			"the prompts notification must have been filtered out")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the opted-in notification")
	}
}

func TestSubscriptionsListen_ResourceSubscriptionsUseTheSessionStore(t *testing.T) {
	srv := NewMCPServer("listen-test", "1.0.0", WithResourceCapabilities(true, true))
	session := newListenSession()

	stop := listen(t, srv, session, mcp.SubscriptionFilter{
		ResourceSubscriptions: []string{"file:///a", "file:///b"},
	})

	// The URIs flow into the existing per-session subscription store, so
	// notifications/resources/updated fan-out is unchanged.
	assert.True(t, session.IsSubscribedToResource("file:///a"))
	assert.True(t, session.IsSubscribedToResource("file:///b"))

	stop()

	// Closing the stream releases them.
	assert.Empty(t, session.SubscribedResources())
}

func TestSubscriptionsListen_EmptyFilterClosesImmediately(t *testing.T) {
	// The server advertises nothing the client asked for, so there is nothing
	// to hold the stream open for.
	srv := NewMCPServer("listen-test", "1.0.0")
	session := newListenSession()
	require.NoError(t, srv.RegisterSession(t.Context(), session))

	ctx := srv.WithContext(t.Context(), session)
	ctx = WithRequestProtocolInfo(ctx, &RequestProtocolInfo{
		Modern:          true,
		ProtocolVersion: mcp.ProtocolVersion20260728,
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		result, err := srv.handleSubscriptionsListen(ctx, 1, mcp.SubscriptionsListenRequest{
			Params: mcp.SubscriptionsListenParams{
				Notifications: mcp.SubscriptionFilter{ToolsListChanged: true},
			},
		})
		assert.Nil(t, err)
		assert.NotNil(t, result)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("subscriptions/listen should not block when nothing was established")
	}
}

func TestSubscriptionsListen_RequiresRequestID(t *testing.T) {
	srv := NewMCPServer("listen-test", "1.0.0", WithToolCapabilities(true))
	session := newListenSession()

	ctx := srv.WithContext(t.Context(), session)
	_, err := srv.handleSubscriptionsListen(ctx, nil, mcp.SubscriptionsListenRequest{})
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "request ID")
}

func TestSubscriptionsListen_LegacySessionsAreUnfiltered(t *testing.T) {
	// A session that never opened a subscription stream is a legacy session:
	// notifications are not opt-in there, so nothing is filtered.
	session := newListenSession()
	assert.True(t, subscriptionAllowsNotification(session, mcp.MethodNotificationToolsListChanged))
	assert.True(t, subscriptionAllowsNotification(session, mcp.MethodNotificationPromptsListChanged))
}
