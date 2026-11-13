package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestDuplicateInflightRequest(t *testing.T) {
	for _, id := range []string{"7", `"request"`, "9007199254740993"} {
		t.Run(id, func(t *testing.T) {
			s := NewMCPServer("test", "1.0")
			started := make(chan context.Context, 1)
			release := make(chan struct{})
			var releaseOnce sync.Once
			finish := func() { releaseOnce.Do(func() { close(release) }) }
			s.AddTool(mcp.NewTool("slow"), func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				started <- ctx
				<-release
				return mcp.NewToolResultText("done"), nil
			})
			ctx, cancel := context.WithCancel(t.Context())
			ctx = s.WithContext(ctx, NewInProcessSession("first", nil))
			var workers sync.WaitGroup
			defer func() { cancel(); finish(); workers.Wait() }()
			workers.Go(func() {
				s.HandleMessage(ctx, json.RawMessage(`{"jsonrpc":"2.0","id":`+id+`,"method":"tools/call","params":{"name":"slow"}}`))
			})
			var original context.Context
			select {
			case original = <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("original request did not start")
			}
			ping := json.RawMessage(`{"jsonrpc":"2.0","id":` + id + `,"method":"ping"}`)
			assertDuplicate := func() {
				t.Helper()
				response, ok := s.HandleMessage(ctx, ping).(mcp.JSONRPCError)
				require.True(t, ok, "duplicate request must be rejected")
				require.Equal(t, mcp.INVALID_REQUEST, response.Error.Code)
			}
			assertDuplicate()
			other := s.WithContext(ctx, NewInProcessSession("second", nil))
			_, rejected := s.HandleMessage(other, ping).(mcp.JSONRPCError)
			require.False(t, rejected, "request IDs are scoped to the session")
			s.HandleMessage(ctx, json.RawMessage(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":`+id+`}}`))
			require.ErrorIs(t, original.Err(), context.Canceled)
			assertDuplicate()
			finish()
			workers.Wait()
			_, rejected = s.HandleMessage(ctx, ping).(mcp.JSONRPCError)
			require.False(t, rejected, "completed requests must release their registration")
		})
	}
}

func TestStdioDuplicateQueuedRequest(t *testing.T) {
	s := NewMCPServer("test", "1.0")
	called := false
	s.AddTool(mcp.NewTool("queued"), func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		called = true
		require.ErrorIs(t, ctx.Err(), context.Canceled)
		return mcp.NewToolResultText("done"), nil
	})
	stdio := NewStdioServer(s)
	stdio.SetErrorLogger(log.New(io.Discard, "", 0))
	stdio.toolCallQueue = make(chan *toolCallWork, 2)
	ctx := s.WithContext(t.Context(), NewInProcessSession("stdio-test", nil))
	var output bytes.Buffer
	request := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"queued"}}`
	require.NoError(t, stdio.processMessage(ctx, request, &output))
	work := <-stdio.toolCallQueue
	defer work.cancel()
	require.NoError(t, stdio.processMessage(ctx, request, &output))
	var response mcp.JSONRPCError
	require.NoError(t, json.Unmarshal(output.Bytes(), &response), "duplicate must be rejected before queuing")
	require.Equal(t, mcp.INVALID_REQUEST, response.Error.Code)
	require.Empty(t, stdio.toolCallQueue)
	output.Reset()
	require.NoError(t, stdio.processMessage(ctx, `{"jsonrpc":"2.0","id":7,"method":"ping"}`, &output))
	require.NoError(t, json.Unmarshal(output.Bytes(), &response))
	require.Equal(t, mcp.INVALID_REQUEST, response.Error.Code)
	require.NoError(t, stdio.processMessage(ctx, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":7}}`, &output))
	s.HandleMessage(work.ctx, work.message)
	require.True(t, called)
}
