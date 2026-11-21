package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

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
func TestStreamableHttpSession_CancelsDeliveredRequestOnGETStream(t *testing.T) {
	tests := []struct {
		name    string
		call    func(context.Context, *streamableHttpSession) error
		deliver func(*streamableHttpSession) int64
	}{
		{
			name: "sampling",
			call: func(ctx context.Context, s *streamableHttpSession) error {
				_, err := s.RequestSampling(ctx, mcp.CreateMessageRequest{})
				return err
			},
			deliver: func(s *streamableHttpSession) int64 {
				item := <-s.samplingRequestChan
				require.True(t, item.take())
				require.Nil(t, item.wrote())
				return item.requestID
			},
		},
		{
			name: "elicitation",
			call: func(ctx context.Context, s *streamableHttpSession) error {
				_, err := s.RequestElicitation(ctx, mcp.ElicitationRequest{})
				return err
			},
			deliver: func(s *streamableHttpSession) int64 {
				item := <-s.elicitationRequestChan
				require.True(t, item.take())
				require.Nil(t, item.wrote())
				return item.requestID
			},
		},
		{
			name: "roots",
			call: func(ctx context.Context, s *streamableHttpSession) error {
				_, err := s.ListRoots(ctx, mcp.ListRootsRequest{})
				return err
			},
			deliver: func(s *streamableHttpSession) int64 {
				item := <-s.rootsRequestChan
				require.True(t, item.take())
				require.Nil(t, item.wrote())
				return item.requestID
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := newStreamableHttpSession("s1", nil, nil, nil, nil, new(atomic.Int64))
			ctx, cancel := context.WithCancel(t.Context())
			errc := make(chan error, 1)
			go func() { errc <- tt.call(ctx, session) }()
			id := tt.deliver(session)
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

// A request queued on the POST stream whose context ends before it is
// written is marked abandoned, so the stream skips it instead of writing it
// after the cancellation the caller is about to publish.
func TestRequestScopedSSE_AbandonsQueuedRequestWhenContextEnds(t *testing.T) {
	requests := make(chan *scopedRequest, 8)
	scoped := &requestScopedSSE{
		requests:      requests,
		notifications: make(chan mcp.JSONRPCNotification, 100),
		done:          make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(t.Context())
	type outcome struct {
		sent   bool
		issued bool
	}
	result := make(chan outcome, 1)
	go func() {
		sent, issued := scoped.trySend(ctx, mcp.JSONRPCRequest{})
		result <- outcome{sent, issued}
	}()
	// The item is queued but no one is writing it yet.
	item := <-requests
	cancel()
	got := <-result
	require.True(t, got.sent)
	require.False(t, got.issued, "an unwritten queued request was not issued")
	item.mu.Lock()
	require.True(t, item.abandoned, "an unwritten queued request must be marked abandoned")
	item.mu.Unlock()
}

// A request cancelled while still queued on a session request channel was
// never issued: the consumer skips it and no cancellation is sent, since it
// would reference an ID the client never received.
func TestStreamableHttpSession_AbandonsQueuedRequestBeforeCancelling(t *testing.T) {
	session := newStreamableHttpSession("s1", nil, nil, nil, nil, new(atomic.Int64))
	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)
	go func() {
		_, err := session.RequestSampling(ctx, mcp.CreateMessageRequest{})
		errc <- err
	}()
	require.Eventually(t, func() bool { return len(session.samplingRequestChan) == 1 },
		5*time.Second, time.Millisecond, "request should be queued")
	cancel()
	require.ErrorIs(t, <-errc, context.Canceled)
	item := <-session.samplingRequestChan
	require.False(t, item.take(), "a consumer must skip the cancelled request")
	require.Empty(t, session.notificationChannel,
		"no cancellation may reference a request the client never received")
}

// When a stream consumer has already claimed a queued request, the caller
// must not publish the cancellation: another stream's consumer could take
// it from the shared notification channel and deliver it before the
// claiming stream writes the request. The claiming stream delivers the
// cancellation itself, right behind the request.
func TestStreamableHttpSession_ClaimedRequestRidesItsCancellation(t *testing.T) {
	session := newStreamableHttpSession("s1", nil, nil, nil, nil, new(atomic.Int64))
	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)
	go func() {
		_, err := session.RequestSampling(ctx, mcp.CreateMessageRequest{})
		errc <- err
	}()
	require.Eventually(t, func() bool { return len(session.samplingRequestChan) == 1 },
		5*time.Second, time.Millisecond, "request should be queued")
	item := <-session.samplingRequestChan
	require.True(t, item.take(), "an uncancelled request must be claimable")
	cancel()
	require.ErrorIs(t, <-errc, context.Canceled)
	require.Empty(t, session.notificationChannel,
		"the caller must not publish a cancellation another stream could deliver first")
	// The stream writes the request, and hands it over at the write: the
	// cancellation recorded before the write rides right behind it.
	ride := item.wrote()
	require.NotNil(t, ride, "the claiming stream must deliver the cancellation behind the request")
	requireCancels(t, *ride, item.requestID)
}

// A cancellation decided after the stream wrote the request — the common
// case, a client that received the request and never answered — is
// delivered by the caller through the session's notification channel.
func TestStreamableHttpSession_CancellationAfterDeliveryIsPublished(t *testing.T) {
	session := newStreamableHttpSession("s1", nil, nil, nil, nil, new(atomic.Int64))
	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)
	go func() {
		_, err := session.RequestSampling(ctx, mcp.CreateMessageRequest{})
		errc <- err
	}()
	require.Eventually(t, func() bool { return len(session.samplingRequestChan) == 1 },
		5*time.Second, time.Millisecond, "request should be queued")
	item := <-session.samplingRequestChan
	require.True(t, item.take(), "an uncancelled request must be claimable")
	require.Nil(t, item.wrote(), "no cancellation is pending at the write")
	cancel()
	require.ErrorIs(t, <-errc, context.Canceled)
	require.Len(t, session.notificationChannel, 1)
	requireCancels(t, <-session.notificationChannel, item.requestID)
}

// A request the client received and never answered is withdrawn when the
// server gives up on it: the standalone GET stream that delivered the
// request also delivers notifications/cancelled for it, with the matching
// request ID. This is the transport-level view of the delivery handoff, and
// the case a client like an open elicitation dialog needs to close it.
func TestStreamableHTTPServer_CancelsDeliveredRequestOnGETStream(t *testing.T) {
	cancels := make(chan context.CancelFunc, 1)
	mcpServer := NewMCPServer("test-server", "1.0.0", WithRoots())
	mcpServer.AddTool(mcp.NewTool("askroots"), func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// A detached context with the request-scoped stream cleared, so the
		// roots request takes the GET stream instead of this POST's own
		// stream, as a background task would.
		detached := context.WithValue(context.WithoutCancel(ctx), requestScopedSSEKey{}, nil)
		rootsCtx, cancel := context.WithCancel(detached)
		cancels <- cancel
		go func() { _, _ = mcpServer.RequestRoots(rootsCtx, mcp.ListRootsRequest{}) }()
		return mcp.NewToolResultText("asking"), nil
	})
	testServer := httptest.NewServer(NewStreamableHTTPServer(mcpServer))
	defer testServer.Close()
	sessionID := initializeSession(t, testServer.URL)

	getReq, _ := http.NewRequest(http.MethodGet, testServer.URL, nil)
	getReq.Header.Set("Accept", "text/event-stream")
	getReq.Header.Set(HeaderKeySessionID, sessionID)
	getResp, err := scopedRequestClient.Do(getReq)
	require.NoError(t, err)
	defer getResp.Body.Close()
	require.Equal(t, "text/event-stream", getResp.Header.Get("Content-Type"))
	reader := bufio.NewReader(getResp.Body)

	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 7, "method": "tools/call",
		"params": map[string]any{"name": "askroots"},
	})
	postReq, _ := http.NewRequest(http.MethodPost, testServer.URL, bytes.NewReader(body))
	postReq.Header.Set("Content-Type", "application/json")
	postReq.Header.Set("Accept", "application/json")
	postReq.Header.Set(HeaderKeySessionID, sessionID)
	postResp, err := http.DefaultClient.Do(postReq)
	require.NoError(t, err)
	result, _ := io.ReadAll(postResp.Body)
	postResp.Body.Close()
	assert.Contains(t, string(result), "asking", "the tool call should have completed without the roots answer")

	request, err := nextSSEMessage(reader)
	require.NoError(t, err)
	require.Equal(t, string(mcp.MethodListRoots), request.Method)

	// The client has the request open and does not answer. The server
	// gives up on it, and the GET stream carries the withdrawal.
	(<-cancels)()

	var cancelled struct {
		Method string `json:"method"`
		Params struct {
			RequestID json.RawMessage `json:"requestId"`
			Reason    string          `json:"reason"`
		} `json:"params"`
	}
	readSSEData(t, reader, &cancelled)
	require.Equal(t, string(mcp.MethodNotificationCancelled), cancelled.Method)
	assert.JSONEq(t, string(request.ID), string(cancelled.Params.RequestID))
	assert.Equal(t, context.Canceled.Error(), cancelled.Params.Reason)

}
