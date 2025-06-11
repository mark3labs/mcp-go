package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// StdioContextFunc is a function that takes an existing context and returns
// a potentially modified context.
// This can be used to inject context values from environment variables,
// for example.
type StdioContextFunc func(ctx context.Context) context.Context

// StdioServer wraps a MCPServer and handles stdio communication.
// It provides a simple way to create command-line MCP servers that
// communicate via standard input/output streams using JSON-RPC messages.
type StdioServer struct {
	server      *MCPServer
	errLogger   *log.Logger
	contextFunc StdioContextFunc
}

// StdioOption defines a function type for configuring StdioServer
type StdioOption func(*StdioServer)

// WithErrorLogger sets the error logger for the server
func WithErrorLogger(logger *log.Logger) StdioOption {
	return func(s *StdioServer) {
		s.errLogger = logger
	}
}

// WithStdioContextFunc sets a function that will be called to customise the context
// to the server. Note that the stdio server uses the same context for all requests,
// so this function will only be called once per server instance.
func WithStdioContextFunc(fn StdioContextFunc) StdioOption {
	return func(s *StdioServer) {
		s.contextFunc = fn
	}
}

// stdioSession is a static client session, since stdio has only one client.
// Enhanced to support bidirectional requests for sampling.
type stdioSession struct {
	notifications chan mcp.JSONRPCNotification
	initialized   atomic.Bool
	loggingLevel  atomic.Value
	clientInfo    atomic.Value // stores session-specific client info
	
	// Bidirectional communication support
	stdout        io.Writer
	requestID     atomic.Int64
	responses     map[string]chan *JSONRPCResponse
	responsesMu   sync.RWMutex
}

func (s *stdioSession) SessionID() string {
	return "stdio"
}

func (s *stdioSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return s.notifications
}

func (s *stdioSession) Initialize() {
	// set default logging level
	s.loggingLevel.Store(mcp.LoggingLevelError)
	s.initialized.Store(true)
}

func (s *stdioSession) Initialized() bool {
	return s.initialized.Load()
}

func (s *stdioSession) GetClientInfo() mcp.Implementation {
	if value := s.clientInfo.Load(); value != nil {
		if clientInfo, ok := value.(mcp.Implementation); ok {
			return clientInfo
		}
	}
	return mcp.Implementation{}
}

func (s *stdioSession) SetClientInfo(clientInfo mcp.Implementation) {
	s.clientInfo.Store(clientInfo)
}

func (s *stdioSession) SetLogLevel(level mcp.LoggingLevel) {
	s.loggingLevel.Store(level)
}

func (s *stdioSession) GetLogLevel() mcp.LoggingLevel {
	level := s.loggingLevel.Load()
	if level == nil {
		return mcp.LoggingLevelError
	}
	return level.(mcp.LoggingLevel)
}

// SendRequest implements SessionWithRequests interface for bidirectional communication
func (s *stdioSession) SendRequest(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
	if s.stdout == nil {
		return nil, fmt.Errorf("stdout not available for sending requests")
	}

	// Generate unique request ID
	id := s.requestID.Add(1)
	requestID := mcp.NewRequestId(id)

	// Create JSON-RPC request
	request := struct {
		JSONRPC string        `json:"jsonrpc"`
		ID      mcp.RequestId `json:"id"`
		Method  string        `json:"method"`
		Params  any           `json:"params,omitempty"`
	}{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      requestID,
		Method:  method,
		Params:  params,
	}

	// Marshal request
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	requestBytes = append(requestBytes, '\n')

	// Create response channel
	responseChan := make(chan *JSONRPCResponse, 1)
	idKey := requestID.String()
	
	s.responsesMu.Lock()
	if s.responses == nil {
		s.responses = make(map[string]chan *JSONRPCResponse)
	}
	s.responses[idKey] = responseChan
	s.responsesMu.Unlock()

	// Cleanup function
	cleanup := func() {
		s.responsesMu.Lock()
		delete(s.responses, idKey)
		s.responsesMu.Unlock()
	}

	// Send request
	if _, err := s.stdout.Write(requestBytes); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	// Wait for response with timeout
	select {
	case <-ctx.Done():
		cleanup()
		return nil, ctx.Err()
	case response := <-responseChan:
		return response, nil
	case <-time.After(30 * time.Second):
		cleanup()
		return nil, fmt.Errorf("request timed out")
	}
}

// handleResponse processes incoming responses to server-initiated requests
func (s *stdioSession) handleResponse(response *JSONRPCResponse) {
	if response.ID.IsNil() {
		return // Not a response to our request
	}

	idKey := response.ID.String()
	
	s.responsesMu.RLock()
	ch, exists := s.responses[idKey]
	s.responsesMu.RUnlock()

	if exists {
		select {
		case ch <- response:
		default:
			// Channel is full or closed, ignore
		}
	}
}

var (
	_ ClientSession         = (*stdioSession)(nil)
	_ SessionWithLogging    = (*stdioSession)(nil)
	_ SessionWithClientInfo = (*stdioSession)(nil)
	_ SessionWithRequests   = (*stdioSession)(nil)
)

