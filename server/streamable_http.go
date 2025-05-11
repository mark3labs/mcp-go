package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
)

// streamableHTTPSession represents an active Streamable HTTP connection.
type streamableHTTPSession struct {
	sessionID           string
	notificationChannel chan mcp.JSONRPCNotification
	initialized         atomic.Bool
	eventStore          EventStore
	sessionTools        sync.Map // Maps tool name to ServerTool

	// For handling notifications during request processing
	notificationHandler func(mcp.JSONRPCNotification)
	notifyMu            sync.RWMutex
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
	s.sessionTools.Range(func(k, _ interface{}) bool {
		s.sessionTools.Delete(k)
		return true
	})

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

// WithSessionIDGenerator sets a custom session ID generator
func WithSessionIDGenerator(generator func() string) StreamableHTTPOption {
	return streamableHTTPOption(func(s *StreamableHTTPServer) {
		s.sessionIDGenerator = generator
	})
}

// WithStatelessMode enables stateless mode (no sessions)
func WithStatelessMode(enable bool) StreamableHTTPOption {
	return streamableHTTPOption(func(s *StreamableHTTPServer) {
		s.statelessMode = enable
	})
}

// WithEnableJSONResponse enables direct JSON responses instead of SSE streams
func WithEnableJSONResponse(enable bool) StreamableHTTPOption {
	return streamableHTTPOption(func(s *StreamableHTTPServer) {
		s.enableJSONResponse = enable
	})
}

// WithEventStore sets a custom event store for resumability
func WithEventStore(store EventStore) StreamableHTTPOption {
	return streamableHTTPOption(func(s *StreamableHTTPServer) {
		s.eventStore = store
	})
}

// WithStreamableHTTPContextFunc sets a function that will be called to customize the context
// to the server using the incoming request.
func WithStreamableHTTPContextFunc(fn HTTPContextFunc) StreamableHTTPOption {
	return streamableHTTPOption(func(s *StreamableHTTPServer) {
		s.contextFunc = fn
	})
}

// WithOriginAllowlist sets the allowed origins for CORS validation
func WithOriginAllowlist(allowlist []string) StreamableHTTPOption {
	return streamableHTTPOption(func(s *StreamableHTTPServer) {
		s.originAllowlist = allowlist
	})
}

// realStreamableHTTPServer is the concrete implementation of StreamableHTTPServer.
// It provides HTTP transport capabilities following the MCP Streamable HTTP specification.
type StreamableHTTPServer struct {
	// Implement the httpTransportConfigurable interface
	server             *MCPServer
	baseURL            string
	basePath           string
	endpoint           string
	sessions           sync.Map // Maps sessionID to ClientSession
	srv                *http.Server
	contextFunc        HTTPContextFunc
	sessionIDGenerator func() string
	enableJSONResponse bool
	eventStore         EventStore
	streamMapping      sync.Map // Maps streamID to response writer
	requestToStreamMap sync.Map // Maps requestID to streamID
	statelessMode      bool
	originAllowlist    []string // List of allowed origins for CORS validation
}

// NewStreamableHTTPServer creates a new Streamable HTTP server instance with the given MCP server and options.
func NewStreamableHTTPServer(server *MCPServer, opts ...StreamableHTTPOption) *StreamableHTTPServer {
	// Create our implementation
	s := &StreamableHTTPServer{
		server:             server,
		endpoint:           "/mcp",
		sessionIDGenerator: func() string { return uuid.New().String() },
		enableJSONResponse: false,
		originAllowlist:    []string{}, // Initialize empty allowlist
	}

	// Apply all options
	for _, opt := range opts {
		opt.applyToStreamableHTTP(s)
	}

	// If no event store is provided, create an in-memory one
	if s.eventStore == nil {
		s.eventStore = NewInMemoryEventStore()
	}

	// Return the stub
	return s
}

// Start begins serving Streamable HTTP connections on the specified address.
// It sets up HTTP handlers for the MCP endpoint.
func (s *StreamableHTTPServer) Start(addr string) error {
	s.srv = &http.Server{
		Addr:    addr,
		Handler: s,
	}

	err := s.srv.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
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

	// Validate Origin header if present (MUST requirement from spec)
	origin := r.Header.Get("Origin")
	if origin != "" {
		// Simple validation - in production you might want more sophisticated checks
		if !s.isValidOrigin(origin) {
			http.Error(w, "Invalid origin", http.StatusForbidden)
			return
		}
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

	// Create a buffer for notifications sent during request processing
	var notificationBuffer []mcp.JSONRPCNotification
	var originalNotificationHandler func(mcp.JSONRPCNotification)

	// Set up temporary notification handler if we have a session
	if session != nil {
		// Store the original notification handler if any
		originalNotificationHandler = nil
		session.notifyMu.RLock()
		if session.notificationHandler != nil {
			originalNotificationHandler = session.notificationHandler
		}
		session.notifyMu.RUnlock()

		// Set a temporary handler to buffer notifications
		session.notifyMu.Lock()
		session.notificationHandler = func(notification mcp.JSONRPCNotification) {
			notificationBuffer = append(notificationBuffer, notification)
			// Also forward to original handler if it exists
			if originalNotificationHandler != nil {
				originalNotificationHandler(notification)
			}
		}
		session.notifyMu.Unlock()
	}

	// Process the request
	response := s.server.HandleMessage(ctx, rawMessage)

	// Restore the original notification handler
	if session != nil && originalNotificationHandler != nil {
		session.notifyMu.Lock()
		session.notificationHandler = originalNotificationHandler
		session.notifyMu.Unlock()
	}

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

			// Start a goroutine to listen for notifications and call the notification handler
			go func() {
				for notification := range newSession.notificationChannel {
					// Call the notification handler if set
					newSession.notifyMu.RLock()
					handler := newSession.notificationHandler
					newSession.notifyMu.RUnlock()

					if handler != nil {
						handler(notification)
					}
				}
			}()

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
		s.handleSSEResponse(w, r, ctx, response, session, notificationBuffer...)
	} else {
		// Send a direct JSON response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if response != nil {
			json.NewEncoder(w).Encode(response)
		}
	}
}

