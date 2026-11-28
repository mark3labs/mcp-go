package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

type serverRequestQueueCase struct {
	name   string
	send   func(ctx context.Context, s *streamableHttpSession) error
	queue  func(s *streamableHttpSession) (length, capacity int)
	answer func(ctx context.Context, s *streamableHttpSession)
}

func serverRequestQueueCases() []serverRequestQueueCase {
	return []serverRequestQueueCase{
		{
			name: "sampling",
			send: func(ctx context.Context, s *streamableHttpSession) error {
				_, err := s.RequestSampling(ctx, mcp.CreateMessageRequest{})
				return err
			},
			queue: func(s *streamableHttpSession) (int, int) {
				return len(s.samplingRequestChan), cap(s.samplingRequestChan)
			},
			answer: func(ctx context.Context, s *streamableHttpSession) {
				for {
					select {
					case item := <-s.samplingRequestChan:
						item.response <- samplingResponseItem{requestID: item.requestID, result: json.RawMessage(`{"role":"assistant","content":{"type":"text","text":"hi"},"model":"m"}`)}
					case <-ctx.Done():
						return
					}
				}
			},
		},
		{
			name: "elicitation",
			send: func(ctx context.Context, s *streamableHttpSession) error {
				_, err := s.RequestElicitation(ctx, mcp.ElicitationRequest{})
				return err
			},
			queue: func(s *streamableHttpSession) (int, int) {
				return len(s.elicitationRequestChan), cap(s.elicitationRequestChan)
			},
			answer: func(ctx context.Context, s *streamableHttpSession) {
				for {
					select {
					case item := <-s.elicitationRequestChan:
						item.response <- samplingResponseItem{requestID: item.requestID, result: json.RawMessage(`{"action":"accept","content":{}}`)}
					case <-ctx.Done():
						return
					}
				}
			},
		},
		{
			name: "roots",
			send: func(ctx context.Context, s *streamableHttpSession) error {
				_, err := s.ListRoots(ctx, mcp.ListRootsRequest{})
				return err
			},
			queue: func(s *streamableHttpSession) (int, int) {
				return len(s.rootsRequestChan), cap(s.rootsRequestChan)
			},
			answer: func(ctx context.Context, s *streamableHttpSession) {
				for {
					select {
					case item := <-s.rootsRequestChan:
						item.response <- samplingResponseItem{requestID: item.requestID, result: json.RawMessage(`{"roots":[]}`)}
					case <-ctx.Done():
						return
					}
				}
			},
		},
	}
}

func fillServerRequestQueue(t *testing.T, ctx context.Context, tt serverRequestQueueCase, session *streamableHttpSession, errs chan<- error) {
	t.Helper()
	_, capacity := tt.queue(session)
	for range capacity {
		go func() { errs <- tt.send(ctx, session) }()
	}
	require.Eventually(t, func() bool {
		length, _ := tt.queue(session)
		return length == capacity
	}, 5*time.Second, time.Millisecond)
}

func requireStillWaiting(t *testing.T, errs <-chan error) {
	t.Helper()
	select {
	case err := <-errs:
		t.Fatalf("request into the full queue returned before the queue had room: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestStreamableHTTPSession_ServerRequestWaitsForQueueRoom(t *testing.T) {
	for _, tt := range serverRequestQueueCases() {
		t.Run(tt.name, func(t *testing.T) {
			session := NewStreamableHTTPServer(NewMCPServer("test-server", "1.0.0")).newSession("session-1")
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			_, capacity := tt.queue(session)
			total := capacity + 1
			errs := make(chan error, total)
			fillServerRequestQueue(t, ctx, tt, session, errs)

			go func() { errs <- tt.send(ctx, session) }()
			requireStillWaiting(t, errs)
			go tt.answer(ctx, session)

			for range total {
				require.NoError(t, <-errs)
			}
		})
	}
}

func TestStreamableHTTPSession_ServerRequestStopsWaitingWhenSessionCloses(t *testing.T) {
	closers := []struct {
		name  string
		close func(ctx context.Context, srv *StreamableHTTPServer, session *streamableHttpSession)
	}{
		{name: "closed", close: func(_ context.Context, _ *StreamableHTTPServer, session *streamableHttpSession) {
			session.closeDone()
		}},
		{name: "terminated", close: func(ctx context.Context, srv *StreamableHTTPServer, session *streamableHttpSession) {
			srv.cleanupSessionState(ctx, session.SessionID())
		}},
	}
	for _, tt := range serverRequestQueueCases() {
		for _, closer := range closers {
			t.Run(tt.name+"/"+closer.name, func(t *testing.T) {
				srv := NewStreamableHTTPServer(NewMCPServer("test-server", "1.0.0"))
				session := srv.newSession("session-1")
				srv.activeSessions.Store(session.SessionID(), session)
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()

				_, capacity := tt.queue(session)
				fillServerRequestQueue(t, ctx, tt, session, make(chan error, capacity))

				extra := make(chan error, 1)
				go func() { extra <- tt.send(ctx, session) }()
				requireStillWaiting(t, extra)
				closer.close(ctx, srv, session)

				select {
				case err := <-extra:
					require.ErrorIs(t, err, errSessionClosed)
				case <-time.After(time.Second):
					t.Fatal("request waiting for queue room did not return when the session ended")
				}
			})
		}
	}
}
