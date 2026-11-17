package server

import (
	"context"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// In-process session IDs are unique however fast they are generated: the
// wall clock alone is not, as it moves in microseconds on some platforms.
func TestGenerateInProcessSessionIDIsUnique(t *testing.T) {
	const workers, each = 4, 2500
	ids := make(chan string, workers*each)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for range each {
				ids <- GenerateInProcessSessionID()
			}
		})
	}
	wg.Wait()
	close(ids)
	seen := make(map[string]bool, workers*each)
	for id := range ids {
		assert.False(t, seen[id], "session ID %q was generated twice", id)
		seen[id] = true
	}
}

func TestInProcessSession_SubscriptionFilter(t *testing.T) {
	session := NewInProcessSession("subscription-test", nil)
	_, active := session.SubscriptionFilter()
	require.False(t, active)
	require.True(t, subscriptionAllowsNotification(session, mcp.MethodNotificationToolsListChanged))

	filter := mcp.SubscriptionFilter{ResourceSubscriptions: []string{"file:///watched"}}
	session.SetSubscriptionFilter(filter)
	filter.ResourceSubscriptions[0] = "file:///caller-mutation"
	got, active := session.SubscriptionFilter()
	require.True(t, active, "a resource-only subscription is active")
	require.Equal(t, []string{"file:///watched"}, got.ResourceSubscriptions)
	require.False(t, subscriptionAllowsNotification(session, mcp.MethodNotificationToolsListChanged))

	got.ResourceSubscriptions[0] = "file:///snapshot-mutation"
	got, _ = session.SubscriptionFilter()
	require.Equal(t, []string{"file:///watched"}, got.ResourceSubscriptions)

	session.SetSubscriptionFilter(mcp.SubscriptionFilter{})
	got, active = session.SubscriptionFilter()
	require.False(t, active)
	require.True(t, got.IsEmpty())
}

func TestInProcessSession_CanceledSubscriptionCannotReplaceFilter(t *testing.T) {
	session := NewInProcessSession("replacement", nil)
	filter := mcp.SubscriptionFilter{ToolsListChanged: true}
	clearCurrent := session.setOwnedSubscriptionFilter(t.Context(), "current", filter)
	defer clearCurrent()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	checked := false
	ctx = &subscriptionLockCheckingContext{Context: ctx, check: func() {
		checked = true
		if session.mu.TryLock() {
			session.mu.Unlock()
			t.Error("cancellation must be checked while holding the session lock")
		}
	}}
	clearCanceled := session.setOwnedSubscriptionFilter(ctx, "old", mcp.SubscriptionFilter{PromptsListChanged: true})
	got, active := session.SubscriptionFilter()
	require.True(t, active)
	require.Equal(t, filter, got)
	clearCanceled()
	got, active = session.SubscriptionFilter()
	require.True(t, active)
	require.Equal(t, filter, got)
	require.True(t, checked)
}

type subscriptionLockCheckingContext struct {
	context.Context
	check func()
}

func (c *subscriptionLockCheckingContext) Err() error {
	c.check()
	return c.Context.Err()
}

func TestInProcessSession_BroadcastSubscriptionMetadata(t *testing.T) {
	srv := NewMCPServer("metadata", "1.0")
	first := NewInProcessSession("first", nil)
	second := NewInProcessSession("second", nil)
	legacy := NewInProcessSession("legacy", nil)
	filter := mcp.SubscriptionFilter{ToolsListChanged: true}
	clearFirst := first.setOwnedSubscriptionFilter(t.Context(), "first-listen", filter)
	clearSecond := second.setOwnedSubscriptionFilter(t.Context(), "second-listen", filter)
	defer clearSecond()
	notification := mcp.JSONRPCNotification{
		Notification: mcp.Notification{
			Method: mcp.MethodNotificationToolsListChanged,
			Params: mcp.NotificationParams{Meta: map[string]any{"other": "preserved"}},
		},
	}
	for _, tt := range []struct {
		session *InProcessSession
		id      any
	}{
		{first, "first-listen"},
		{second, "second-listen"},
		{legacy, nil},
	} {
		srv.broadcastToSession(tt.session, notification)
		got := <-tt.session.ClientNotifications()
		require.Equal(t, tt.id, got.Params.Meta[mcp.MetaKeySubscriptionID])
		require.Equal(t, "preserved", got.Params.Meta["other"])
	}
	require.Equal(t, map[string]any{"other": "preserved"}, notification.Params.Meta)

	clearFirst()
	srv.broadcastToSession(first, notification)
	got := <-first.ClientNotifications()
	require.NotContains(t, got.Params.Meta, mcp.MetaKeySubscriptionID)
}

