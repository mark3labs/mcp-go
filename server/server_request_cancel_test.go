package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

// requireCancels asserts that notification withdraws the server request id.
func requireCancels(t *testing.T, notification mcp.JSONRPCNotification, id int64) {
	t.Helper()
	b, err := json.Marshal(notification)
	require.NoError(t, err)
	var got struct {
		Method string `json:"method"`
		Params struct {
			RequestID json.RawMessage `json:"requestId"`
			Reason    string          `json:"reason"`
		} `json:"params"`
	}
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, string(mcp.MethodNotificationCancelled), got.Method)
	assert.JSONEq(t, fmt.Sprint(id), string(got.Params.RequestID))
	assert.Equal(t, context.Canceled.Error(), got.Params.Reason)
}

// chanWriter hands each write to a channel.
type chanWriter chan []byte

func (w chanWriter) Write(p []byte) (int, error) {
	w <- bytes.Clone(p)
	return len(p), nil
}

// A stdio server request abandoned because its context ended is withdrawn
// with notifications/cancelled, and a response arriving afterwards finds
// nothing waiting for it. An answered request is not withdrawn.
func TestStdioSession_CancelsAbandonedServerRequests(t *testing.T) {
	tests := []struct {
		name   string
		call   func(context.Context, *stdioSession) error
		answer func(*stdioSession, string) bool
	}{
		{
			name: "sampling",
			call: func(ctx context.Context, s *stdioSession) error {
				_, err := s.RequestSampling(ctx, mcp.CreateMessageRequest{})
				return err
			},
			answer: func(s *stdioSession, msg string) bool {
				return s.handleSamplingResponse(json.RawMessage(msg))
			},
		},
		{
			name: "elicitation",
			call: func(ctx context.Context, s *stdioSession) error {
				_, err := s.RequestElicitation(ctx, mcp.ElicitationRequest{})
				return err
			},
			answer: func(s *stdioSession, msg string) bool {
				return s.handleElicitationResponse(json.RawMessage(msg))
			},
		},
		{
			name: "roots",
			call: func(ctx context.Context, s *stdioSession) error {
				_, err := s.ListRoots(ctx, mcp.ListRootsRequest{})
				return err
			},
			answer: func(s *stdioSession, msg string) bool {
				return s.handleListRootsResponse(json.RawMessage(msg))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := &stdioSession{
				notifications:       make(chan mcp.JSONRPCNotification, 1),
				pendingRequests:     make(map[int64]chan *samplingResponse),
				pendingElicitations: make(map[int64]chan *elicitationResponse),
				pendingRoots:        make(map[int64]chan *rootsResponse),
			}
			written := make(chan []byte, 1)
			session.SetWriter(chanWriter(written))
			sent := func() int64 {
				var request struct {
					ID int64 `json:"id"`
				}
				require.NoError(t, json.Unmarshal(<-written, &request))
				return request.ID
			}
			response := func(id int64) string {
				return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"role":"assistant","model":"m","content":{"type":"text","text":"ok"},"action":"decline","roots":[]}}`, id)
			}

			ctx, cancel := context.WithCancel(t.Context())
			errc := make(chan error, 1)
			go func() { errc <- tt.call(ctx, session) }()
			id := sent()
			cancel()
			require.ErrorIs(t, <-errc, context.Canceled)
			require.Len(t, session.notifications, 1)
			requireCancels(t, <-session.notifications, id)

			assert.False(t, tt.answer(session, response(id)), "a late response must find nothing pending")
			assert.Empty(t, session.pendingRequests)
			assert.Empty(t, session.pendingElicitations)
			assert.Empty(t, session.pendingRoots)

			go func() { errc <- tt.call(t.Context(), session) }()
			require.True(t, tt.answer(session, response(sent())))
			require.NoError(t, <-errc)
			assert.Empty(t, session.notifications, "an answered request must not be withdrawn")
		})
	}
}

// httpServerRequests are the server requests a streamable HTTP tool handler
// can issue through the MCPServer.
var httpServerRequests = []struct {
	name string
	call func(context.Context, *MCPServer) error
}{
	{"sampling", func(ctx context.Context, s *MCPServer) error {
		_, err := s.RequestSampling(ctx, mcp.CreateMessageRequest{})
		return err
	}},
	{"elicitation", func(ctx context.Context, s *MCPServer) error {
		_, err := s.RequestElicitation(ctx, mcp.ElicitationRequest{Params: mcp.ElicitationParams{
			Message:         "Proceed?",
			RequestedSchema: map[string]any{"type": "object"},
		}})
		return err
	}},
	{"roots", func(ctx context.Context, s *MCPServer) error {
		_, err := s.RequestRoots(ctx, mcp.ListRootsRequest{})
		return err
	}},
}

// A server request sent on the originating POST's stream and then abandoned
// is withdrawn on that same stream, ahead of the tool's response, and a
// response arriving afterwards is refused without leaving anything pending.
func TestStreamableHTTPServer_CancelsAbandonedRequestOnPOSTStream(t *testing.T) {
	for _, tt := range httpServerRequests {
		t.Run(tt.name, func(t *testing.T) {
			mcpServer := NewMCPServer("test-server", "1.0.0", WithRoots())
			mcpServer.EnableSampling()
			cancels := make(chan context.CancelFunc, 1)
			mcpServer.AddTool(mcp.NewTool("ask"), func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				ctx, cancel := context.WithCancel(ctx)
				cancels <- cancel
				return mcp.NewToolResultText(fmt.Sprint(tt.call(ctx, mcpServer))), nil
			})
			httpServer := NewStreamableHTTPServer(mcpServer)
			testServer := httptest.NewServer(httpServer)
			defer testServer.Close()
			sessionID := initializeSession(t, testServer.URL)

			resp, reader := postToolCall(t, testServer.URL, sessionID, "ask")
			defer resp.Body.Close()

			var request struct {
				ID int64 `json:"id"`
			}
			readSSEData(t, reader, &request)
			(<-cancels)()

			var cancelled mcp.JSONRPCNotification
			readSSEData(t, reader, &cancelled)
			requireCancels(t, cancelled, request.ID)
			assert.Equal(t, context.Canceled.Error(), readToolResultText(t, reader))

			require.NoError(t, postServerResponse(testServer.URL, sessionID, json.RawMessage(fmt.Sprint(request.ID)), map[string]any{}))
			httpServer.pendingRequests.Range(func(key, _ any) bool {
				t.Errorf("request %v still pending", key)
				return true
			})
		})
	}
}

// A server request that went to the standalone GET stream is withdrawn
// through the session's notification channel, which feeds that stream.
func TestStreamableHttpSession_CancelsAbandonedRequestOnGETStream(t *testing.T) {
	tests := []struct {
		name   string
		call   func(context.Context, *streamableHttpSession) error
		queued func(*streamableHttpSession) int64
	}{
		{
			name: "sampling",
			call: func(ctx context.Context, s *streamableHttpSession) error {
				_, err := s.RequestSampling(ctx, mcp.CreateMessageRequest{})
				return err
			},
			queued: func(s *streamableHttpSession) int64 { return (<-s.samplingRequestChan).requestID },
		},
		{
			name: "elicitation",
			call: func(ctx context.Context, s *streamableHttpSession) error {
				_, err := s.RequestElicitation(ctx, mcp.ElicitationRequest{})
				return err
			},
			queued: func(s *streamableHttpSession) int64 { return (<-s.elicitationRequestChan).requestID },
		},
		{
			name: "roots",
			call: func(ctx context.Context, s *streamableHttpSession) error {
				_, err := s.ListRoots(ctx, mcp.ListRootsRequest{})
				return err
			},
			queued: func(s *streamableHttpSession) int64 { return (<-s.rootsRequestChan).requestID },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := newStreamableHttpSession("s1", nil, nil, nil, nil, new(atomic.Int64))
			ctx, cancel := context.WithCancel(t.Context())
			errc := make(chan error, 1)
			go func() { errc <- tt.call(ctx, session) }()
			id := tt.queued(session)
			cancel()
			require.ErrorIs(t, <-errc, context.Canceled)
			require.Len(t, session.notificationChannel, 1)
			requireCancels(t, <-session.notificationChannel, id)
			session.samplingRequests.Range(func(key, _ any) bool {
				t.Errorf("request %v still pending", key)
				return true
			})
		})
	}
}
