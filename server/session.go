package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Hirocloud/mcp-go/mcp"
	"github.com/Hirocloud/mcp-go/server/queues"
	session2 "github.com/Hirocloud/mcp-go/server/session"
	"github.com/gorilla/mux"
	"github.com/redis/go-redis/v9"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

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
	remoteQueue              queues.Queue[string]
	remoteQueueNotifications queues.Queue[mcp.JSONRPCNotification]

	redisClient redis.Cmdable
}

type MCSSEOption func(*MCSSEServer)

// WithBaseURL sets the base URL for the SSE server
func WithMCBaseURL(baseURL string) MCSSEOption {
	return func(s *MCSSEServer) {
		if baseURL != "" {
			u, err := url.Parse(baseURL)
			if err != nil {
				return
			}
			if u.Scheme != "http" && u.Scheme != "https" {
				return
			}
			// Check if the host is empty or only contains a port
			if u.Host == "" || strings.HasPrefix(u.Host, ":") {
				return
			}
			if len(u.Query()) > 0 {
				return
			}
		}
		s.baseURL = strings.TrimSuffix(baseURL, "/")
	}
}

// Add a new option for setting base path
func WithMCBasePath(basePath string) MCSSEOption {
	return func(s *MCSSEServer) {
		// Ensure the path starts with / and doesn't end with /
		if !strings.HasPrefix(basePath, "/") {
			basePath = "/" + basePath
		}
		s.basePath = strings.TrimSuffix(basePath, "/")
	}
}

// WithMessageEndpoint sets the message endpoint path
func WithMCMessageEndpoint(endpoint string) MCSSEOption {
	return func(s *MCSSEServer) {
		s.messageEndpoint = endpoint
	}
}

// WithSSEEndpoint sets the SSE endpoint path
func WithMCSSEEndpoint(endpoint string) MCSSEOption {
	return func(s *MCSSEServer) {
		s.sseEndpoint = endpoint
	}
}

// WithHTTPServer sets the HTTP server instance
func WithMCTTPServer(srv *http.Server) MCSSEOption {
	return func(s *MCSSEServer) {
		s.srv = srv
	}
}

// WithContextFunc sets a function that will be called to customise the context
// to the server using the incoming request.
func WithMCSSEContextFunc(fn SSEContextFunc) MCSSEOption {
	return func(s *MCSSEServer) {
		s.contextFunc = fn
	}
}

func NewMCSSEServer(server *MCPServer, redisClient redis.Cmdable, opts ...MCSSEOption) *MCSSEServer {
	s := &MCSSEServer{
		server:                   server,
		sseEndpoint:              "/sse",
		messageEndpoint:          "/message",
		redisClient:              redisClient,
		remoteQueue:              queues.NewRedisQueue[string](redisClient, ""),
		remoteQueueNotifications: queues.NewRedisQueue[mcp.JSONRPCNotification](redisClient, session2.NotificationQueueName),
	}
	for _, opt := range opts {
		opt(s)
	}
	//
	return s
}

// Start begins serving SSE connections on the specified address.
// It sets up HTTP handlers for SSE and message endpoints.
func (s *MCSSEServer) Start(addr string) error {
	s.srv = &http.Server{
		Addr:    addr,
		Handler: s,
	}
	return s.srv.ListenAndServe()
}

func (s *MCSSEServer) Shutdown(ctx context.Context) error {
	if s.srv != nil {
		s.localSessions.Range(func(key, value interface{}) bool {
			if session, ok := value.(session2.Session); ok {
				s.deleteSession(ctx, session)
			} else {
				s.localSessions.Delete(key)
			}
			return true
		})

		return s.srv.Shutdown(ctx)
	} else {
		s.Clean(ctx, true)
	}
	return nil
}

func (s *MCSSEServer) CompleteSseEndpoint() string {
	return s.baseURL + s.basePath + s.sseEndpoint
}

func (s *MCSSEServer) CompleteSsePath() string {
	path, err := GetUrlPath(s.CompleteSseEndpoint())
	if err != nil {
		return s.basePath + s.sseEndpoint
	}
	return path
}

