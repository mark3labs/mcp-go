package server

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

type stdioQueueMessage struct {
	ID     mcp.RequestId            `json:"id"`
	Method string                   `json:"method,omitempty"`
	Result json.RawMessage          `json:"result,omitempty"`
	Error  *mcp.JSONRPCErrorDetails `json:"error,omitempty"`
}

type stdioQueueMessageWriter struct {
	messages chan stdioQueueMessage
}

func (w *stdioQueueMessageWriter) Write(p []byte) (int, error) {
	var message stdioQueueMessage
	if err := json.Unmarshal(p, &message); err != nil {
		return 0, err
	}
	if !message.ID.IsNil() {
		w.messages <- message
	}
	return len(p), nil
}

// TestStdioServerFullQueueKeepsInputReadable verifies that queue saturation
// permits client responses and EOF to release independently waiting requests.
func TestStdioServerFullQueueKeepsInputReadable(t *testing.T) {
	tests := []struct {
		name   string
		method mcp.MCPMethod
		result string
		call   func(context.Context) error
	}{
		{name: "sampling", method: mcp.MethodSamplingCreateMessage, result: `{"role":"assistant","content":{"type":"text","text":"ok"},"model":"test"}`, call: func(ctx context.Context) error {
			_, err := stdioSessionInstance.RequestSampling(ctx, mcp.CreateMessageRequest{})
			return err
		}},
		{name: "elicitation", method: mcp.MethodElicitationCreate, result: `{"action":"accept","content":{}}`, call: func(ctx context.Context) error {
			_, err := stdioSessionInstance.RequestElicitation(ctx, mcp.ElicitationRequest{})
			return err
		}},
		{name: "roots", method: mcp.MethodListRoots, result: `{"roots":[]}`, call: func(ctx context.Context) error {
			_, err := stdioSessionInstance.ListRoots(ctx, mcp.ListRootsRequest{})
			return err
		}},
	}
	modes := []struct {
		name  string
		reply bool
	}{
		{name: "EOF"},
		{name: "client response", reply: true},
	}
	for _, tt := range tests {
		for _, mode := range modes {
			t.Run(tt.name+"/"+mode.name, func(t *testing.T) {
				requestCtx, cancelRequest := context.WithCancel(t.Context())
				listenCtx, cancelListen := context.WithCancel(t.Context())
				var calls atomic.Int32
				handlerDone := make(chan error, 2)
				mcpServer := NewMCPServer("test", "1.0.0", WithToolCapabilities(true))
				mcpServer.AddTool(mcp.NewTool("ask"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					calls.Add(1)
					handlerDone <- tt.call(requestCtx)
					return mcp.NewToolResultText("done"), nil
				})
				mcpServer.AddTool(mcp.NewTool("queued"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return mcp.NewToolResultText("queued"), nil
				})
				stdioServer := NewStdioServer(mcpServer)
				stdioServer.SetErrorLogger(log.New(io.Discard, "", 0))
				WithWorkerPoolSize(1)(stdioServer)
				WithQueueSize(1)(stdioServer)
				reader, input := io.Pipe()
				output := &stdioQueueMessageWriter{messages: make(chan stdioQueueMessage, 16)}
				listenDone := make(chan struct{})
				var listenErr error
				go func() {
					listenErr = stdioServer.Listen(listenCtx, reader, output)
					close(listenDone)
				}()
				t.Cleanup(func() {
					cancelRequest()
					cancelListen()
					_ = input.Close()
					_ = reader.Close()
					select {
					case <-listenDone:
					case <-time.After(2 * time.Second):
						t.Error("Listen did not stop during cleanup")
					}
				})
				send := func(message string) {
					t.Helper()
					written := make(chan error, 1)
					go func() {
						_, err := io.WriteString(input, message+"\n")
						written <- err
					}()
					select {
					case err := <-written:
						require.NoError(t, err)
					case <-time.After(2 * time.Second):
						t.Fatal("input processing stalled")
					}
				}
				awaitMessage := func() stdioQueueMessage {
					t.Helper()
					select {
					case message := <-output.messages:
						return message
					case <-time.After(2 * time.Second):
						t.Fatal("server did not write a message")
						return stdioQueueMessage{}
					}
				}

				send(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask"}}`)
				request := awaitMessage()
				require.Equal(t, string(tt.method), request.Method)
				send(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"queued"}}`)
				send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ask"}}`)
				overflow := awaitMessage()
				if mode.reply {
					response, err := json.Marshal(mcp.JSONRPCResponse{JSONRPC: "2.0", ID: request.ID, Result: json.RawMessage(tt.result)})
					require.NoError(t, err)
					send(string(response))
					for _, id := range []int64{1, 2} {
						response := awaitMessage()
						require.Equal(t, id, response.ID.Value())
						require.Nil(t, response.Error)
						require.NotEmpty(t, response.Result)
					}
				}
				require.NoError(t, input.Close())
				select {
				case <-listenDone:
					require.NoError(t, listenErr)
				case <-time.After(2 * time.Second):
					t.Fatal("Listen blocked after EOF with a full tool queue")
				}
				handlerErr := <-handlerDone
				if mode.reply {
					require.NoError(t, handlerErr)
				} else {
					require.ErrorIs(t, handlerErr, ErrStdioServerClosed)
				}
				require.NoError(t, requestCtx.Err(), "shutdown must not depend on canceling the request")
				require.EqualValues(t, 1, calls.Load(), "overflow must not start another handler")
				require.Equal(t, int64(3), overflow.ID.Value())
				require.NotNil(t, overflow.Error)
				require.Equal(t, mcp.INTERNAL_ERROR, overflow.Error.Code)
				require.Equal(t, "Tool call queue is full", overflow.Error.Message)
			})
		}
	}
}
