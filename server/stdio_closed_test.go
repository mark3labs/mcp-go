package server

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestStdioServerListenRejectsCanceledContext(t *testing.T) {
	var registered atomic.Int32
	hooks := &Hooks{}
	hooks.AddOnRegisterSession(func(context.Context, ClientSession) {
		registered.Add(1)
	})
	stdioServer := NewStdioServer(NewMCPServer("test", "1.0.0", WithHooks(hooks)))
	stdioServer.SetErrorLogger(log.New(io.Discard, "", 0))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := stdioServer.Listen(ctx, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`), io.Discard)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, registered.Load(), "a canceled Listen must not register a session")
}

type stdioRequestSignalWriter struct {
	once    sync.Once
	written chan struct{}
}

func (w *stdioRequestSignalWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.written) })
	return len(p), nil
}

func TestStdioServerPendingRequestsReturnClosed(t *testing.T) {
	tests := []struct {
		name string
		call func(context.Context, *stdioSession) error
	}{
		{name: "sampling", call: func(ctx context.Context, session *stdioSession) error {
			_, err := session.RequestSampling(ctx, mcp.CreateMessageRequest{})
			return err
		}},
		{name: "elicitation", call: func(ctx context.Context, session *stdioSession) error {
			_, err := session.RequestElicitation(ctx, mcp.ElicitationRequest{})
			return err
		}},
		{name: "roots", call: func(ctx context.Context, session *stdioSession) error {
			_, err := session.ListRoots(ctx, mcp.ListRootsRequest{})
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdioServer := NewStdioServer(NewMCPServer("test", "1.0.0"))
			for range 2 {
				reader, input := io.Pipe()
				t.Cleanup(func() { _ = reader.Close(); _ = input.Close() })
				ready := make(chan *stdioSession, 1)
				stdioServer.SetContextFunc(func(ctx context.Context) context.Context {
					ready <- ClientSessionFromContext(ctx).(*stdioSession)
					return ctx
				})
				output := &stdioRequestSignalWriter{written: make(chan struct{})}
				listenDone := make(chan error, 1)
				go func() { listenDone <- stdioServer.Listen(t.Context(), reader, output) }()
				var session *stdioSession
				select {
				case session = <-ready:
				case <-time.After(2 * time.Second):
					t.Fatal("Listen did not install a writer")
				}

				ctx, cancel := context.WithCancel(t.Context())
				t.Cleanup(cancel)
				requestDone := make(chan error, 1)
				go func() { requestDone <- tt.call(ctx, session) }()
				select {
				case <-output.written:
				case <-time.After(2 * time.Second):
					t.Fatal("request was not sent during Listen")
				}
				require.NoError(t, input.Close())
				select {
				case err := <-listenDone:
					require.NoError(t, err)
				case <-time.After(2 * time.Second):
					t.Fatal("Listen did not return after EOF")
				}
				select {
				case err := <-requestDone:
					require.ErrorIs(t, err, ErrStdioServerClosed)
				case <-time.After(2 * time.Second):
					cancel()
					<-requestDone
					t.Fatal("request remained blocked after Listen returned")
				}
				require.NoError(t, ctx.Err(), "closure must not depend on request cancellation")
				require.ErrorIs(t, tt.call(ctx, session), ErrStdioServerClosed)
				cancel()
			}
		})
	}
}

func TestStdioServerShutdownReleasesHandlerRequests(t *testing.T) {
	tests := []struct {
		name string
		call func(context.Context, *stdioSession) error
	}{
		{name: "sampling", call: func(ctx context.Context, session *stdioSession) error {
			_, err := session.RequestSampling(ctx, mcp.CreateMessageRequest{})
			return err
		}},
		{name: "elicitation", call: func(ctx context.Context, session *stdioSession) error {
			_, err := session.RequestElicitation(ctx, mcp.ElicitationRequest{})
			return err
		}},
		{name: "roots", call: func(ctx context.Context, session *stdioSession) error {
			_, err := session.ListRoots(ctx, mcp.ListRootsRequest{})
			return err
		}},
	}
	contexts := []struct {
		name        string
		independent bool
	}{
		{name: "handler context"},
		{name: "independent context", independent: true},
	}
	shutdowns := []struct {
		name   string
		err    error
		cancel bool
	}{
		{name: "EOF"},
		{name: "read error", err: errors.New("input failed")},
		{name: "parent cancellation", err: context.Canceled, cancel: true},
	}
	for _, tt := range tests {
		for _, requestContext := range contexts {
			for _, shutdown := range shutdowns {
				t.Run(tt.name+"/"+requestContext.name+"/"+shutdown.name, func(t *testing.T) {
					independentCtx, cancel := context.WithCancel(t.Context())
					listenCtx, cancelListen := context.WithCancel(t.Context())
					handlerDone := make(chan error, 1)
					mcpServer := NewMCPServer("test", "1.0.0", WithToolCapabilities(true))
					mcpServer.AddTool(mcp.NewTool("ask"), func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						session := ClientSessionFromContext(ctx).(*stdioSession)
						if requestContext.independent {
							ctx = independentCtx
						}
						handlerDone <- tt.call(ctx, session)
						return mcp.NewToolResultText("done"), nil
					})
					stdioServer := NewStdioServer(mcpServer)
					stdioServer.SetErrorLogger(log.New(io.Discard, "", 0))
					WithWorkerPoolSize(1)(stdioServer)

					reader, input := io.Pipe()
					output := &stdioRequestSignalWriter{written: make(chan struct{})}
					listenDone := make(chan struct{})
					var listenErr error
					go func() {
						listenErr = stdioServer.Listen(listenCtx, reader, output)
						close(listenDone)
					}()
					t.Cleanup(func() {
						cancel()
						cancelListen()
						_ = input.Close()
						_ = reader.Close()
						select {
						case <-listenDone:
						case <-time.After(2 * time.Second):
							t.Error("Listen did not stop during cleanup")
						}
					})

					_, err := io.WriteString(input, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask"}}`+"\n")
					require.NoError(t, err)
					select {
					case <-output.written:
					case <-time.After(2 * time.Second):
						t.Fatal("handler did not send its request")
					}
					if shutdown.cancel {
						cancelListen()
					} else {
						require.NoError(t, input.CloseWithError(shutdown.err))
					}
					select {
					case <-listenDone:
						if shutdown.err == nil {
							require.NoError(t, listenErr)
						} else {
							require.ErrorIs(t, listenErr, shutdown.err)
						}
					case <-time.After(2 * time.Second):
						t.Fatal("Listen blocked waiting for the handler after input ended")
					}
					handlerErr := <-handlerDone
					if shutdown.cancel && !requestContext.independent {
						require.True(t, errors.Is(handlerErr, context.Canceled) || errors.Is(handlerErr, ErrStdioServerClosed), "unexpected handler error: %v", handlerErr)
					} else {
						require.ErrorIs(t, handlerErr, ErrStdioServerClosed)
					}
					require.NoError(t, independentCtx.Err(), "shutdown must not depend on independent context cancellation")
				})
			}
		}
	}
}

