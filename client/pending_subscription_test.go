package client

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
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

// racingListenTransport serializes the outgoing params the way a real
// transport does, outside the subscription mutex. Marshalling repeatedly
// widens the window in which the request's ResourceSubscriptions backing
// array is read.
type racingListenTransport struct {
	result json.RawMessage
}

func (s *racingListenTransport) Start(context.Context) error { return nil }
func (s *racingListenTransport) SendNotification(context.Context, mcp.JSONRPCNotification) error {
	return nil
}
func (s *racingListenTransport) SetNotificationHandler(func(mcp.JSONRPCNotification)) {}
func (s *racingListenTransport) Close() error                                         { return nil }
func (s *racingListenTransport) GetSessionId() string                                 { return "" }
func (s *racingListenTransport) SendRequest(_ context.Context, r transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	for range 64 {
		if _, err := json.Marshal(r.Params); err != nil {
			return nil, err
		}
	}
	return &transport.JSONRPCResponse{JSONRPC: "2.0", ID: r.ID, Result: s.result}, nil
}

// TestListenAndUnsubscribeDoNotRaceOnFilter covers the ownership boundary
// between the filter Listen sends and the filter the client retains.
//
// Listen releases the subscription mutex before the transport serializes the
// request, so the outgoing ResourceSubscriptions slice must not share a
// backing array with the retained filter that Unsubscribe mutates in place.
// Run under -race.
func TestListenAndUnsubscribeDoNotRaceOnFilter(t *testing.T) {
	uris := make([]string, 0, 32)
	for i := range 32 {
		uris = append(uris, fmt.Sprintf("file:///resource-%02d", i))
	}

	tr := &racingListenTransport{result: json.RawMessage(`{}`)}
	c := NewClient(tr)
	c.initialized.Store(true)
	c.protocolVersion = mcp.ProtocolVersion20260728

	for _, uri := range uris {
		req := mcp.SubscribeRequest{}
		req.Params.URI = uri
		require.NoError(t, c.Subscribe(t.Context(), req))
	}

	var wg sync.WaitGroup
	for range 40 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = c.Listen(t.Context(), mcp.SubscriptionFilter{ResourceSubscriptions: uris})
		}()
		go func() {
			defer wg.Done()
			for _, uri := range uris {
				req := mcp.UnsubscribeRequest{}
				req.Params.URI = uri
				require.NoError(t, c.Unsubscribe(t.Context(), req))
			}
		}()
	}
	wg.Wait()
}
