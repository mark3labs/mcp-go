package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
)

// streamableHTTPSession represents an active Streamable HTTP connection.
type streamableHTTPSession struct {
	sessionID           string
	notificationChannel chan mcp.JSONRPCNotification
	initialized         atomic.Bool
	lastEventID         string
	eventStore          EventStore
	sessionTools        sync.Map // Maps tool name to ServerTool
}

func (s *streamableHTTPSession) SessionID() string {
	return s.sessionID
}

func (s *streamableHTTPSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return s.notificationChannel
}

func (s *streamableHTTPSession) Initialize() {
	s.initialized.Store(true)
}

func (s *streamableHTTPSession) Initialized() bool {
	return s.initialized.Load()
}

// GetSessionTools returns the tools specific to this session
func (s *streamableHTTPSession) GetSessionTools() map[string]ServerTool {
	tools := make(map[string]ServerTool)
	s.sessionTools.Range(func(key, value interface{}) bool {
		if toolName, ok := key.(string); ok {
			if tool, ok := value.(ServerTool); ok {
				tools[toolName] = tool
			}
		}
		return true
	})
	return tools
}

// SetSessionTools sets tools specific to this session
func (s *streamableHTTPSession) SetSessionTools(tools map[string]ServerTool) {
	// Clear existing tools
	s.sessionTools = sync.Map{}

	// Add new tools
	for name, tool := range tools {
		s.sessionTools.Store(name, tool)
	}
}

// Ensure streamableHTTPSession implements both ClientSession and SessionWithTools interfaces
var _ ClientSession = (*streamableHTTPSession)(nil)
var _ SessionWithTools = (*streamableHTTPSession)(nil)

// EventStore is an interface for storing and retrieving events for resumability
type EventStore interface {
	// StoreEvent stores an event and returns its ID
	StoreEvent(streamID string, message mcp.JSONRPCMessage) (string, error)
	// ReplayEventsAfter replays events that occurred after the given event ID
	ReplayEventsAfter(lastEventID string, send func(eventID string, message mcp.JSONRPCMessage) error) error
}

// InMemoryEventStore is a simple in-memory implementation of EventStore
type InMemoryEventStore struct {
	mu     sync.RWMutex
	events map[string][]storedEvent
}

type storedEvent struct {
	id      string
	message mcp.JSONRPCMessage
}

// NewInMemoryEventStore creates a new in-memory event store
func NewInMemoryEventStore() *InMemoryEventStore {
	return &InMemoryEventStore{
		events: make(map[string][]storedEvent),
	}
}

// StoreEvent stores an event in memory
func (s *InMemoryEventStore) StoreEvent(streamID string, message mcp.JSONRPCMessage) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	eventID := uuid.New().String()
	event := storedEvent{
		id:      eventID,
		message: message,
	}

	if _, ok := s.events[streamID]; !ok {
		s.events[streamID] = []storedEvent{}
	}
	s.events[streamID] = append(s.events[streamID], event)

	return eventID, nil
}

