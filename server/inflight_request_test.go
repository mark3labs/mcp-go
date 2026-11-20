package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestDuplicateRequestIDKeepsOriginalCancellable(t *testing.T) {
	const (
		call   = `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"slow"}}`
		second = `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"fast"}}`
		cancel = `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":7}}`
	)
	ctx, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()

	s := NewMCPServer("test", "1")
	sessionCtx := s.WithContext(ctx, NewInProcessSession("test-session", nil))
	started := make(chan context.Context, 1)
	release := make(chan struct{})
	s.AddTool(mcp.NewTool("slow"), func(requestCtx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		started <- requestCtx
		<-requestCtx.Done()
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, requestCtx.Err()
	})
	fastCalled := false
	s.AddTool(mcp.NewTool("fast"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		fastCalled = true
		return mcp.NewToolResultText("ok"), nil
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.HandleMessage(sessionCtx, []byte(call))
	}()

	var requestCtx context.Context
	select {
	case requestCtx = <-started:
	case <-ctx.Done():
		t.Fatal("original tool call did not start")
	}

	rejectDuplicate := func() {
		t.Helper()
		response := s.HandleMessage(sessionCtx, []byte(second))
		rpcErr, ok := response.(mcp.JSONRPCError)
		require.True(t, ok, "duplicate should return an error, got %v", response)
		require.Equal(t, mcp.INVALID_REQUEST, rpcErr.Error.Code)
		require.True(t, rpcErr.ID.IsNil(), "the error must not use the original request's ID")
		require.False(t, fastCalled, "the duplicate tool must not run")
	}

	rejectDuplicate()
	require.NoError(t, requestCtx.Err())
	require.Nil(t, s.HandleMessage(sessionCtx, []byte(cancel)))
	require.ErrorIs(t, requestCtx.Err(), context.Canceled)
	rejectDuplicate() // Cancellation signals the handler, but it has not exited yet.

	close(release)
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("cancelled tool call did not finish")
	}
	_, tracked := s.inflightCancels.Load(inflightKey(sessionCtx, int64(7)))
	require.False(t, tracked, "finished requests must release their registration")
}

func TestRequestIDAdmissionIsScopedToSession(t *testing.T) {
	tests := []struct {
		name            string
		firstSessionID  string
		secondSessionID string
	}{
		{name: "distinct sessions", firstSessionID: "first", secondSessionID: "second"},
		{name: "no session IDs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			s := NewMCPServer("test", "1")
			first := s.WithContext(ctx, NewInProcessSession(tt.firstSessionID, nil))
			second := s.WithContext(ctx, NewInProcessSession(tt.secondSessionID, nil))
			started := make(chan struct{})
			s.AddTool(mcp.NewTool("slow"), func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				close(started)
				<-ctx.Done()
				return nil, ctx.Err()
			})

			go s.HandleMessage(first, []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"slow"}}`))
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("first tool call did not start")
			}
			response := s.HandleMessage(second, []byte(`{"jsonrpc":"2.0","id":7,"method":"ping"}`))
			require.IsType(t, mcp.JSONRPCResponse{}, response)
		})
	}
}