// ServeHTTP implements the http.Handler interface.
func (s *MCSSEServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Use exact path matching rather than Contains
	ssePath := s.CompleteSsePath()
	if ssePath != "" && path == ssePath {
		s.handleSSE(w, r)
		return
	}
	messagePath := s.CompleteMessagePath()
	if messagePath != "" && path == messagePath {
		s.handleMessage(w, r)
		return
	}

	http.NotFound(w, r)
}

func (s *MCSSEServer) DoesSessionExist(ctx context.Context, sessionID string) (bool, bool) { //local, remote
	var local bool
	_, ok := s.localSessions.Load(sessionID)
	if ok {
		local = true
	}
	result, err := s.redisClient.Exists(ctx, "SES_"+sessionID).Result()
	if err != nil {
		return local, false
	}
	return local, result == 1
}

func (s *MCSSEServer) deleteSession(ctx context.Context, session session2.Session) {
	session.Cancel()
	s.localSessions.Delete(session.SessionID())
	_, err := s.redisClient.Del(ctx, "SES_"+session.SessionID()).Result()
	if err != nil {
		slog.Error("deleteSession: ", "err", err, "sessionID", session.SessionID())
		return
	} else {
		slog.Info("deleteSession: ", "sessionID", session.SessionID(), "local", session.IsLocal())
	}
}
func (s *MCSSEServer) CleanAuto(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		for {
			select {
			case <-ctx.Done():
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				s.Clean(ctx, true)
				cancel()
				return
			case <-ticker.C:
				s.Clean(ctx, false)
			}
		}
	}()

}

func (s *MCSSEServer) Clean(ctx context.Context, removeAll bool) {
	s.localSessions.Range(func(key, value interface{}) bool {
		if session, ok := value.(session2.Session); ok {
			if removeAll {
				s.deleteSession(ctx, session)
				return true
			}
			if session.IsLocal() {
				return true
			}
			result, err := s.redisClient.Exists(ctx, "SES_"+session.SessionID()).Result()
			if err != nil {
				return true
			}
			if result != 1 {
				slog.Info("Clean: ", "sessionID", session.SessionID())
				s.deleteSession(ctx, session)
			}
		} else {
			s.localSessions.Delete(key)
		}
		return true
	})
}

func (s *MCSSEServer) storeSession(ctx context.Context, session session2.Session) {
	s.redisClient.Set(ctx, "SES_"+session.SessionID(), "true", 0)
	s.localSessions.Store(session.SessionID(), session)
}

func (s *MCSSEServer) getSession(sessionID string) (session2.Session, error) {
	sessionI, ok := s.localSessions.Load(sessionID)
	if !ok {
		return nil, fmt.Errorf("invalid session ID")
	}
	session, ok := sessionI.(session2.Session)
	if !ok {
		return nil, fmt.Errorf("invalid session type")
	}
	return session, nil
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

	session, err := session2.NewLocalMCMCPSession(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.storeSession(session.Context(), session)
	defer s.deleteSession(context.Background(), session)
	if err := s.server.RegisterSession(session); err != nil {
		http.Error(w, fmt.Sprintf("Session registration failed: %v", err), http.StatusInternalServerError)
		return
	}
	defer s.server.UnregisterSession(session.SessionID())
	go func() {
		subscribeToNotifications[mcp.JSONRPCNotification](session.Context(), session.SessionID(), session.QueueNotificationEvent(), session.QueueEvent())
	}()
	go func() {
		subscribeToNotifications(session.Context(), session.SessionID(), s.remoteQueueNotifications, session.QueueEvent())
	}()
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host // Includes hostname and port (if present)
	fullURL := fmt.Sprintf("%s://%s", scheme, host)
	endpoint := strings.ReplaceAll(r.URL.Path, "/sse", "/message")

	messageEndpoint := fmt.Sprintf("%s%s?sessionId=%s", fullURL, endpoint, session.SessionID())
	println(messageEndpoint)
	// Send the initial endpoint event
	_, _ = fmt.Fprintf(w, "event: endpoint\ndata: %s\r\n\r\n", messageEndpoint)
	flusher.Flush()
	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		defer wg.Done()
		subscribeToEvent(session.Context(), session.Cancel, session.SessionID(), session.Queue(), w, flusher)
	}()
	go func() {
		defer wg.Done()
		subscribeToEvent(session.Context(), session.Cancel, session.SessionID(), s.remoteQueue, w, flusher)
	}()
	wg.Wait()
}

