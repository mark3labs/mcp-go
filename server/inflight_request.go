package server

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

type inflightRequestKey struct {
	sessionID string
	requestID string
}

func inflightKey(ctx context.Context, requestID any) inflightRequestKey {
	return inflightRequestKey{
		sessionID: getSessionID(ctx),
		requestID: mcp.NewRequestId(requestID).String(),
	}
}

// startRequest reserves the ID for the session. A nil finish means the ID is
// already in use. The caller that starts a request must call finish when it exits.
func (s *MCPServer) startRequest(ctx context.Context, key inflightRequestKey) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	if key.sessionID != "" {
		if _, loaded := s.inflightCancels.LoadOrStore(key, cancel); loaded {
			cancel()
			return nil, nil
		}
	} else {
		// Preserve the existing behavior for callers without a session ID.
		s.inflightCancels.Store(key, cancel)
	}
	return ctx, func() {
		s.inflightCancels.Delete(key)
		cancel()
	}
}

func (s *MCPServer) cancelRequest(ctx context.Context, id any) {
	key := inflightKey(ctx, id)
	var value any
	if key.sessionID == "" {
		// Preserve the existing behavior for callers without a session ID.
		value, _ = s.inflightCancels.LoadAndDelete(key)
	} else {
		// Keep the ID reserved until the handler finishes.
		value, _ = s.inflightCancels.Load(key)
	}
	if cancel, ok := value.(context.CancelFunc); ok {
		cancel()
	}
}

func duplicateRequestIDError() mcp.JSONRPCMessage {
	// A response with the duplicate ID could be matched to the original call.
	return createErrorResponse(nil, mcp.INVALID_REQUEST, "Request ID is already in use")
}
