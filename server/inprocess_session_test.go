package server

import (
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