func (s *MCSSEServer) CompleteMessageEndpoint() string {
	return s.baseURL + s.basePath + s.messageEndpoint
}
func (s *MCSSEServer) CompleteMessagePath() string {
	path, err := GetUrlPath(s.CompleteMessageEndpoint())
	if err != nil {
		return s.basePath + s.messageEndpoint
	}
	return path
}

func (s *MCSSEServer) AddMuxRoutes(mux *mux.Router) {
	mux.HandleFunc(s.CompleteMessagePath(), s.handleMessage).Methods(http.MethodPost, http.MethodOptions)
	mux.HandleFunc(s.CompleteSsePath(), s.handleSSE).Methods(http.MethodGet, http.MethodOptions)
}

// handleMessage processes incoming JSON-RPC messages from clients and sends responses
// back through both the SSE connection and HTTP response.
func (s *MCSSEServer) handleMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		writeJSONRPCError(w, nil, mcp.INVALID_PARAMS, "Missing sessionId")
		return
	}
	local, redisSession := s.DoesSessionExist(r.Context(), sessionID)
	if !local && !redisSession {
		writeJSONRPCError(w, nil, mcp.INVALID_PARAMS, "Invalid sessionId")
		return
	}
	slog.Debug(">>> handleMessage: ", "session", sessionID, " local: ", local, " redis: ", redisSession)
	var session session2.Session
	var err error

	if local {
		session, err = s.getSession(sessionID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !session.IsLocal() && !redisSession {
			s.deleteSession(r.Context(), session)
			writeJSONRPCError(w, nil, mcp.INVALID_PARAMS, "Invalid sessionId")
			return
		}
	} else if !local {
		session, err = session2.NewRedisMCMCPSession(context.Background(), sessionID, s.redisClient)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.storeSession(session.Context(), session)
	}
	if session == nil {
		http.Error(w, "Invalid session", http.StatusInternalServerError)
		return
	}
	ctx := s.server.WithContext(r.Context(), session)
	if s.contextFunc != nil {
		ctx = s.contextFunc(ctx, r)
	}
	var rawMessage json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawMessage); err != nil {
		writeJSONRPCError(w, nil, mcp.PARSE_ERROR, "Parse error")
		return
	}
	response := s.server.HandleMessage(ctx, rawMessage)
	if response != nil {
		eventData, _ := json.Marshal(response)
		ticker := time.NewTicker(1 * time.Second)

		select {
		case session.QueueEvent() <- fmt.Sprintf("event: message\ndata: %s\n\n", eventData):
		case <-ticker.C:
			local, redisSession := s.DoesSessionExist(r.Context(), sessionID)
			if !local && !redisSession {
				s.deleteSession(r.Context(), session)
				return
			}
			if !session.IsLocal() && !redisSession {
				s.deleteSession(r.Context(), session)
				return
			}
			// Event queued successfully

		case <-ctx.Done():
			// Session is closed, don't try to queue
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

	} else {
		// For notifications, just send 202 Accepted with no body
		w.WriteHeader(http.StatusAccepted)
	}

}
func subscribeToEvent(ctx context.Context, cancel func(), sessionID string, queue queues.Queue[string], w http.ResponseWriter, flusher http.Flusher) {
	sub := queue.Subscribe(ctx, sessionID)
	for {
		select {
		case event := <-sub:
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

func subscribeToNotifications[T any](ctx context.Context, sessionID string, queue queues.Queue[T], publishQueue chan string) {
	notifications := queue.Subscribe(ctx, sessionID)
	log.Printf("subscribeToNotifications: %s", sessionID)
	for {
		select {
		case notification := <-notifications:
			log.Printf("notification: %v", notification)
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