func TestStdioSessionPendingRequestsReturnContextErrorBeforeClosure(t *testing.T) {
	tests := []struct {
		name string
		call func(context.Context, *stdioSession) error
	}{
		{name: "sampling", call: func(ctx context.Context, session *stdioSession) error {
			_, err := session.RequestSampling(ctx, mcp.CreateMessageRequest{})
			return err
		}},
		{name: "elicitation", call: func(ctx context.Context, session *stdioSession) error {
			_, err := session.RequestElicitation(ctx, mcp.ElicitationRequest{})
			return err
		}},
		{name: "roots", call: func(ctx context.Context, session *stdioSession) error {
			_, err := session.ListRoots(ctx, mcp.ListRootsRequest{})
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := newStdioSession()
			output := &stdioRequestSignalWriter{written: make(chan struct{})}
			gen := session.SetWriter(output)
			t.Cleanup(func() { session.markStopped(gen) })
			parentCtx, cancelParent := context.WithCancel(t.Context())
			t.Cleanup(cancelParent)
			ctx, cancel := context.WithCancel(parentCtx)
			defer cancel()
			requestDone := make(chan error, 1)
			go func() { requestDone <- tt.call(ctx, session) }()
			select {
			case <-output.written:
			case <-time.After(2 * time.Second):
				t.Fatal("request was not sent")
			}
			cancelParent()
			select {
			case err := <-requestDone:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(2 * time.Second):
				t.Fatal("pending request did not return after parent cancellation")
			}
			_, done, err := session.outboundWriter()
			require.NoError(t, err)
			select {
			case <-done:
				t.Fatal("caller cancellation closed the writer")
			default:
			}
		})
	}
}