// ReplayEventsAfter replays events that occurred after the given event ID
func (s *InMemoryEventStore) ReplayEventsAfter(lastEventID string, send func(eventID string, message mcp.JSONRPCMessage) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if lastEventID == "" {
		return nil
	}

	// Find the stream that contains the event
	var streamEvents []storedEvent
	var found bool
	var _ string // streamID, used for debugging if needed

	for sid, events := range s.events {
		for _, event := range events {
			if event.id == lastEventID {
				streamEvents = events
				_ = sid // store for debugging if needed
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	if !found {
		return fmt.Errorf("event ID not found: %s", lastEventID)
	}

	// Find the index of the last event
	lastIdx := -1
	for i, event := range streamEvents {
		if event.id == lastEventID {
			lastIdx = i
			break
		}
	}

	// Replay events after the last event
	for i := lastIdx + 1; i < len(streamEvents); i++ {
		if err := send(streamEvents[i].id, streamEvents[i].message); err != nil {
			return err
		}
	}

	return nil
}

// StreamableHTTPOption defines a function type for configuring StreamableHTTPServer
type StreamableHTTPOption func(*StreamableHTTPServer)

// WithSessionIDGenerator sets a custom session ID generator
func WithSessionIDGenerator(generator func() string) StreamableHTTPOption {
	return func(s *StreamableHTTPServer) {
		s.sessionIDGenerator = generator
	}
}

// WithStatelessMode enables stateless mode (no sessions)
func WithStatelessMode(enable bool) StreamableHTTPOption {
	return func(s *StreamableHTTPServer) {
		s.statelessMode = enable
	}
}

// WithEnableJSONResponse enables direct JSON responses instead of SSE streams
func WithEnableJSONResponse(enable bool) StreamableHTTPOption {
	return func(s *StreamableHTTPServer) {
		s.enableJSONResponse = enable
	}
}

// WithEventStore sets a custom event store for resumability
func WithEventStore(store EventStore) StreamableHTTPOption {
	return func(s *StreamableHTTPServer) {
		s.eventStore = store
	}
}

// WithStreamableHTTPContextFunc sets a function that will be called to customize the context
// to the server using the incoming request.
func WithStreamableHTTPContextFunc(fn SSEContextFunc) StreamableHTTPOption {
	return func(s *StreamableHTTPServer) {
		s.contextFunc = fn
	}
}

// StreamableHTTPServer implements a Streamable HTTP based MCP server.
// It provides HTTP transport capabilities following the MCP Streamable HTTP specification.
type StreamableHTTPServer struct {
	server             *MCPServer
	baseURL            string
	basePath           string
	endpoint           string
	sessions           sync.Map // Maps sessionID to ClientSession
	srv                *http.Server
	contextFunc        SSEContextFunc
	sessionIDGenerator func() string
	enableJSONResponse bool
	eventStore         EventStore
	standaloneStreamID string
	streamMapping      sync.Map // Maps streamID to response writer
	requestToStreamMap sync.Map // Maps requestID to streamID
	statelessMode      bool
}

// NewStreamableHTTPServer creates a new Streamable HTTP server instance with the given MCP server and options.
func NewStreamableHTTPServer(server *MCPServer, opts ...StreamableHTTPOption) *StreamableHTTPServer {
	s := &StreamableHTTPServer{
		server:             server,
		endpoint:           "/mcp",
		sessionIDGenerator: func() string { return uuid.New().String() },
		enableJSONResponse: false,
		standaloneStreamID: "_GET_stream",
	}

	// Apply all options
	for _, opt := range opts {
		opt(s)
	}

	// If no event store is provided, create an in-memory one
	if s.eventStore == nil {
		s.eventStore = NewInMemoryEventStore()
	}

	return s
}

// Start begins serving Streamable HTTP connections on the specified address.
// It sets up HTTP handlers for the MCP endpoint.
func (s *StreamableHTTPServer) Start(addr string) error {
	s.srv = &http.Server{
		Addr:    addr,
		Handler: s,
	}

	return s.srv.ListenAndServe()
}

// Shutdown gracefully stops the Streamable HTTP server, closing all active sessions
// and shutting down the HTTP server.
func (s *StreamableHTTPServer) Shutdown(ctx context.Context) error {
	if s.srv != nil {
		s.sessions.Range(func(key, value interface{}) bool {
			if session, ok := value.(ClientSession); ok {
				if httpSession, ok := session.(*streamableHTTPSession); ok {
					close(httpSession.notificationChannel)
				}
			}
			s.sessions.Delete(key)
			return true
		})

		return s.srv.Shutdown(ctx)
	}
	return nil
}

// ServeHTTP implements the http.Handler interface.
func (s *StreamableHTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	endpoint := s.basePath + s.endpoint

	if path != endpoint {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodPost:
		s.handlePost(w, r)
	case http.MethodGet:
		s.handleGet(w, r)
	case http.MethodDelete:
		s.handleDelete(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePost processes POST requests to the MCP endpoint
func (s *StreamableHTTPServer) handlePost(w http.ResponseWriter, r *http.Request) {
	// Get session ID from header if present
	sessionID := r.Header.Get("Mcp-Session-Id")
	var session *streamableHTTPSession

	// Check if this is a request with a valid session
	if sessionID != "" {
		if sessionValue, ok := s.sessions.Load(sessionID); ok {
			if sess, ok := sessionValue.(SessionWithTools); ok {
				session = sess.(*streamableHTTPSession)
			} else {
				http.Error(w, "Invalid session", http.StatusBadRequest)
				return
			}
		} else {
			// Session not found
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}
	}

	// Parse the request body
	var rawMessage json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawMessage); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Parse the base message to determine if it's a request or notification
	var baseMessage struct {
		JSONRPC string      `json:"jsonrpc"`
		Method  string      `json:"method"`
		ID      interface{} `json:"id,omitempty"`
	}
	if err := json.Unmarshal(rawMessage, &baseMessage); err != nil {
		http.Error(w, "Invalid JSON-RPC message", http.StatusBadRequest)
		return
	}

	// Create context for the request
	ctx := r.Context()
	if session != nil {
		ctx = s.server.WithContext(ctx, session)
	}
	if s.contextFunc != nil {
		ctx = s.contextFunc(ctx, r)
	}

	// Handle the message based on whether it's a request or notification
	if baseMessage.ID == nil {
		// It's a notification
		s.handleNotification(w, ctx, rawMessage)
	} else {
		// It's a request
		s.handleRequest(w, r, ctx, rawMessage, session)
	}
}

// handleNotification processes JSON-RPC notifications
func (s *StreamableHTTPServer) handleNotification(w http.ResponseWriter, ctx context.Context, rawMessage json.RawMessage) {
	// Process the notification
	s.server.HandleMessage(ctx, rawMessage)

	// Return 202 Accepted for notifications
	w.WriteHeader(http.StatusAccepted)
}

// handleRequest processes JSON-RPC requests
func (s *StreamableHTTPServer) handleRequest(w http.ResponseWriter, r *http.Request, ctx context.Context, rawMessage json.RawMessage, session *streamableHTTPSession) {
	// Parse the request to get the method and ID
	var request struct {
		JSONRPC string      `json:"jsonrpc"`
		Method  string      `json:"method"`
		ID      interface{} `json:"id"`
	}
	if err := json.Unmarshal(rawMessage, &request); err != nil {
		http.Error(w, "Invalid JSON-RPC request", http.StatusBadRequest)
		return
	}

	// Check if this is an initialization request
	isInitialize := request.Method == "initialize"

	// If this is not an initialization request and we don't have a session,
	// and we're not in stateless mode, then reject the request
	if !isInitialize && session == nil && !s.statelessMode {
		http.Error(w, "Bad Request: Server not initialized", http.StatusBadRequest)
		return
	}

	// Process the request
	response := s.server.HandleMessage(ctx, rawMessage)

	// If this is an initialization request, create a new session
	if isInitialize && response != nil {
		// Only create a session if we're not in stateless mode
		if !s.statelessMode {
			newSessionID := s.sessionIDGenerator()
			newSession := &streamableHTTPSession{
				sessionID:           newSessionID,
				notificationChannel: make(chan mcp.JSONRPCNotification, 100),
				eventStore:          s.eventStore,
				sessionTools:        sync.Map{},
			}

			// Initialize and register the session
			newSession.Initialize()
			s.sessions.Store(newSessionID, newSession)
			if err := s.server.RegisterSession(ctx, newSession); err != nil {
				http.Error(w, fmt.Sprintf("Failed to register session: %v", err), http.StatusInternalServerError)
				return
			}

			// Set the session ID in the response header
			w.Header().Set("Mcp-Session-Id", newSessionID)

			// Update the session reference for further processing
			session = newSession
		}
	}

	// Check if the client accepts SSE
	acceptHeader := r.Header.Get("Accept")
	acceptsSSE := false
	for _, accept := range splitHeader(acceptHeader) {
		if strings.HasPrefix(accept, "text/event-stream") {
			acceptsSSE = true
			break
		}
	}

	// Determine if we should use SSE or direct JSON response
	useSSE := false

	// If the request contains any requests (not just notifications), we might use SSE
	if request.ID != nil {
		// Use SSE if:
		// 1. The client accepts SSE
		// 2. We have a valid session
		// 3. JSON response is not explicitly enabled
		// 4. The request is not an initialization request (those always return JSON)
		if acceptsSSE && session != nil && !s.enableJSONResponse && !isInitialize {
			useSSE = true
		}
	}

	if useSSE {
		// Start an SSE stream for this request
		s.handleSSEResponse(w, r, ctx, response, session)
	} else {
		// Send a direct JSON response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if response != nil {
			json.NewEncoder(w).Encode(response)
		}
	}
}
func (s *StreamableHTTPServer) handleSSEResponse(w http.ResponseWriter, r *http.Request, ctx context.Context, initialResponse mcp.JSONRPCMessage, session SessionWithTools) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// Create a unique stream ID for this request
	streamID := uuid.New().String()

	// Get the request ID from the initial response
	var requestID interface{}
	if resp, ok := initialResponse.(mcp.JSONRPCResponse); ok {
		requestID = resp.ID
	} else if errResp, ok := initialResponse.(mcp.JSONRPCError); ok {
		requestID = errResp.ID
	}

	// If we have a request ID, map it to this stream
	if requestID != nil {
		s.requestToStreamMap.Store(requestID, streamID)
		defer s.requestToStreamMap.Delete(requestID)
	}

	// Check for Last-Event-ID header for resumability
	lastEventID := r.Header.Get("Last-Event-Id")
	httpSession, ok := session.(*streamableHTTPSession)
	if lastEventID != "" && ok && httpSession.eventStore != nil {
		// Replay events that occurred after the last event ID
		err := httpSession.eventStore.ReplayEventsAfter(lastEventID, func(eventID string, message mcp.JSONRPCMessage) error {
			data, err := json.Marshal(message)
			if err != nil {
				return err
			}

			// Write the event directly to the response writer
			fmt.Fprintf(w, "id: %s\ndata: %s\n\n", eventID, data)
			w.(http.Flusher).Flush()
			return nil
		})

		if err != nil {
			// Log the error but continue
			fmt.Printf("Error replaying events: %v\n", err)
		}
	}

	// Send the initial response if there is one
	if initialResponse != nil {
		data, err := json.Marshal(initialResponse)
		if err != nil {
			http.Error(w, "Failed to marshal response", http.StatusInternalServerError)
			return
		}

		// Store the event if we have an event store
		var eventID string
		if httpSession != nil && httpSession.eventStore != nil {
			var storeErr error
			eventID, storeErr = httpSession.eventStore.StoreEvent(streamID, initialResponse)
			if storeErr != nil {
				// Log the error but continue
				fmt.Printf("Error storing event: %v\n", storeErr)
			}
		}

		// Write the event directly to the response writer
		if eventID != "" {
			fmt.Fprintf(w, "id: %s\ndata: %s\n\n", eventID, data)
		} else {
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		w.(http.Flusher).Flush()
	}

	// Create a channel to pass notifications from the goroutine to the main handler
	notificationCh := make(chan struct {
		eventID string
		data    []byte
	}, 100) // Buffer size to prevent blocking
	notifDone := make(chan struct{})
	defer close(notifDone)

	// Start a goroutine to listen for notifications and send them to the notification channel
	go func() {
		for {
			select {
			case notification, ok := <-httpSession.notificationChannel:
				if !ok {
					return
				}

				data, err := json.Marshal(notification)
				if err != nil {
					continue
				}

				// Store the event if we have an event store
				var eventID string
				if httpSession != nil && httpSession.eventStore != nil {
					var storeErr error
					eventID, storeErr = httpSession.eventStore.StoreEvent(streamID, notification)
					if storeErr != nil {
						// Log the error but continue
						fmt.Printf("Error storing event: %v\n", storeErr)
					}
				}

				// Send the notification to the main handler goroutine via channel
				select {
				case notificationCh <- struct {
					eventID string
					data    []byte
				}{eventID: eventID, data: data}:
				case <-notifDone:
					return
				}
			case <-notifDone:
				return
			}
		}
	}()

	// Create a context with cancellation
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Process notifications in the main handler goroutine
	for {
		select {
		case notification := <-notificationCh:
			// Write the event directly to the response writer from the main handler goroutine
			if notification.eventID != "" {
				fmt.Fprintf(w, "id: %s\ndata: %s\n\n", notification.eventID, notification.data)
			} else {
				fmt.Fprintf(w, "data: %s\n\n", notification.data)
			}
			w.(http.Flusher).Flush()
		case <-ctx.Done():
			// Request context is done, exit the loop
			return
		}
	}
}

// handleGet processes GET requests to the MCP endpoint (for standalone SSE streams)
func (s *StreamableHTTPServer) handleGet(w http.ResponseWriter, r *http.Request) {
	// Check if the client accepts SSE
	acceptHeader := r.Header.Get("Accept")
	acceptsSSE := false
	for _, accept := range splitHeader(acceptHeader) {
		if strings.HasPrefix(accept, "text/event-stream") {
			acceptsSSE = true
			break
		}
	}

	if !acceptsSSE {
		http.Error(w, "Not Acceptable: Client must accept text/event-stream", http.StatusNotAcceptable)
		return
	}

	// Get session ID from header if present
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		http.Error(w, "Bad Request: Mcp-Session-Id header must be provided", http.StatusBadRequest)
		return
	}

	// Check if the session exists
	sessionValue, ok := s.sessions.Load(sessionID)
	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// Get the session
	session, ok := sessionValue.(*streamableHTTPSession)
	if !ok {
		http.Error(w, "Invalid session type", http.StatusInternalServerError)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// Generate a unique ID for this stream
	s.standaloneStreamID = uuid.New().String()

	// Send an initial event to confirm the connection is established
	initialEvent := fmt.Sprintf("data: {\"jsonrpc\": \"2.0\", \"method\": \"connection/established\"}\n\n")
	if _, err := fmt.Fprint(w, initialEvent); err != nil {
		return
	}
	// Ensure the event is sent immediately
	w.(http.Flusher).Flush()

	// Start a goroutine to listen for notifications and forward them to the client
	notifDone := make(chan struct{})
	defer close(notifDone)

	go func() {
		for {
			select {
			case notification, ok := <-session.notificationChannel:
				if !ok {
					return
				}

				data, err := json.Marshal(notification)
				if err != nil {
					continue
				}

				// Make sure the notification is properly formatted as a JSON-RPC message
				// The test expects a specific format with jsonrpc, method, and params fields
				fmt.Fprintf(w, "data: %s\n\n", data)
				w.(http.Flusher).Flush()
			case <-notifDone:
				return
			}
		}
	}()

	// Wait for the request context to be done
	<-r.Context().Done()
}

// handleDelete processes DELETE requests to the MCP endpoint (for session termination)
func (s *StreamableHTTPServer) handleDelete(w http.ResponseWriter, r *http.Request) {
	// Get session ID from header
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		http.Error(w, "Bad Request: Mcp-Session-Id header must be provided", http.StatusBadRequest)
		return
	}

	// Check if the session exists
	if _, ok := s.sessions.Load(sessionID); !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// Unregister the session
	s.server.UnregisterSession(r.Context(), sessionID)
	s.sessions.Delete(sessionID)

	// Return 200 OK
	w.WriteHeader(http.StatusOK)
}

// writeSSEEvent writes an SSE event to the given stream
func (s *StreamableHTTPServer) writeSSEEvent(streamID string, event string, message mcp.JSONRPCMessage) error {
	// Get the stream channel
	streamChanI, ok := s.streamMapping.Load(streamID)
	if !ok {
		return fmt.Errorf("stream not found: %s", streamID)
	}

	streamChan, ok := streamChanI.(chan string)
	if !ok {
		return fmt.Errorf("invalid stream channel type")
	}

	// Marshal the message
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}

	// Create the event data
	eventData := fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)

	// Send the event to the channel
	select {
	case streamChan <- eventData:
		return nil
	default:
		return fmt.Errorf("stream channel full")
	}
}

// splitHeader splits a comma-separated header value into individual values
func splitHeader(header string) []string {
	if header == "" {
		return nil
	}

	var values []string
	for _, value := range splitAndTrim(header, ',') {
		if value != "" {
			values = append(values, value)
		}
	}

	return values
}

// splitAndTrim splits a string by the given separator and trims whitespace from each part
func splitAndTrim(s string, sep rune) []string {
	var result []string
	var builder strings.Builder
	var inQuotes bool

	for _, r := range s {
		if r == '"' {
			inQuotes = !inQuotes
			builder.WriteRune(r)
		} else if r == sep && !inQuotes {
			result = append(result, strings.TrimSpace(builder.String()))
			builder.Reset()
		} else {
			builder.WriteRune(r)
		}
	}

	if builder.Len() > 0 {
		result = append(result, strings.TrimSpace(builder.String()))
	}

	return result
}

// NewTestStreamableHTTPServer creates a test server for testing purposes
func NewTestStreamableHTTPServer(server *MCPServer, opts ...StreamableHTTPOption) *httptest.Server {
	streamableServer := NewStreamableHTTPServer(server, opts...)
	testServer := httptest.NewServer(streamableServer)
	streamableServer.baseURL = testServer.URL
	return testServer
}

// validateSession checks if the session ID is valid and the session is initialized
func (s *StreamableHTTPServer) validateSession(sessionID string) bool {
	// Check if the session ID is valid
	if sessionID == "" {
		return false
	}

	// Check if the session exists
	if sessionValue, ok := s.sessions.Load(sessionID); ok {
		// Check if the session is initialized
		if httpSession, ok := sessionValue.(*streamableHTTPSession); ok {
			return httpSession.Initialized()
		}
	}

	return false
}
