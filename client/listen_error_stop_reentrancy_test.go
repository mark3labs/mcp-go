package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

type failedListenTransport struct{}

func (*failedListenTransport) Start(context.Context) error { return nil }
func (*failedListenTransport) SendNotification(context.Context, mcp.JSONRPCNotification) error {
	return nil
}
func (*failedListenTransport) SetNotificationHandler(func(mcp.JSONRPCNotification)) {}
func (*failedListenTransport) Close() error                                         { return nil }
func (*failedListenTransport) GetSessionId() string                                 { return "" }
func (*failedListenTransport) SendRequest(context.Context, transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	return nil, errors.New("listen failed")
}

func TestListenAsyncErrorHandlerCanStopListener(t *testing.T) {
	c := NewClient(&failedListenTransport{})
	c.initialized.Store(true)
	c.protocolVersion = mcp.ProtocolVersion20260728
	ready := make(chan struct{})
	observed := make(chan struct{})
	var stop func()
	var err error
	stop, err = c.ListenAsync(t.Context(), mcp.SubscriptionFilter{ToolsListChanged: true}, func(error) {
		<-ready
		stop()
		close(observed)
	})
	require.NoError(t, err)
	close(ready)
	select {
	case <-observed:
	case <-time.After(time.Second):
		t.Fatal("error handler deadlocked stopping its failed listener")
	}
	stop()
}