func (s *StreamableHTTPServer) handleSSEResponse(w http.ResponseWriter, r *http.Request, ctx context.Context, initialResponse mcp.JSONRPCMessage, session SessionWithTools, notificationBuffer ...mcp.JSONRPCNotification) {
	// Set up the stream
	streamID, err := s.setupStream(w)
	if err != nil {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}
	defer s.closeStream(streamID)

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
			// Use the event ID from the store
			if err := s.writeSSEEvent(streamID, "", message); err != nil {
				return err
			}
			return nil
		})

		if err != nil {
			// Log the error but continue
			fmt.Printf("Error replaying events: %v\n", err)
		}
	}

	// Send any buffered notifications first
	for _, notification := range notificationBuffer {
		// Store the event in the event store if available
		if httpSession != nil && httpSession.eventStore != nil {
			_, err := httpSession.eventStore.StoreEvent(streamID, notification)
			if err != nil {
				// Log the error but continue
				fmt.Printf("Error storing event: %v\n", err)
			}
		}

		// Send the notification
		if err := s.writeSSEEvent(streamID, "", notification); err != nil {
			fmt.Printf("Error writing notification: %v\n", err)
		}
	}

	// Send the initial response if there is one
	if initialResponse != nil {
		// Store the event in the event store if available
		if httpSession != nil && httpSession.eventStore != nil {
			_, err := httpSession.eventStore.StoreEvent(streamID, initialResponse)
			if err != nil {
				// Log the error but continue
				fmt.Printf("Error storing event: %v\n", err)
			}
		}

		// Send the response
		if err := s.writeSSEEvent(streamID, "", initialResponse); err != nil {
			fmt.Printf("Error writing response: %v\n", err)
		}

		// According to the MCP specification, the server SHOULD close the SSE stream
		// after all JSON-RPC responses have been sent.
		// Since we've sent the response, we can close the stream now.
		return
	}

	// If there's no response (which shouldn't happen in normal operation),
	// we'll keep the stream open for a short time to handle any notifications
	// that might come in, then close it.

	// Create a channel to pass notifications from the goroutine to the main handler
	notificationCh := make(chan mcp.JSONRPCNotification, 100) // Buffer size to prevent blocking
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

				// Send the notification to the main handler goroutine via channel
				select {
				case notificationCh <- notification:
				case <-notifDone:
					return
				}
			case <-notifDone:
				return
			}
		}
	}()

	// Create a context with cancellation and a timeout
	// We'll only keep the stream open for a short time if there's no response
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Process notifications in the main handler goroutine
	for {
		select {
		case notification := <-notificationCh:
			// Store the event in the event store if available
			if httpSession != nil && httpSession.eventStore != nil {
				_, err := httpSession.eventStore.StoreEvent(streamID, notification)
				if err != nil {
					// Log the error but continue
					fmt.Printf("Error storing event: %v\n", err)
				}
			}

			// Send the notification
			if err := s.writeSSEEvent(streamID, "", notification); err != nil {
				fmt.Printf("Error writing notification: %v\n", err)
			}
		case <-ctx.Done():
			// Request context is done or timeout reached, exit the loop
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

	// Check if the session exists using validateSession
	if !s.validateSession(sessionID) {
		http.Error(w, "Session not found or not initialized", http.StatusNotFound)
		return
	}

	// Get the session
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

	// Set up the stream
	streamID, err := s.setupStream(w)
	if err != nil {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}
	defer s.closeStream(streamID)

	// Send an initial event to confirm the connection is established
	initialNotification := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "connection/established",
		"params":  nil,
	}
	if err := s.writeSSEEvent(streamID, "", initialNotification); err != nil {
		fmt.Printf("Error writing initial notification: %v\n", err)
		return
	}

	// Create a channel to pass notifications from the goroutine to the main handler
	notificationCh := make(chan mcp.JSONRPCNotification, 100) // Buffer size to prevent blocking
	notifDone := make(chan struct{})
	defer close(notifDone)

	// Start a goroutine to listen for notifications and send them to the notification channel
	go func() {
		for {
			select {
			case notification, ok := <-session.notificationChannel:
				if !ok {
					return
				}

				// Send the notification to the main handler goroutine via channel
				select {
				case notificationCh <- notification:
				case <-notifDone:
					return
				}
			case <-notifDone:
				return
			}
		}
	}()

	// Create a context with cancellation
	// For standalone SSE streams, we'll keep the connection open until the client disconnects
	ctx := r.Context()

	// Process notifications in the main handler goroutine
	for {
		select {
		case notification := <-notificationCh:
			// Send the notification
			if err := s.writeSSEEvent(streamID, "", notification); err != nil {
				fmt.Printf("Error writing notification: %v\n", err)
			}
		case <-ctx.Done():
			// Request context is done, exit the loop
			return
		}
	}
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

// streamInfo holds information about an active SSE stream
type streamInfo struct {
	writer  http.ResponseWriter
	flusher http.Flusher
	eventID int
	mu      sync.Mutex // For thread-safe event ID updates
}

// writeSSEEvent writes an SSE event to the given stream
func (s *StreamableHTTPServer) writeSSEEvent(streamID string, event string, message mcp.JSONRPCMessage) error {
	// Get the stream info
	streamInfoI, ok := s.streamMapping.Load(streamID)
	if !ok {
		return fmt.Errorf("stream not found: %s", streamID)
	}

	streamInfo, ok := streamInfoI.(*streamInfo)
	if !ok {
		return fmt.Errorf("invalid stream info type")
	}

	// Lock for thread-safe event ID update
	streamInfo.mu.Lock()
	defer streamInfo.mu.Unlock()

	// Marshal the message
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}

	// Write the event to the response
	if event != "" {
		fmt.Fprintf(streamInfo.writer, "event: %s\n", event)
	}
	fmt.Fprintf(streamInfo.writer, "id: %d\ndata: %s\n\n", streamInfo.eventID, data)
	streamInfo.flusher.Flush()

	// Increment the event ID
	streamInfo.eventID++

	return nil
}

