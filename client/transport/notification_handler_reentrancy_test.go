package transport

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestTransportNotificationHandlerCanReplaceItself verifies that replacing and
// removing a notification handler from its callback preserves notification order
// and allows the receive loop to deliver the following response.
func TestTransportNotificationHandlerCanReplaceItself(t *testing.T) {
	const first = `{"jsonrpc":"2.0","method":"notifications/first"}`
	const second = `{"jsonrpc":"2.0","method":"notifications/second"}`
	const ignored = `{"jsonrpc":"2.0","method":"notifications/ignored"}`
	const response = `{"jsonrpc":"2.0","id":1,"result":{}}`
	responseID := mcp.NewRequestId(int64(1)).String()
	messages := []string{first, second, ignored, response}
	var sse strings.Builder
	for _, message := range messages {
		fmt.Fprintf(&sse, "event: message\ndata: %s\n\n", message)
	}

	tests := []struct {
		name  string
		setup func(t *testing.T) (Interface, func() (*JSONRPCResponse, error))
	}{
		{
			name: "stdio",
			setup: func(t *testing.T) (Interface, func() (*JSONRPCResponse, error)) {
				c := NewIO(strings.NewReader(strings.Join(messages, "\n")+"\n"), nil, nil)
				responses := make(chan *JSONRPCResponse, 1)
				c.responses[responseID] = responses
				return c, func() (*JSONRPCResponse, error) {
					c.readResponses(nil)
					select {
					case result := <-responses:
						return result, nil
					default:
						return nil, fmt.Errorf("response after notifications was not delivered")
					}
				}
			},
		},
		{
			name: "sse",
			setup: func(t *testing.T) (Interface, func() (*JSONRPCResponse, error)) {
				c, err := NewSSE("http://localhost")
				require.NoError(t, err)
				c.SetConnectionLostHandler(func(error) {})
				responses := make(chan *JSONRPCResponse, 1)
				c.responses[responseID] = responses
				return c, func() (*JSONRPCResponse, error) {
					c.readSSE(io.NopCloser(strings.NewReader(sse.String())), make(chan struct{}), &sync.Once{})
					select {
					case result := <-responses:
						return result, nil
					default:
						return nil, fmt.Errorf("response after notifications was not delivered")
					}
				}
			},
		},
		{
			name: "streamable_http",
			setup: func(t *testing.T) (Interface, func() (*JSONRPCResponse, error)) {
				c, err := NewStreamableHTTP("http://localhost")
				require.NoError(t, err)
				return c, func() (*JSONRPCResponse, error) {
					return c.handleSSEResponse(t.Context(), io.NopCloser(strings.NewReader(sse.String())), false)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, receive := tt.setup(t)
			var received []string
			c.SetNotificationHandler(func(notification mcp.JSONRPCNotification) {
				received = append(received, "original:"+notification.Method)
				c.SetNotificationHandler(func(notification mcp.JSONRPCNotification) {
					received = append(received, "replacement:"+notification.Method)
					c.SetNotificationHandler(nil)
				})
			})

			done := make(chan struct{})
			var result *JSONRPCResponse
			var err error
			go func() {
				defer close(done)
				result, err = receive()
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("notification handler replacement blocked the receive loop")
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, responseID, result.ID.String())
			require.Equal(t, []string{
				"original:notifications/first",
				"replacement:notifications/second",
			}, received)
		})
	}
}
