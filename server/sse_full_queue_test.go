package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// floodTool sends notifications until the calling session's event queue is
// full, which it gets once nothing drains the stream, and then ends the call
// with finish. It reports on filled whether the queue filled up, and passes
// on the session.
func floodTool(finish func() (*mcp.CallToolResult, error), filled chan<- bool, sessions chan<- *sseSession) ToolHandlerFunc {
	return func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		srv := ServerFromContext(ctx)
		session := ClientSessionFromContext(ctx).(*sseSession)
		queue := session.eventQueue
		payload := strings.Repeat("x", 64<<10)
		deadline := time.Now().Add(10 * time.Second)
		for len(queue) < cap(queue) && time.Now().Before(deadline) {
			_ = srv.SendNotificationToClient(ctx, "notifications/message", map[string]any{"level": "info", "data": payload})
			time.Sleep(time.Millisecond)
		}
		filled <- len(queue) == cap(queue)
		sessions <- session
		return finish()
	}
}

// sseTestClient reads an SSE stream and posts messages to its message
// endpoint.
type sseTestClient struct {
	t          *testing.T
	events     *bufio.Reader
	messageURL string
}

func newSSETestClient(t *testing.T, url string) *sseTestClient {
	t.Helper()
	resp, err := http.Get(url)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })
	c := &sseTestClient{t: t, events: bufio.NewReader(resp.Body)}
	c.messageURL, err = c.nextData()
	require.NoError(t, err)
	return c
}

// nextData returns the data of the next SSE event.
func (c *sseTestClient) nextData() (string, error) {
	var data string
	for {
		line, err := c.events.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" && data != "" {
			return data, nil
		}
		if value, ok := strings.CutPrefix(line, "data: "); ok {
			data = value
		}
	}
}

func (c *sseTestClient) post(message string) {
	c.t.Helper()
	resp, err := http.Post(c.messageURL, "application/json", bytes.NewBufferString(message))
	require.NoError(c.t, err)
	resp.Body.Close()
	require.Equal(c.t, http.StatusAccepted, resp.StatusCode)
}

// initialize runs the handshake and waits until mcpServer has the session
// initialized, since messages are handled concurrently.
func (c *sseTestClient) initialize(mcpServer *MCPServer) {
	c.t.Helper()
	c.post(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`)
	_, err := c.nextData()
	require.NoError(c.t, err)
	c.post(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	require.Eventually(c.t, func() bool {
		initialized := false
		mcpServer.sessions.Range(func(_, value any) bool {
			if session, ok := value.(ClientSession); ok && session.Initialized() {
				initialized = true
			}
			return true
		})
		return initialized
	}, 5*time.Second, time.Millisecond)
}

// The response to a call is delivered even when the session's event queue is
// full at the time, for example because the tool sent more notifications than
// a slow client has read yet. It used to be dropped, and the call waited
// forever. The same goes for the error sent after a handler panics.
func TestSSEServer_DeliversResponseDespiteFullQueue(t *testing.T) {
	tests := []struct {
		name   string
		finish func() (*mcp.CallToolResult, error)
		// isResponse reports whether a message is the call's response.
		isResponse func(message map[string]json.RawMessage) bool
	}{
		{
			name:   "result",
			finish: func() (*mcp.CallToolResult, error) { return mcp.NewToolResultText("done"), nil },
			isResponse: func(message map[string]json.RawMessage) bool {
				return string(message["id"]) == "2" && message["result"] != nil
			},
		},
		{
			name:   "panic",
			finish: func() (*mcp.CallToolResult, error) { panic("flood") },
			isResponse: func(message map[string]json.RawMessage) bool {
				return strings.Contains(string(message["error"]), "internal panic: flood")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filled := make(chan bool, 1)
			sessions := make(chan *sseSession, 1)
			mcpServer := NewMCPServer("test", "1.0.0")
			mcpServer.AddTool(mcp.NewTool("flood"), floodTool(tt.finish, filled, sessions))
			testServer := NewTestServer(mcpServer)
			t.Cleanup(testServer.Close)

			client := newSSETestClient(t, testServer.URL+"/sse")
			client.initialize(mcpServer)

			// Call the tool, and read nothing while it floods the stream.
			client.post(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"flood"}}`)
			select {
			case ok := <-filled:
				require.True(t, ok, "the event queue never filled up")
			case <-time.After(15 * time.Second):
				t.Fatal("the tool was never called")
			}
			// Give the response time to meet the full queue.
			time.Sleep(100 * time.Millisecond)

			found := make(chan error, 1)
			go func() {
				for {
					data, err := client.nextData()
					if err != nil {
						found <- err
						return
					}
					var message map[string]json.RawMessage
					if json.Unmarshal([]byte(data), &message) == nil && tt.isResponse(message) {
						found <- nil
						return
					}
				}
			}()
			select {
			case err := <-found:
				require.NoError(t, err)
			case <-time.After(10 * time.Second):
				t.Fatal("the tool call's response never arrived")
			}
		})
	}
}

// stallingWriter is a ResponseWriter whose writes, once stall is set, block
// until release is closed and then panic.
type stallingWriter struct {
	http.ResponseWriter
	stall   atomic.Bool
	release chan struct{}
}

func (w *stallingWriter) Write(p []byte) (int, error) {
	if w.stall.Load() {
		<-w.release
		panic(http.ErrAbortHandler)
	}
	return w.ResponseWriter.Write(p)
}

func (w *stallingWriter) Flush() {
	w.ResponseWriter.(http.Flusher).Flush()
}

// A stream that ends with a panic, here from its ResponseWriter, closes the
// session's done channel, which releases the responses waiting for room in
// its queue.
func TestSSEServer_PanickingStreamReleasesWaitingResponses(t *testing.T) {
	filled := make(chan bool, 1)
	sessions := make(chan *sseSession, 1)
	mcpServer := NewMCPServer("test", "1.0.0")
	mcpServer.AddTool(mcp.NewTool("flood"), floodTool(func() (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("done"), nil
	}, filled, sessions))

	writer := &stallingWriter{release: make(chan struct{})}
	var once sync.Once
	sseServer := NewSSEServer(mcpServer)
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sse" {
			once.Do(func() { writer.ResponseWriter = w })
			w = writer
		}
		sseServer.ServeHTTP(w, r)
	}))
	t.Cleanup(testServer.Close)
	WithBaseURL(testServer.URL)(sseServer)
	t.Cleanup(func() { close(writer.release) })

	client := newSSETestClient(t, testServer.URL+"/sse")
	client.initialize(mcpServer)

	// Stall the stream, so the tool fills the queue, and its response waits.
	writer.stall.Store(true)
	client.post(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"flood"}}`)
	var session *sseSession
	select {
	case ok := <-filled:
		require.True(t, ok, "the event queue never filled up")
		session = <-sessions
	case <-time.After(15 * time.Second):
		t.Fatal("the tool was never called")
	}

	// End the stream with a panic.
	select {
	case writer.release <- struct{}{}:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream never stalled")
	}
	select {
	case <-session.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the session's done channel is still open after the stream ended")
	}
}