var stdioSessionInstance = stdioSession{
	notifications: make(chan mcp.JSONRPCNotification, 100),
}

// NewStdioServer creates a new stdio server wrapper around an MCPServer.
// It initializes the server with a default error logger that discards all output.
func NewStdioServer(server *MCPServer) *StdioServer {
	return &StdioServer{
		server: server,
		errLogger: log.New(
			os.Stderr,
			"",
			log.LstdFlags,
		), // Default to discarding logs
	}
}

// SetErrorLogger configures where error messages from the StdioServer are logged.
// The provided logger will receive all error messages generated during server operation.
func (s *StdioServer) SetErrorLogger(logger *log.Logger) {
	s.errLogger = logger
}

// SetContextFunc sets a function that will be called to customise the context
// to the server. Note that the stdio server uses the same context for all requests,
// so this function will only be called once per server instance.
func (s *StdioServer) SetContextFunc(fn StdioContextFunc) {
	s.contextFunc = fn
}

// handleNotifications continuously processes notifications from the session's notification channel
// and writes them to the provided output. It runs until the context is cancelled.
// Any errors encountered while writing notifications are logged but do not stop the handler.
func (s *StdioServer) handleNotifications(ctx context.Context, stdout io.Writer) {
	for {
		select {
		case notification := <-stdioSessionInstance.notifications:
			notificationBytes, err := json.Marshal(notification)
			if err != nil {
				s.errLogger.Printf("Failed to marshal notification: %v", err)
				continue
			}
			notificationBytes = append(notificationBytes, '\n')

			if _, err := stdout.Write(notificationBytes); err != nil {
				s.errLogger.Printf("Failed to write notification: %v", err)
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// handleInput processes incoming messages from stdin, handling both regular client requests
// and responses to server-initiated requests (for bidirectional communication).
func (s *StdioServer) handleInput(ctx context.Context, stdin io.Reader, stdout io.Writer) {
	scanner := bufio.NewScanner(stdin)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		// Try to determine if this is a response to a server request or a client request
		var baseMessage struct {
			JSONRPC string         `json:"jsonrpc"`
			ID      *mcp.RequestId `json:"id,omitempty"`
			Method  *string        `json:"method,omitempty"`
			Result  json.RawMessage `json:"result,omitempty"`
			Error   json.RawMessage `json:"error,omitempty"`
		}

		if err := json.Unmarshal([]byte(line), &baseMessage); err != nil {
			s.errLogger.Printf("Failed to parse message: %v", err)
			continue
		}

		// If it has an ID but no method, it's likely a response to our request
		if baseMessage.ID != nil && baseMessage.Method == nil {
			var response JSONRPCResponse
			if err := json.Unmarshal([]byte(line), &response); err == nil {
				stdioSessionInstance.handleResponse(&response)
				continue
			}
		}

		// Otherwise, treat it as a regular client request
		response := s.server.HandleMessage(ctx, json.RawMessage(line))
		responseBytes, err := json.Marshal(response)
		if err != nil {
			s.errLogger.Printf("Failed to marshal response: %v", err)
			continue
		}
		responseBytes = append(responseBytes, '\n')

		if _, err := stdout.Write(responseBytes); err != nil {
			s.errLogger.Printf("Failed to write response: %v", err)
			return
		}
	}

	if err := scanner.Err(); err != nil {
		s.errLogger.Printf("Error reading from stdin: %v", err)
	}
}

// Serve starts the stdio server and processes messages until the context is cancelled
// or a termination signal is received. It handles both incoming requests from the client
// and outgoing notifications to the client.
func (s *StdioServer) Serve(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	// Set up stdout for the session to enable bidirectional communication
	stdioSessionInstance.stdout = stdout

	// Register the session with the server
	serverCtx := s.server.WithContext(ctx, &stdioSessionInstance)
	if s.contextFunc != nil {
		serverCtx = s.contextFunc(serverCtx)
	}

	if err := s.server.RegisterSession(serverCtx, &stdioSessionInstance); err != nil {
		return fmt.Errorf("failed to register session: %w", err)
	}
	defer s.server.UnregisterSession(serverCtx, stdioSessionInstance.SessionID())

	// Start notification handler
	go s.handleNotifications(serverCtx, stdout)

	// Handle input messages
	s.handleInput(serverCtx, stdin, stdout)

	return nil
}

// ServeStdio is a convenience function that creates a stdio server and serves it
// using os.Stdin and os.Stdout. It blocks until a termination signal is received.
func ServeStdio(server *MCPServer, options ...StdioOption) error {
	stdioServer := NewStdioServer(server)
	for _, opt := range options {
		opt(stdioServer)
	}

	// Create context that cancels on interrupt signals
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	err := stdioServer.Serve(ctx, os.Stdin, os.Stdout)
		stdioServer.errLogger.Printf("Server error: %v", err)
		os.Exit(1)
	}
}