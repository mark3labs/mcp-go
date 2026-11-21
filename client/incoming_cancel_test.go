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
