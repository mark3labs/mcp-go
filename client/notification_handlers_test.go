package client

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dispatchNotification(t *testing.T, handler func(mcp.JSONRPCNotification), notification mcp.JSONRPCNotification) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		handler(notification)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("notification dispatch blocked")
	}
}

func TestClient_NotificationHandlerCanRegisterHandler(t *testing.T) {
	mockTrans := &mockProtocolTransport{}
	client := NewClient(mockTrans)
	require.NoError(t, client.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	var callOrder []int
	registered := false
	client.OnNotification(func(mcp.JSONRPCNotification) {
		callOrder = append(callOrder, 1)
		if !registered {
			registered = true
			client.OnNotification(func(mcp.JSONRPCNotification) {
				callOrder = append(callOrder, 3)
			})
		}
	})
	client.OnNotification(func(mcp.JSONRPCNotification) {
		callOrder = append(callOrder, 2)
	})

	notification := mcp.JSONRPCNotification{
		JSONRPC:      mcp.JSONRPC_VERSION,
		Notification: mcp.Notification{Method: "test-method"},
	}
	dispatchNotification(t, mockTrans.notificationHandler, notification)
	assert.Equal(t, []int{1, 2}, callOrder)
	dispatchNotification(t, mockTrans.notificationHandler, notification)
	assert.Equal(t, []int{1, 2, 1, 2, 3}, callOrder)
}

func TestClient_ConcurrentNotificationRegistrationAndDispatch(t *testing.T) {
	mockTrans := &mockProtocolTransport{}
	client := NewClient(mockTrans)
	require.NoError(t, client.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	var calls atomic.Int64
	client.OnNotification(func(mcp.JSONRPCNotification) { calls.Add(1) })
	notification := mcp.JSONRPCNotification{
		JSONRPC:      mcp.JSONRPC_VERSION,
		Notification: mcp.Notification{Method: "test-method"},
	}
	const registrations = 64
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(5)
	go func() {
		defer workers.Done()
		<-start
		for range registrations {
			client.OnNotification(func(mcp.JSONRPCNotification) { calls.Add(1) })
		}
	}()
	for range 4 {
		go func() {
			defer workers.Done()
			<-start
			for range 32 {
				mockTrans.notificationHandler(notification)
			}
		}()
	}
	close(start)
	done := make(chan struct{})
	go func() {
		workers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent notification registration or dispatch blocked")
	}

	before := calls.Load()
	dispatchNotification(t, mockTrans.notificationHandler, notification)
	assert.Equal(t, int64(registrations+1), calls.Load()-before)
}
