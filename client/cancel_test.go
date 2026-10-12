package client

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connectOverPipes connects a client to s over in-memory stdio pipes, on the
// given protocol version.
func connectOverPipes(t *testing.T, s *server.MCPServer, version string) *Client {
	t.Helper()
	ctx := t.Context()
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()
	t.Cleanup(func() {
		_ = serverWrite.Close()
		_ = serverRead.Close()
	})
	go func() { _ = server.NewStdioServer(s).Listen(ctx, serverRead, serverWrite) }()

	c := NewClient(transport.NewIO(clientRead, clientWrite, io.NopCloser(strings.NewReader(""))))
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Start(ctx))
	result, err := c.Initialize(ctx, mcp.InitializeRequest{Params: mcp.InitializeParams{
		ProtocolVersion: version,
		ClientInfo:      mcp.Implementation{Name: "test-client", Version: "1.0.0"},
	}})
	require.NoError(t, err)
	require.Equal(t, version, result.ProtocolVersion)
	return c
}

// A request whose context ends is cancelled on the server too. Over stdio
// that takes a notifications/cancelled, which protocol version 2026-07-28
// requires there; without it the server kept working on a request nobody
// was waiting for.
func TestClient_CancelsAbandonedRequestOverStdio(t *testing.T) {
	for _, version := range []string{mcp.ProtocolVersion20260728, mcp.LATEST_LEGACY_PROTOCOL_VERSION} {
		t.Run(version, func(t *testing.T) {
			stopped := make(chan struct{})
			s := server.NewMCPServer("test-server", "1.0.0")
			s.AddTool(mcp.NewTool("slow"), func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				select {
				case <-ctx.Done():
					close(stopped)
				case <-time.After(10 * time.Second):
				}
				return mcp.NewToolResultText("done"), nil
			})
			c := connectOverPipes(t, s, version)

			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			_, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "slow"}})
			require.ErrorIs(t, err, context.DeadlineExceeded)

			select {
			case <-stopped:
			case <-time.After(5 * time.Second):
				t.Fatal("the server kept running the tool after the client gave up on it")
			}
		})
	}
}

// pipePeer plays the server end of a stdio connection by hand.
type pipePeer struct {
	t        *testing.T
	client   *Client
	write    *io.PipeWriter
	messages chan map[string]any
}

func newPipePeer(t *testing.T) *pipePeer {
	t.Helper()
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()
	t.Cleanup(func() {
		_ = serverWrite.Close()
		_ = serverRead.Close()
	})
	p := &pipePeer{t: t, write: serverWrite, messages: make(chan map[string]any, 10)}
	p.client = NewClient(transport.NewIO(clientRead, clientWrite, io.NopCloser(strings.NewReader(""))), WithLegacyProtocolOnly())
	t.Cleanup(func() { _ = p.client.Close() })
	require.NoError(t, p.client.Start(t.Context()))
	go func() {
		scanner := bufio.NewScanner(serverRead)
		for scanner.Scan() {
			var message map[string]any
			if json.Unmarshal(scanner.Bytes(), &message) == nil {
				p.messages <- message
			}
		}
	}()
	return p
}

// next returns the next message the client sent.
func (p *pipePeer) next() map[string]any {
	p.t.Helper()
	select {
	case m := <-p.messages:
		return m
	case <-time.After(5 * time.Second):
		p.t.Fatal("no message from the client")
		return nil
	}
}

func (p *pipePeer) respond(id any, result string) {
	p.t.Helper()
	response, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": json.RawMessage(result)})
	require.NoError(p.t, err)
	_, err = p.write.Write(append(response, '\n'))
	require.NoError(p.t, err)
}

// initialize completes the handshake with the client.
func (p *pipePeer) initialize() {
	p.t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := p.client.Initialize(p.t.Context(), mcp.InitializeRequest{})
		done <- err
	}()
	initialize := p.next()
	require.Equal(p.t, "initialize", initialize["method"])
	p.respond(initialize["id"], `{"protocolVersion":"`+mcp.LATEST_LEGACY_PROTOCOL_VERSION+`","capabilities":{},"serverInfo":{"name":"s","version":"1"}}`)
	require.NoError(p.t, <-done)
	require.Equal(p.t, "notifications/initialized", p.next()["method"])
}

// expectNothingElse checks that the next message is a ping the test sends
// after waiting a little, so that nothing else was sent in the meantime.
func (p *pipePeer) expectNothingElse() {
	p.t.Helper()
	time.Sleep(100 * time.Millisecond)
	go func() { _ = p.client.Ping(p.t.Context()) }()
	assert.Equal(p.t, "ping", p.next()["method"])
}