// setupStream creates a new SSE stream and returns its ID
func (s *StreamableHTTPServer) setupStream(w http.ResponseWriter) (string, error) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// Create a unique stream ID
	streamID := uuid.New().String()

	// Get the flusher
	flusher, ok := w.(http.Flusher)
	if !ok {
		return "", fmt.Errorf("streaming not supported")
	}

	// Store the stream info
	s.streamMapping.Store(streamID, &streamInfo{
		writer:  w,
		flusher: flusher,
		eventID: 0,
	})

	return streamID, nil
}

// closeStream removes a stream from the mapping
func (s *StreamableHTTPServer) closeStream(streamID string) {
	s.streamMapping.Delete(streamID)
}

// BroadcastNotification sends a notification to all active streams
func (s *StreamableHTTPServer) BroadcastNotification(notification mcp.JSONRPCNotification) {
	s.streamMapping.Range(func(key, value interface{}) bool {
		streamID := key.(string)
		s.writeSSEEvent(streamID, "", notification)
		return true
	})
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
	// Create the server
	base := NewStreamableHTTPServer(server, opts...)

	// Create the test server
	testServer := httptest.NewServer(base)

	// Set the base URL
	base.baseURL = testServer.URL

	return testServer
}

// isValidOrigin validates the Origin header to prevent DNS rebinding attacks
func (s *StreamableHTTPServer) isValidOrigin(origin string) bool {
	// Basic validation - parse URL and check scheme
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}

	// For local development, allow localhost
	if strings.HasPrefix(u.Host, "localhost:") || u.Host == "localhost" || u.Host == "127.0.0.1" {
		return true
	}

	// Check against allowlist if configured
	if len(s.originAllowlist) > 0 {
		for _, allowed := range s.originAllowlist {
			// Exact match
			if allowed == origin {
				return true
			}

			// Wildcard subdomain match (e.g., *.example.com)
			if strings.HasPrefix(allowed, "*.") {
				domain := allowed[2:] // Remove the "*." prefix
				if strings.HasSuffix(u.Host, domain) {
					// Check that it's a proper subdomain
					hostWithoutDomain := strings.TrimSuffix(u.Host, domain)
					if hostWithoutDomain != "" && strings.HasSuffix(hostWithoutDomain, ".") {
						return true
					}
				}
			}
		}

		// If we have an allowlist and the origin isn't in it, reject
		return false
	}

	// If no allowlist is configured, allow all origins (backward compatibility)
	// In production, you should always configure an allowlist
	return true
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
