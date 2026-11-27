package transport

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestSSELineReader(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		partial string
	}{
		{name: "empty"},
		{name: "LF", input: "one\ntwo\n\n", want: []string{"one", "two", ""}},
		{name: "CRLF", input: "one\r\ntwo\r\n\r\n", want: []string{"one", "two", ""}},
		{name: "CR", input: "one\rtwo\r\r", want: []string{"one", "two", ""}},
		{name: "mixed", input: "one\rtwo\r\nthree\n\r", want: []string{"one", "two", "three", ""}},
		{name: "consecutive CR", input: "\r\r\r\n", want: []string{"", "", ""}},
		{name: "significant spaces", input: "data:  one \r\n", want: []string{"data:  one "}},
		{name: "unterminated", input: "one", partial: "one"},
		{name: "partial after CRLF", input: "one\r\npartial", want: []string{"one"}, partial: "partial"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, fragmented := range []bool{false, true} {
				var reader io.Reader = strings.NewReader(tt.input)
				if fragmented {
					reader = iotest.OneByteReader(reader)
				}
				lines := newSSELineReader(reader)
				for _, want := range tt.want {
					got, err := lines.readLine(0)
					require.NoError(t, err)
					require.Equal(t, want, got)
				}
				got, err := lines.readLine(0)
				require.ErrorIs(t, err, io.EOF)
				require.Equal(t, tt.partial, got)
			}
		})
	}
}

func TestSSELineReader_DelayedLF(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	reader, writer := io.Pipe()
	lines := newSSELineReader(reader)
	results := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			line, err := lines.readLine(0)
			if err != nil {
				return
			}
			select {
			case results <- line:
			case <-ctx.Done():
				return
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		_ = writer.Close()
		_ = reader.Close()
		<-done
	})
	for _, step := range []struct{ input, want string }{
		{input: "data: first\r", want: "data: first"},
		{input: "\ndata: second\r", want: "data: second"},
		{input: "\r", want: ""},
		{input: "\ndata: third\n", want: "data: third"},
	} {
		_, err := io.WriteString(writer, step.input)
		require.NoError(t, err)
		select {
		case got := <-results:
			require.Equal(t, step.want, got)
		case <-time.After(time.Second):
			t.Fatal("completed line blocked waiting for another byte")
		}
	}
}

func TestSSE_LineEndingsAtEOF(t *testing.T) {
	const data = `{"jsonrpc":"2.0","method":"notifications/test"}`
	for _, tt := range []struct {
		name string
		body string
		want int
	}{
		{name: "terminated LF", body: "data: " + data + "\n", want: 1},
		{name: "terminated CRLF", body: "data: " + data + "\r\n", want: 1},
		{name: "terminated CR", body: "data: " + data + "\r", want: 1},
		{name: "unterminated", body: "data: " + data},
		{name: "ignore partial final line", body: "data: " + data + "\ndata: incomplete", want: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sse := newBareSSE()
			var notifications int
			sse.SetNotificationHandler(func(n mcp.JSONRPCNotification) {
				require.Equal(t, "notifications/test", n.Method)
				notifications++
			})
			sse.readSSE(io.NopCloser(strings.NewReader(tt.body)), make(chan struct{}), &sync.Once{})
			require.Equal(t, tt.want, notifications)

			transport, err := NewStreamableHTTP("http://127.0.0.1:1/")
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var events int
			transport.readSSE(ctx, io.NopCloser(strings.NewReader(tt.body)), func(event, got string) {
				require.Equal(t, "message", event)
				require.Equal(t, data, got)
				events++
			})
			require.Equal(t, tt.want, events)
		})
	}
}

func TestSSE_LineEndingsWhileStreamOpen(t *testing.T) {
	tests := []struct {
		name     string
		lineEnd  string
		blankEnd string
	}{
		{name: "LF", lineEnd: "\n", blankEnd: "\n"},
		{name: "CRLF", lineEnd: "\r\n", blankEnd: "\r\n"},
		{name: "CR", lineEnd: "\r", blankEnd: "\r"},
		{name: "mixed", lineEnd: "\r\n", blankEnd: "\r"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sse := newBareSSE()
			ctx, cancel := context.WithCancel(t.Context())
			notifications := make(chan mcp.JSONRPCNotification, 1)
			sse.SetNotificationHandler(func(n mcp.JSONRPCNotification) {
				select {
				case notifications <- n:
				case <-ctx.Done():
				}
			})
			reader, writer := io.Pipe()
			done := make(chan struct{})
			endpoint := make(chan struct{})
			go func() {
				defer close(done)
				sse.readSSE(reader, endpoint, &sync.Once{})
			}()
			t.Cleanup(func() {
				cancel()
				_ = writer.Close()
				_ = reader.Close()
				<-done
			})
			_, err := io.WriteString(writer, strings.Join([]string{"event: endpoint", "data: /messages", ""}, tt.lineEnd)+tt.blankEnd)
			require.NoError(t, err)
			select {
			case <-endpoint:
			case <-time.After(time.Second):
				t.Fatal("endpoint was not dispatched while the stream remained open")
			}
			_, err = io.WriteString(writer, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/test\"}"+tt.lineEnd+tt.blankEnd)
			require.NoError(t, err)
			select {
			case n := <-notifications:
				require.Equal(t, "notifications/test", n.Method)
			case <-time.After(time.Second):
				t.Fatal("notification was not dispatched while the stream remained open")
			}
		})
	}
}

func TestStreamableHTTP_LineEndingsWhileStreamOpen(t *testing.T) {
	tests := []struct {
		name     string
		lineEnd  string
		blankEnd string
	}{
		{name: "LF", lineEnd: "\n", blankEnd: "\n"},
		{name: "CRLF", lineEnd: "\r\n", blankEnd: "\r\n"},
		{name: "CR", lineEnd: "\r", blankEnd: "\r"},
		{name: "mixed", lineEnd: "\r\n", blankEnd: "\r"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport, err := NewStreamableHTTP("http://127.0.0.1:1/")
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			reader, writer := io.Pipe()
			done := make(chan struct{})
			events := make(chan string, 1)
			var cursor sseCursor
			go func() {
				defer close(done)
				transport.readSSEWithCursor(ctx, reader, &cursor, func(event, data string) {
					select {
					case events <- event + ":" + data:
					case <-ctx.Done():
					}
				})
			}()
			t.Cleanup(func() {
				cancel()
				_ = writer.Close()
				_ = reader.Close()
				<-done
			})
			_, err = io.WriteString(writer, strings.Join([]string{"id: event-1", "retry: 123", "event: custom", "data: one", "data: two", ""}, tt.lineEnd)+tt.blankEnd)
			require.NoError(t, err)
			select {
			case got := <-events:
				require.Equal(t, "custom:one\ntwo", got)
			case <-time.After(time.Second):
				t.Fatal("event was not dispatched while the stream remained open")
			}
			cancel()
			<-done
			require.Equal(t, "event-1", cursor.lastEventID)
			require.Equal(t, 123*time.Millisecond, cursor.retry)
			require.True(t, cursor.hasRetry)
		})
	}
}
