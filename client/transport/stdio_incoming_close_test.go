package transport

import (
	"context"
	"encoding/json"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStdio_IncomingRequestCanceledOnTransportEnd(t *testing.T) {
	tests := []struct {
		name         string
		endTransport func(t *testing.T, stdio *Stdio, stdoutWriter *io.PipeWriter)
	}{
		{
			name: "Close cancels in-flight incoming request",
			endTransport: func(t *testing.T, stdio *Stdio, _ *io.PipeWriter) {
				require.NoError(t, stdio.Close())
			},
		},
		{
			name: "server stdout EOF cancels in-flight incoming request",
			endTransport: func(t *testing.T, _ *Stdio, stdoutWriter *io.PipeWriter) {
				require.NoError(t, stdoutWriter.Close())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdoutReader, stdoutWriter := io.Pipe()
			stdin := &concurrentBuffer{}
			stdio := NewIO(stdoutReader, stdin, nil)
			t.Cleanup(func() {
				_ = stdoutWriter.Close()
				_ = stdio.Close()
			})

			started := make(chan struct{})
			handlerErr := make(chan error, 1)
			stdio.SetRequestHandler(func(ctx context.Context, _ JSONRPCRequest) (*JSONRPCResponse, error) {
				close(started)
				<-ctx.Done()
				handlerErr <- ctx.Err()
				return nil, ctx.Err()
			})

			require.NoError(t, stdio.Start(t.Context()))

			reqBytes, err := json.Marshal(JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      mcp.NewRequestId(int64(1)),
				Method:  "sampling/createMessage",
			})
			require.NoError(t, err)
			_, err = stdoutWriter.Write(append(reqBytes, '\n'))
			require.NoError(t, err)

			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("incoming request handler did not start")
			}

			tt.endTransport(t, stdio, stdoutWriter)

			select {
			case err := <-handlerErr:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(2 * time.Second):
				t.Fatal("incoming request handler was not canceled when transport ended")
			}

			assert.Never(t, func() bool {
				return stdin.String() != ""
			}, 50*time.Millisecond, 5*time.Millisecond)
		})
	}
}

func TestStdio_IncomingRequestSkippedAfterClose(t *testing.T) {
	tests := []struct {
		name        string
		withHandler bool
	}{
		{name: "with handler", withHandler: true},
		{name: "without handler", withHandler: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdio := newBareStdio()
			stdio.ctx = t.Context()
			sink := &concurrentBuffer{}
			stdio.stdin = sink

			var called atomic.Bool
			if tt.withHandler {
				stdio.SetRequestHandler(func(_ context.Context, request JSONRPCRequest) (*JSONRPCResponse, error) {
					called.Store(true)
					return NewJSONRPCResultResponse(request.ID, json.RawMessage(`"ok"`)), nil
				})
			}

			stdio.closeDone()

			stdio.handleIncomingRequest(JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      mcp.NewRequestId(int64(1)),
				Method:  "sampling/createMessage",
			})

			assert.Never(t, func() bool {
				return called.Load() || sink.String() != ""
			}, 50*time.Millisecond, 5*time.Millisecond)
		})
	}
}