// The cancellation names the request. Initialize, a request that was never
// sent, and a task-augmented request are not cancelled.
func TestClient_CancellationNotificationOnTheWire(t *testing.T) {
	p := newPipePeer(t)

	// An initialize that times out is not cancelled.
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := p.client.Initialize(ctx, mcp.InitializeRequest{})
	require.Error(t, err)
	assert.Equal(t, "initialize", p.next()["method"])
	p.initialize()

	// A tool call that times out is cancelled by id, with the reason.
	ctx, cancel = context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = p.client.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "slow"}})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	call := p.next()
	require.Equal(t, "tools/call", call["method"])
	cancelled := p.next()
	assert.Equal(t, "notifications/cancelled", cancelled["method"])
	params, _ := cancelled["params"].(map[string]any)
	assert.Equal(t, call["id"], params["requestId"])
	assert.Equal(t, context.DeadlineExceeded.Error(), params["reason"])

	// A call whose context had ended before it was sent isn't sent or
	// cancelled.
	done, stop := context.WithCancel(t.Context())
	stop()
	_, err = p.client.CallTool(done, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "slow"}})
	require.ErrorIs(t, err, context.Canceled)
	p.expectNothingElse()

	// A task-augmented call is cancelled with tasks/cancel, not this.
	ctx, cancel = context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = p.client.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "slow", Task: &mcp.TaskParams{}}})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, "tools/call", p.next()["method"])
	p.expectNothingElse()
}

// A peer that stops reading doesn't hold up a caller whose context ended:
// the cancellation is sent in the background.
func TestClient_CancellationDoesNotBlockTheCaller(t *testing.T) {
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()
	t.Cleanup(func() {
		_ = serverWrite.Close()
		_ = serverRead.Close()
	})
	c := NewClient(transport.NewIO(clientRead, clientWrite, io.NopCloser(strings.NewReader(""))), WithLegacyProtocolOnly())
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Start(t.Context()))
	reader := bufio.NewReader(serverRead)
	read := func() map[string]any {
		line, err := reader.ReadBytes('\n')
		require.NoError(t, err)
		var message map[string]any
		require.NoError(t, json.Unmarshal(line, &message))
		return message
	}

	done := make(chan error, 1)
	go func() {
		_, err := c.Initialize(t.Context(), mcp.InitializeRequest{})
		done <- err
	}()
	initialize := read()
	response, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": initialize["id"], "result": json.RawMessage(
		`{"protocolVersion":"` + mcp.LATEST_LEGACY_PROTOCOL_VERSION + `","capabilities":{},"serverInfo":{"name":"s","version":"1"}}`)})
	require.NoError(t, err)
	_, err = serverWrite.Write(append(response, '\n'))
	require.NoError(t, err)
	require.Equal(t, "notifications/initialized", read()["method"])
	require.NoError(t, <-done)

	// Read the tool call, then nothing more.
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	returned := make(chan error, 1)
	go func() {
		_, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "slow"}})
		returned <- err
	}()
	require.Equal(t, "tools/call", read()["method"])
	select {
	case err := <-returned:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(5 * time.Second):
		t.Fatal("the call didn't return while the cancellation couldn't be written")
	}
}

// Over Streamable HTTP on protocol version 2026-07-28 the transport ends the
// request's stream, which is the cancellation signal there, so no
// notifications/cancelled is sent.
func TestClient_NoCancellationNotificationOverHTTP(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&message)
		mu.Lock()
		methods = append(methods, message.Method)
		mu.Unlock()
		switch message.Method {
		case string(mcp.MethodServerDiscover):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(message.ID) + `,"result":{"supportedVersions":["` + mcp.ProtocolVersion20260728 + `"],"capabilities":{},"serverInfo":{"name":"s","version":"1"}}}`))
		case string(mcp.MethodToolsCall):
			<-r.Context().Done()
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer httpServer.Close()

	trans, err := transport.NewStreamableHTTP(httpServer.URL)
	require.NoError(t, err)
	c := NewClient(trans)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Start(t.Context()))
	result, err := c.Initialize(t.Context(), mcp.InitializeRequest{})
	require.NoError(t, err)
	require.Equal(t, mcp.ProtocolVersion20260728, result.ProtocolVersion)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "slow"}})
	require.Error(t, err)

	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	assert.NotContains(t, methods, string(mcp.MethodNotificationCancelled))
}
