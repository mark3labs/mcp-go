package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Hirocloud/mcp-go/mcp"
	"github.com/google/uuid"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type Session interface {
	SessionID() string
	NotificationChannel() chan<- mcp.JSONRPCNotification
	Initialize()
	Initialized() bool
	QueueEvent(event string)
	GetEvent() chan string
	IsDone() chan struct{}
	Done()
	IsLocal() bool
}

var _ Session = &MCMCPSession{}

// MCMCPSession (Multi-Cluster MCP Session)
type MCMCPSession struct {
	sessionID   string
	initialized atomic.Bool

	localQueue  Queue[string]
	remoteQueue Queue[string]

	localQueueNotifications  Queue[string]
	remoteQueueNotifications Queue[string]
	LastUpdated              time.Time
}

func NewMCMCPSession() Session {
	//sessionID := uuid.New().String()
	//session := &MCMCPSession{}
	return &MCMCPSession{}
}

func (M *MCMCPSession) IsLocal() bool {
	//TODO implement me
	panic("implement me")
}

func (M *MCMCPSession) SessionID() string {
	//TODO implement me
	panic("implement me")
}

func (M *MCMCPSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	//TODO implement me
	panic("implement me")
}

func (M *MCMCPSession) Initialize() {
	//TODO implement me
	panic("implement me")
}

func (M *MCMCPSession) Initialized() bool {
	//TODO implement me
	panic("implement me")
}

func (M *MCMCPSession) QueueEvent(event string) {
	//TODO implement me
	panic("implement me")
}

func (M *MCMCPSession) GetEvent() chan string {
	//TODO implement me
	panic("implement me")
}

func (M *MCMCPSession) IsDone() chan struct{} {
	//TODO implement me
	panic("implement me")
}

func (M *MCMCPSession) Done() {
	//TODO implement me
	panic("implement me")
}

// todo update this to use mCMCPSession
type MCSSEServer struct {
	server          *MCPServer
	baseURL         string
	basePath        string
	messageEndpoint string
	sseEndpoint     string
	srv             *http.Server
	contextFunc     SSEContextFunc
	localSessions   sync.Map

	//todo remove these queues and use sessions map...
	localQueue  Queue[string]
	remoteQueue Queue[string]

	localQueueNotifications  Queue[string]
	remoteQueueNotifications Queue[string]
}

func (s *MCSSEServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher := upgradeWriter(w)
	if flusher == nil {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	sessionID := uuid.New().String()
	session := NewMCMCPSession()

	err := s.localQueue.New(ctx, sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session registration failed: %v", err), http.StatusInternalServerError)
		return
	}
	localPublishQueue := make(chan string)
	go func() {
		err := s.localQueue.PublishFromChan(ctx, sessionID, localPublishQueue)
		if err != nil {
			fmt.Printf("error publishing from chan: %s", err.Error())
			cancel()
		}
	}()

	//todo add done logic?
	//	defer s.server.UnregisterSession(sessionID)
	if err := s.server.RegisterSession(session); err != nil {
		http.Error(w, fmt.Sprintf("Session registration failed: %v", err), http.StatusInternalServerError)
		return
	}
	defer s.server.UnregisterSession(sessionID)
	go func() {
		subscribeToNotifications(ctx, sessionID, s.localQueueNotifications, localPublishQueue)
	}()
	go func() {
		subscribeToNotifications(ctx, sessionID, s.remoteQueueNotifications, localPublishQueue)
	}()

	//messageEndpoint := fmt.Sprintf("%s?sessionId=%s", s.CompleteMessageEndpoint(), sessionID)

	// Send the initial endpoint event
	//fmt.Fprintf(w, "event: endpoint\ndata: %s\r\n\r\n", messageEndpoint)
	flusher.Flush()
	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		defer wg.Done()
		subscribeToEvent(ctx, cancel, sessionID, s.localQueue, w, flusher)
	}()
	go func() {
		defer wg.Done()
		subscribeToEvent(ctx, cancel, sessionID, s.remoteQueue, w, flusher)
	}()
	wg.Wait()
}

func subscribeToEvent(ctx context.Context, cancel func(), sessionID string, queue Queue[string], w http.ResponseWriter, flusher http.Flusher) {
	for {
		select {
		case event := <-queue.Subscribe(ctx, sessionID):
			// Write the event to the response
			_, err := fmt.Fprint(w, event)
			if err != nil {
				fmt.Printf("error writing to response: %s", err)
				cancel()
				return
			}
			flusher.Flush()
		case <-ctx.Done():
			cancel()
			return
		}
	}
}

func subscribeToNotifications(ctx context.Context, sessionID string, queue Queue[string], publishQueue chan string) {
	notifications := queue.Subscribe(ctx, sessionID)
	for {
		select {
		case notification := <-notifications:
			eventData, err := json.Marshal(notification)
			if err == nil {
				select {
				case publishQueue <- fmt.Sprintf("event: message\ndata: %s\n\n", eventData):
					// Event queued successfully
				case <-ctx.Done():
					return
				}
			}
		case <-ctx.Done():
			return
		}
	}

}

func upgradeWriter(w http.ResponseWriter) http.Flusher {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return nil
	}
	return flusher
}