// An unlocked acknowledgement enqueue lets a concurrent broadcast overtake it.
type acknowledgementOrderingSession struct {
	*InProcessSession
	beforeAck func()
}

func (s *acknowledgementOrderingSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	if s.beforeAck != nil {
		f := s.beforeAck
		s.beforeAck = nil
		f()
	}
	return s.InProcessSession.NotificationChannel()
}

func TestInProcessSession_AcknowledgementPrecedesBroadcast(t *testing.T) {
	srv := NewMCPServer("ordering", "1.0", WithToolCapabilities(true))
	session := &acknowledgementOrderingSession{InProcessSession: NewInProcessSession("ordering", nil)}
	session.Initialize()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx = srv.WithContext(ctx, session)
	session.beforeAck = func() {
		if session.mu.TryLock() {
			session.mu.Unlock()
			srv.broadcastToSession(session, mcp.JSONRPCNotification{Notification: mcp.Notification{Method: mcp.MethodNotificationToolsListChanged}})
		}
		cancel()
	}
	request := mcp.SubscriptionsListenRequest{}
	request.Params.Notifications.ToolsListChanged = true
	_, err := srv.handleSubscriptionsListen(ctx, "listen", request)
	require.Nil(t, err)
	first := <-session.ClientNotifications()
	require.Equal(t, mcp.MethodNotificationSubscriptionsAcknowledged, first.Method)
}

func TestInProcessSession_TargetedListChanges(t *testing.T) {
	srv := NewMCPServer("targeted", "1.0")
	session := NewInProcessSession("targeted", nil)
	session.Initialize()
	srv.sessions.Store(session.SessionID(), session)
	clear := session.setOwnedSubscriptionFilter(t.Context(), "listen", mcp.SubscriptionFilter{PromptsListChanged: true})
	defer clear()
	for _, method := range []string{mcp.MethodNotificationToolsListChanged, mcp.MethodNotificationResourcesListChanged, mcp.MethodNotificationPromptsListChanged, string(mcp.MethodNotificationProgress)} {
		require.NoError(t, srv.SendNotificationToSpecificClient(session.SessionID(), method, nil))
	}
	first := <-session.ClientNotifications()
	require.Equal(t, mcp.MethodNotificationPromptsListChanged, first.Method)
	require.Equal(t, "listen", first.Params.Meta[mcp.MetaKeySubscriptionID])
	second := <-session.ClientNotifications()
	require.Equal(t, string(mcp.MethodNotificationProgress), second.Method)
	require.NotContains(t, second.Params.Meta, mcp.MetaKeySubscriptionID)
}

func TestInProcessSession_FailedAcknowledgementPreservesSubscription(t *testing.T) {
	session := NewInProcessSession("failed-ack", nil)
	filter := mcp.SubscriptionFilter{ToolsListChanged: true}
	clear := session.setOwnedSubscriptionFilter(t.Context(), "current", filter)
	defer clear()
	cleanup, err := session.activateSubscription(t.Context(), "replacement", mcp.SubscriptionFilter{PromptsListChanged: true}, func() error {
		return ErrNotificationChannelBlocked
	})
	require.ErrorIs(t, err, ErrNotificationChannelBlocked)
	cleanup()
	got, active := session.SubscriptionFilter()
	require.True(t, active)
	require.Equal(t, filter, got)
	notification, allowed := session.subscriptionNotification(mcp.JSONRPCNotification{Notification: mcp.Notification{Method: mcp.MethodNotificationToolsListChanged}})
	require.True(t, allowed)
	require.Equal(t, "current", notification.Params.Meta[mcp.MetaKeySubscriptionID])
}
