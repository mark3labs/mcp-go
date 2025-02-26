package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
)

// SSEContextFunc is a function that takes an existing context and the current
// request and returns a potentially modified context based on the request
// content. This can be used to inject context values from headers, for example.
type SSEContextFunc func(ctx context.Context, r *http.Request) context.Context

// SSEServer implements a Server-Sent Events (SSE) based MCP server.
// It provides real-time communication capabilities over HTTP using the SSE protocol.
type SSEServer struct {
	server      *MCPServer
	baseURL     string
	sessions    sync.Map
	srv         *http.Server
	contextFunc SSEContextFunc
}

// sseSession represents an active SSE connection.
type sseSession struct {
	writer  http.ResponseWriter
	flusher http.Flusher
	done    chan struct{}
}

// NewSSEServer creates a new SSE server instance with the given MCP server and base URL.
func NewSSEServer(server *MCPServer, baseURL string) *SSEServer {
	return &SSEServer{
		server:  server,
		baseURL: baseURL,
	}
}

// SetContextFunc sets a function that will be called to customise the context
// to the server using the incoming request.
func (s *SSEServer) SetContextFunc(fn SSEContextFunc) {
	s.contextFunc = fn
}

// NewTestServer creates a test server for testing purposes
func NewTestServer(server *MCPServer) *httptest.Server {
	sseServer := &SSEServer{
		server: server,
	}

	testServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/sse":
				sseServer.handleSSE(w, r)
			case "/message":
				sseServer.handleMessage(w, r)
			default:
				http.NotFound(w, r)
			}
		}),
	)

	sseServer.baseURL = testServer.URL
	return testServer
}

// Start begins serving SSE connections on the specified address.
// It sets up HTTP handlers for SSE and message endpoints.
func (s *SSEServer) Start(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", s.handleSSE)
	mux.HandleFunc("/message", s.handleMessage)

	s.srv = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return s.srv.ListenAndServe()
}

// Shutdown gracefully stops the SSE server, closing all active sessions
// and shutting down the HTTP server.
func (s *SSEServer) Shutdown(ctx context.Context) error {
	if s.srv != nil {
		s.sessions.Range(func(key, value interface{}) bool {
			if session, ok := value.(*sseSession); ok {
				close(session.done)
			}
			s.sessions.Delete(key)
			return true
		})

		return s.srv.Shutdown(ctx)
	}
	return nil
}

// handleSSE handles incoming SSE connection requests.
// It sets up appropriate headers and creates a new session for the client.
func (s *SSEServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	sessionID := uuid.New().String()
	session := &sseSession{
		writer:  w,
		flusher: flusher,
		done:    make(chan struct{}),
	}

	s.sessions.Store(sessionID, session)
	defer s.sessions.Delete(sessionID)

	// Start notification handler for this session
	go func() {
		for {
			select {
			case serverNotification := <-s.server.notifications:
				// Only forward notifications meant for this session
				if serverNotification.Context.SessionID == sessionID {
					s.SendEventToSession(
						sessionID,
						serverNotification.Notification,
					)
				}
			case <-session.done:
				return
			case <-r.Context().Done():
				return
			}
		}
	}()

	messageEndpoint := fmt.Sprintf(
		"%s/message?sessionId=%s",
		s.baseURL,
		sessionID,
	)
	fmt.Fprintf(w, "event: endpoint\ndata: %s\r\n\r\n", messageEndpoint)
	flusher.Flush()

	<-r.Context().Done()
	close(session.done)
}

// handleMessage processes incoming JSON-RPC messages from clients and sends responses
// back through both the SSE connection and HTTP response.
func (s *SSEServer) handleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONRPCError(w, nil, mcp.INVALID_REQUEST, "Method not allowed")
		return
	}

	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		s.writeJSONRPCError(w, nil, mcp.INVALID_PARAMS, "Missing sessionId")
		return
	}

	// Set the client context in the server before handling the message
	ctx := s.server.WithContext(r.Context(), NotificationContext{
		ClientID:  sessionID,
		SessionID: sessionID,
	})

	if s.contextFunc != nil {
		ctx = s.contextFunc(ctx, r)
	}

	sessionI, ok := s.sessions.Load(sessionID)
	if !ok {
		s.writeJSONRPCError(w, nil, mcp.INVALID_PARAMS, "Invalid session ID")
		return
	}
	session := sessionI.(*sseSession)

	// Parse message as raw JSON
	var rawMessage json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawMessage); err != nil {
		s.writeJSONRPCError(w, nil, mcp.PARSE_ERROR, "Parse error")
		return
	}

	// Process message through MCPServer
	response := s.server.HandleMessage(ctx, rawMessage)

	// Only send response if there is one (not for notifications)
	if response != nil {
		eventData, _ := json.Marshal(response)
		fmt.Fprintf(session.writer, "event: message\ndata: %s\n\n", eventData)
		session.flusher.Flush()

		// Send HTTP response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(response)
	} else {
		// For notifications, just send 202 Accepted with no body
		w.WriteHeader(http.StatusAccepted)
	}
}

// writeJSONRPCError writes a JSON-RPC error response with the given error details.
func (s *SSEServer) writeJSONRPCError(
	w http.ResponseWriter,
	id interface{},
	code int,
	message string,
) {
	response := createErrorResponse(id, code, message)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(response)
}

// SendEventToSession sends an event to a specific SSE session identified by sessionID.
// Returns an error if the session is not found or closed.
func (s *SSEServer) SendEventToSession(
	sessionID string,
	event interface{},
) error {
	sessionI, ok := s.sessions.Load(sessionID)
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	session := sessionI.(*sseSession)

	eventData, err := json.Marshal(event)
	if err != nil {
		return err
	}

	select {
	case <-session.done:
		return fmt.Errorf("session closed")
	default:
		fmt.Fprintf(session.writer, "event: message\ndata: %s\n\n", eventData)
		session.flusher.Flush()
		return nil
	}
}
