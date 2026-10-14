package client

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

type pendingSubscriptionTransport struct {
	params json.RawMessage
	result json.RawMessage
}

func (s *pendingSubscriptionTransport) Start(context.Context) error { return nil }
func (s *pendingSubscriptionTransport) SendNotification(context.Context, mcp.JSONRPCNotification) error {
	return nil
}
func (s *pendingSubscriptionTransport) SetNotificationHandler(func(mcp.JSONRPCNotification)) {}
func (s *pendingSubscriptionTransport) Close() error                                         { return nil }
func (s *pendingSubscriptionTransport) GetSessionId() string                                 { return "" }
func (s *pendingSubscriptionTransport) SendRequest(_ context.Context, r transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	var err error
	s.params, err = json.Marshal(r.Params)
	if err != nil {
		return nil, err
	}
	return &transport.JSONRPCResponse{JSONRPC: "2.0", ID: r.ID, Result: s.result}, nil
}

func TestListenIncludesPendingResourceSubscriptions(t *testing.T) {
	tr := &pendingSubscriptionTransport{result: json.RawMessage(`{}`)}
	c := NewClient(tr)
	c.initialized.Store(true)
	c.protocolVersion = mcp.ProtocolVersion20260728
	req := mcp.SubscribeRequest{}
	req.Params.URI = "file:///pending"
	require.NoError(t, c.Subscribe(t.Context(), req))
	require.NoError(t, c.Listen(t.Context(), mcp.SubscriptionFilter{ToolsListChanged: true, ResourceSubscriptions: []string{"file:///explicit"}}))
	var got mcp.SubscriptionsListenParams
	require.NoError(t, json.Unmarshal(tr.params, &got))
	require.ElementsMatch(t, []string{"file:///pending", "file:///explicit"}, got.Notifications.ResourceSubscriptions)
	require.True(t, got.Notifications.ToolsListChanged)
}

func TestListenCanNarrowExplicitSubscriptions(t *testing.T) {
	tr := &pendingSubscriptionTransport{result: json.RawMessage(`{}`)}
	c := NewClient(tr)
	c.initialized.Store(true)
	c.protocolVersion = mcp.ProtocolVersion20260728
	require.NoError(t, c.Listen(t.Context(), mcp.SubscriptionFilter{ResourceSubscriptions: []string{"file:///old"}}))
	require.NoError(t, c.Listen(t.Context(), mcp.SubscriptionFilter{ResourceSubscriptions: []string{"file:///new"}}))
	var got mcp.SubscriptionsListenParams
	require.NoError(t, json.Unmarshal(tr.params, &got))
	require.Equal(t, []string{"file:///new"}, got.Notifications.ResourceSubscriptions)
}
