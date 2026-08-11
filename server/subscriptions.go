package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// subscriptionsListenKey carries the JSON-RPC ID of the subscriptions/listen
// request that opened the current notification stream.
type subscriptionsListenKey struct{}

// WithSubscriptionID returns a context tagged with the JSON-RPC ID of the
// subscriptions/listen request that opened the current stream.
func WithSubscriptionID(ctx context.Context, id any) context.Context {
	return context.WithValue(ctx, subscriptionsListenKey{}, id)
}

// SubscriptionIDFromContext returns the JSON-RPC ID of the
// subscriptions/listen request that opened the current stream, or nil when the
// request is not a subscription stream.
func SubscriptionIDFromContext(ctx context.Context) any {
	return ctx.Value(subscriptionsListenKey{})
}

// SessionWithSubscriptionFilter is implemented by sessions that can record
// which notification types a client opted in to through subscriptions/listen.
//
// Protocol version 2026-07-28 makes every server-to-client notification
// opt-in: a server MUST NOT deliver a notification type the client did not
// explicitly request (SEP-2575).
type SessionWithSubscriptionFilter interface {
	ClientSession
	// SetSubscriptionFilter records the notification types the client opted
	// in to. Passing the zero filter clears the subscription.
	SetSubscriptionFilter(filter mcp.SubscriptionFilter)
	// SubscriptionFilter returns the notification types the client opted in
	// to, and whether a subscription is currently active.
	SubscriptionFilter() (mcp.SubscriptionFilter, bool)
}

// handleSubscriptionsListen serves the subscriptions/listen RPC introduced in
// protocol version 2026-07-28 (SEP-2575).
//
// It replaces the standalone HTTP GET stream and the
// resources/subscribe and resources/unsubscribe RPCs with a single long-lived
// response stream. The server intersects the client's requested filter with
// its own capabilities, acknowledges the result, and then blocks, delivering
// notifications on the stream until the client cancels the request or
// disconnects.
func (s *MCPServer) handleSubscriptionsListen(
	ctx context.Context,
	id any,
	request mcp.SubscriptionsListenRequest,
) (*mcp.SubscriptionsListenResult, *requestError) {
	if id == nil {
		return nil, &requestError{
			id:   id,
			code: mcp.INVALID_REQUEST,
			err:  errors.New("subscriptions/listen requires a request ID"),
		}
	}

	session := ClientSessionFromContext(ctx)
	if session == nil {
		return nil, &requestError{
			id:   id,
			code: mcp.INTERNAL_ERROR,
			err:  errors.New("subscriptions/listen requires an active session"),
		}
	}

	allowed := s.allowedSubscriptions(request.Params.Notifications)

	// Record the filter so notification fan-out can honour the opt-in.
	if filtered, ok := session.(SessionWithSubscriptionFilter); ok {
		filtered.SetSubscriptionFilter(allowed)
		defer filtered.SetSubscriptionFilter(mcp.SubscriptionFilter{})
	}

	// Resource subscriptions are expressed through the existing per-session
	// subscription store, so notifications/resources/updated fan-out is
	// unchanged.
	if subs, ok := session.(SessionWithResourceSubscriptions); ok {
		for _, uri := range allowed.ResourceSubscriptions {
			if errSubs, ok := session.(SessionWithResourceSubscriptionsErr); ok {
				if err := errSubs.SubscribeToResourceErr(uri); err != nil {
					return nil, &requestError{
						id:   id,
						code: mcp.INVALID_PARAMS,
						err:  fmt.Errorf("subscribing to %q: %w", uri, err),
					}
				}
				continue
			}
			subs.SubscribeToResource(uri)
		}
		defer func() {
			for _, uri := range allowed.ResourceSubscriptions {
				subs.UnsubscribeFromResource(uri)
			}
		}()
	}

	// Acknowledge the subscription so the client learns which of its requested
	// notification types were actually established.
	ack := mcp.SubscriptionsAcknowledgedParams{
		Notifications: allowed,
		Meta:          map[string]any{mcp.MetaKeySubscriptionID: id},
	}
	if err := s.SendNotificationToSpecificClient(
		session.SessionID(),
		mcp.MethodNotificationSubscriptionsAcknowledged,
		map[string]any{
			"notifications": ack.Notifications,
			"_meta":         ack.Meta,
		},
	); err != nil {
		return nil, &requestError{
			id:   id,
			code: mcp.INTERNAL_ERROR,
			err:  fmt.Errorf("sending subscriptions/acknowledged: %w", err),
		}
	}

	// Hold the stream open for as long as anything was subscribed. When the
	// filter is empty there is nothing to deliver, so close immediately.
	if !allowed.IsEmpty() {
		<-ctx.Done()
	}

	result := &mcp.SubscriptionsListenResult{}
	meta := result.EnsureResultMeta()
	meta.SetSubscriptionID(id)
	return result, nil
}

// allowedSubscriptions intersects the notification types a client asked for
// with the capabilities this server actually advertises. The server MUST NOT
// establish a subscription it cannot serve.
func (s *MCPServer) allowedSubscriptions(want mcp.SubscriptionFilter) mcp.SubscriptionFilter {
	s.capabilitiesMu.RLock()
	defer s.capabilitiesMu.RUnlock()

	var allowed mcp.SubscriptionFilter
	if want.ToolsListChanged && s.capabilities.tools != nil && s.capabilities.tools.listChanged {
		allowed.ToolsListChanged = true
	}
	if want.PromptsListChanged && s.capabilities.prompts != nil && s.capabilities.prompts.listChanged {
		allowed.PromptsListChanged = true
	}
	if want.ResourcesListChanged && s.capabilities.resources != nil && s.capabilities.resources.listChanged {
		allowed.ResourcesListChanged = true
	}
	if len(want.ResourceSubscriptions) > 0 && s.capabilities.resources != nil && s.capabilities.resources.subscribe {
		allowed.ResourceSubscriptions = append([]string(nil), want.ResourceSubscriptions...)
	}
	return allowed
}

// subscriptionAllowsNotification reports whether a session that opted in
// through subscriptions/listen should receive the given notification method.
//
// Sessions that never opened a subscription stream are unaffected: they are
// legacy sessions, where notifications are not opt-in.
func subscriptionAllowsNotification(session ClientSession, method string) bool {
	filtered, ok := session.(SessionWithSubscriptionFilter)
	if !ok {
		return true
	}
	filter, active := filtered.SubscriptionFilter()
	if !active {
		return true
	}
	switch method {
	case mcp.MethodNotificationToolsListChanged:
		return filter.ToolsListChanged
	case mcp.MethodNotificationPromptsListChanged:
		return filter.PromptsListChanged
	case mcp.MethodNotificationResourcesListChanged:
		return filter.ResourcesListChanged
	default:
		return true
	}
}