func TestStdioSessionWriterGeneration(t *testing.T) {
	tests := []struct {
		name    string
		stopOld bool
	}{
		{name: "replace active writer"},
		{name: "replace stopped writer", stopOld: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := &stdioSession{}
			oldGen := session.SetWriter(io.Discard)
			_, oldDone, err := session.outboundWriter()
			require.NoError(t, err)
			if tt.stopOld {
				session.markStopped(oldGen)
			}

			newGen := session.SetWriter(io.Discard)
			select {
			case <-oldDone:
			default:
				t.Fatal("old writer's closure signal is still open")
			}
			session.markStopped(oldGen)
			writer, newDone, err := session.outboundWriter()
			require.NoError(t, err)
			require.NotNil(t, writer)
			select {
			case <-newDone:
				t.Fatal("old Listen closed the new writer")
			default:
			}

			session.markStopped(newGen)
			session.markStopped(newGen)
			select {
			case <-newDone:
			default:
				t.Fatal("stopped writer's closure signal is still open")
			}
			_, _, err = session.outboundWriter()
			require.ErrorIs(t, err, ErrStdioServerClosed)
		})
	}
}

func TestStdioServerListenRejectsConcurrentCall(t *testing.T) {
	hooks := &Hooks{}
	entered := make(chan struct{})
	hooks.AddOnRegisterSession(func(context.Context, ClientSession) {
		close(entered)
	})
	stdioServer := NewStdioServer(NewMCPServer("test", "1.0.0", WithHooks(hooks)))
	stdioServer.SetErrorLogger(log.New(io.Discard, "", 0))

	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = writer.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- stdioServer.Listen(ctx, reader, io.Discard)
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("Listen did not register a session")
	}

	err := stdioServer.Listen(t.Context(), strings.NewReader(""), io.Discard)
	require.ErrorIs(t, err, ErrStdioServerListening)

	cancel()
	require.NoError(t, writer.Close())
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first Listen did not return")
	}
}

func TestStdioServerSamplingAfterListenReturnsClosed(t *testing.T) {
	stdioServer := NewStdioServer(NewMCPServer("test", "1.0.0"))
	stdioServer.SetErrorLogger(log.New(io.Discard, "", 0))
	var sessions []*stdioSession
	stdioServer.SetContextFunc(func(ctx context.Context) context.Context {
		sessions = append(sessions, ClientSessionFromContext(ctx).(*stdioSession))
		return ctx
	})
	require.NoError(t, stdioServer.Listen(t.Context(), strings.NewReader(""), io.Discard))
	require.Len(t, sessions, 1)

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err := sessions[0].RequestSampling(ctx, mcp.CreateMessageRequest{})
	require.ErrorIs(t, err, ErrStdioServerClosed)

	// Listen can run again after it returns; that run ends closed as well.
	require.NoError(t, stdioServer.Listen(t.Context(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`), io.Discard))
	require.Len(t, sessions, 2)
	_, err = sessions[1].RequestSampling(ctx, mcp.CreateMessageRequest{})
	require.ErrorIs(t, err, ErrStdioServerClosed)
}
