package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestStreamableHTTPSession_ServerRequestWaitsForQueueRoom(t *testing.T) {
	tests := []struct {
		name   string
		send   func(ctx context.Context, s *streamableHttpSession) error
		queue  func(s *streamableHttpSession) (length, capacity int)
		answer func(ctx context.Context, s *streamableHttpSession)
	}{
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
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := NewStreamableHTTPServer(NewMCPServer("test-server", "1.0.0")).newSession("session-1")
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			_, capacity := tt.queue(session)
			total := capacity + 1
			errs := make(chan error, total)
			for range capacity {
				go func() { errs <- tt.send(ctx, session) }()
			}
			require.Eventually(t, func() bool {
				length, _ := tt.queue(session)
				return length == capacity
			}, 5*time.Second, time.Millisecond)

			go func() { errs <- tt.send(ctx, session) }()
			go tt.answer(ctx, session)

			for range total {
				require.NoError(t, <-errs)
			}
		})
	}
}
