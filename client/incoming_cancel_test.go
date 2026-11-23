package client

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// blockingSamplingHandler simulates a long-running server-request handler,
// like an elicitation dialog the user has not answered yet.
type blockingSamplingHandler struct{}

func (blockingSamplingHandler) CreateMessage(ctx context.Context, _ mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// When the server withdraws a sampling request with notifications/cancelled,
// the client ends the handler's context, so a handler waiting on it, an
// open elicitation dialog for example, returns early instead of outliving a
// request whose response nobody waits for.
func TestClient_ServerCancelledRequestEndsHandlerContext(t *testing.T) {
	mt := newMockTransport()
	client := &Client{
		transport:       mt,
		samplingHandler: blockingSamplingHandler{},
	}
	require.NoError(t, client.Start(t.Context()))

	request := transport.JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(42)),
		Method:  string(mcp.MethodSamplingCreateMessage),
		Params: map[string]any{
			"messages":   []any{},
			"maxTokens":  10,
			"modelHints": []any{},
		},
	}
	handlerDone := make(chan error, 1)
	go func() {
		_, err := client.handleIncomingRequest(t.Context(), request)
		handlerDone <- err
	}()

	require.Eventually(t, func() bool {
		client.incomingMu.Lock()
		defer client.incomingMu.Unlock()
		_, ok := client.incomingRequests[request.ID.String()]
		return ok
	}, 5*time.Second, time.Millisecond, "the in-flight request should be registered")

	// The server gives up on the request it issued.
	require.NotNil(t, mt.notificationHandler, "Start should have registered the notification handler")
	mt.notificationHandler(mcp.JSONRPCNotification{
		JSONRPC: mcp.JSONRPC_VERSION,
		Notification: mcp.Notification{
			Method: string(mcp.MethodNotificationCancelled),
			Params: mcp.NotificationParams{
				AdditionalFields: map[string]any{"requestId": float64(42), "reason": context.Canceled.Error()},
			},
		},
	})

	select {
	case err := <-handlerDone:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("the handler did not return after the cancellation")
	}

	client.incomingMu.Lock()
	assert.Empty(t, client.incomingRequests, "the request should be deregistered when the handler returns")
	client.incomingMu.Unlock()
}

// cancelNotification is the notifications/cancelled a server sends to
// withdraw its request id.
func cancelNotification(id int64) mcp.JSONRPCNotification {
	return mcp.JSONRPCNotification{
		JSONRPC: mcp.JSONRPC_VERSION,
		Notification: mcp.Notification{
			Method: string(mcp.MethodNotificationCancelled),
			Params: mcp.NotificationParams{
				AdditionalFields: map[string]any{"requestId": float64(id), "reason": context.Canceled.Error()},
			},
		},
	}
}

// samplingRequest is a server request the blocking handler waits on.
func samplingRequest(id int64) transport.JSONRPCRequest {
	return transport.JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(id),
		Method:  string(mcp.MethodSamplingCreateMessage),
		Params: map[string]any{
			"messages":   []any{},
			"maxTokens":  10,
			"modelHints": []any{},
		},
	}
}

// The stdio and Streamable HTTP transports start request handlers in
// goroutines, so a notifications/cancelled can be processed before the
// request is registered. The cancellation must hold anyway: the client
// remembers it, and the registration applies it immediately. The order is
// forced here: the cancellation is fully processed before the handler runs.
func TestClient_CancellationBeforeRegistrationEndsHandlerContext(t *testing.T) {
	mt := newMockTransport()
	client := &Client{
		transport:       mt,
		samplingHandler: blockingSamplingHandler{},
	}
	require.NoError(t, client.Start(t.Context()))
	require.NotNil(t, mt.notificationHandler, "Start should have registered the notification handler")

	mt.notificationHandler(cancelNotification(43))
	client.incomingMu.Lock()
	require.True(t, client.earlyCancels["int64:43"], "the cancellation of an unregistered request must be remembered")
	client.incomingMu.Unlock()

	handlerDone := make(chan error, 1)
	go func() {
		_, err := client.handleIncomingRequest(t.Context(), samplingRequest(43))
		handlerDone <- err
	}()

	select {
	case err := <-handlerDone:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("the handler did not return after the cancellation")
	}

	client.incomingMu.Lock()
	assert.Empty(t, client.incomingRequests, "the request should be deregistered when the handler returns")
	assert.Empty(t, client.earlyCancels, "the early cancellation should be consumed by the registration")
	assert.Empty(t, client.earlyCancelOrder, "the consumed cancellation should leave the eviction order")
	client.incomingMu.Unlock()
}

// Cancellations that arrive after their handler returned are never consumed.
// They must not fill the buffer for good: at the bound the oldest entry is
// evicted, so the newest cancellation, the one that can still beat a
// registration, is always remembered and still ends its handler.
func TestClient_FullEarlyCancelBufferEvictsOldest(t *testing.T) {
	mt := newMockTransport()
	client := &Client{
		transport:       mt,
		samplingHandler: blockingSamplingHandler{},
	}
	require.NoError(t, client.Start(t.Context()))
	require.NotNil(t, mt.notificationHandler, "Start should have registered the notification handler")

	// More stale cancellations than the buffer holds.
	for id := int64(1); id <= maxEarlyCancels+10; id++ {
		mt.notificationHandler(cancelNotification(id))
	}
	client.incomingMu.Lock()
	require.Len(t, client.earlyCancels, maxEarlyCancels, "the buffer must stay bounded")
	require.Len(t, client.earlyCancelOrder, maxEarlyCancels)
	require.False(t, client.earlyCancels["int64:1"], "the oldest stale cancellation must be evicted")
	require.True(t, client.earlyCancels["int64:1034"], "the newest cancellation must be remembered")
	client.incomingMu.Unlock()

	// A cancellation that beats its registration after the buffer filled
	// still ends the handler.
	const fresh = int64(5000)
	mt.notificationHandler(cancelNotification(fresh))
	handlerDone := make(chan error, 1)
	go func() {
		_, err := client.handleIncomingRequest(t.Context(), samplingRequest(fresh))
		handlerDone <- err
	}()
	select {
	case err := <-handlerDone:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("a cancellation arriving after the buffer filled was lost")
	}
}
