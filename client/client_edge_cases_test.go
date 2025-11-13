package client

import (
	"context"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClient_UnsupportedProtocolVersionResponse tests that client rejects unsupported protocol versions
func TestClient_UnsupportedProtocolVersionResponse(t *testing.T) {
	// Create mock transport
	mockTrans := newMockTransport()

	// Create client
	client := &Client{
		transport: mockTrans,
	}

	ctx := context.Background()
	err := client.Start(ctx)
	require.NoError(t, err)

	// Server responds with an unsupported/invalid protocol version
	initResponse := transport.NewJSONRPCResultResponse(
		mcp.NewRequestId(1),
		[]byte(`{"protocolVersion":"9999-99-99","capabilities":{},"serverInfo":{"name":"test-server","version":"1.0.0"}}`),
	)

	go func() {
		mockTrans.responseChan <- initResponse
	}()

	initRequest := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "test-client",
				Version: "1.0.0",
			},
		},
	}

	_, err = client.Initialize(ctx, initRequest)
	require.Error(t, err)

	// Should be an UnsupportedProtocolVersionError
	var unsupportedErr mcp.UnsupportedProtocolVersionError
	assert.ErrorAs(t, err, &unsupportedErr)
	assert.Equal(t, "9999-99-99", unsupportedErr.Version)
}

// TestClient_OperationsBeforeInitialize tests operations fail before initialization
func TestClient_OperationsBeforeInitialize(t *testing.T) {
	mockTrans := newMockTransport()
	client := &Client{
		transport: mockTrans,
	}

	ctx := context.Background()
	err := client.Start(ctx)
	require.NoError(t, err)

	// Try to send request before initialization
	err = client.Ping(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")

	// List tools should also fail
	_, err = client.ListTools(ctx, mcp.ListToolsRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")

	// List resources should also fail
	_, err = client.ListResources(ctx, mcp.ListResourcesRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

// TestClient_NotificationHandlers tests notification handler behavior
func TestClient_NotificationHandlers(t *testing.T) {
	t.Run("multiple handlers called in order", func(t *testing.T) {
		mockTrans := newMockTransport()
		client := &Client{
			transport: mockTrans,
		}

		ctx := context.Background()
		err := client.Start(ctx)
		require.NoError(t, err)

		var callOrder []int
		var handlerCalls int

		// Register multiple handlers
		for i := 0; i < 3; i++ {
			handlerID := i
			client.OnNotification(func(notification mcp.JSONRPCNotification) {
				callOrder = append(callOrder, handlerID)
				handlerCalls++
			})
		}

		// Simulate notification via the handler
		notif := mcp.JSONRPCNotification{
			JSONRPC: mcp.JSONRPC_VERSION,
			Notification: mcp.Notification{
				Method: "test-method",
			},
		}

		// Manually trigger the handlers we registered on the client
		// Access them through the read lock
		client.notifyMu.RLock()
		handlers := make([]func(mcp.JSONRPCNotification), len(client.notifications))
		copy(handlers, client.notifications)
		client.notifyMu.RUnlock()

		for _, h := range handlers {
			h(notif)
		}

		// Wait a bit for handlers to execute
		time.Sleep(50 * time.Millisecond)

		// All handlers should have been called in order
		assert.Equal(t, []int{0, 1, 2}, callOrder)
		assert.Equal(t, 3, handlerCalls)
	})
}

// TestClient_GetSessionId tests session ID retrieval
func TestClient_GetSessionId(t *testing.T) {
	mockTrans := newMockTransport()
	client := &Client{
		transport: mockTrans,
	}

	// Should return the transport's session ID
	sessionID := client.GetSessionId()
	assert.Equal(t, "mock-session-id", sessionID)
}

// TestClient_IsInitialized tests initialization state tracking
func TestClient_IsInitialized(t *testing.T) {
	mockTrans := newMockTransport()
	client := &Client{
		transport: mockTrans,
	}

	// Should not be initialized initially
	assert.False(t, client.IsInitialized())

	ctx := context.Background()
	err := client.Start(ctx)
	require.NoError(t, err)

	// Still not initialized after Start
	assert.False(t, client.IsInitialized())

	// Initialize
	initResponse := transport.NewJSONRPCResultResponse(
		mcp.NewRequestId(1),
		[]byte(`{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"test-server","version":"1.0.0"}}`),
	)
	go func() {
		mockTrans.responseChan <- initResponse
		mockTrans.responseChan <- transport.NewJSONRPCResultResponse(mcp.NewRequestId(2), []byte(`{}`))
	}()

	_, err = client.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "test-client",
				Version: "1.0.0",
			},
		},
	})
	require.NoError(t, err)

	// Should be initialized now
	assert.True(t, client.IsInitialized())
}
