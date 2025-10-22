package server

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// SamplingHandler defines the interface for handling sampling requests from servers.
type SamplingHandler interface {
	CreateMessage(ctx context.Context, request mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error)
}

// ElicitationHandler defines the interface for handling elicitation requests from servers.
type ElicitationHandler interface {
	Elicit(ctx context.Context, request mcp.ElicitationRequest) (*mcp.ElicitationResult, error)
}

type RootsHandler interface {
	ListRoots(ctx context.Context, request mcp.ListRootsRequest) (*mcp.ListRootsResult, error)
}

type InProcessSession struct {
	sessionID          string
	notifications      chan mcp.JSONRPCNotification
	initialized        atomic.Bool
	loggingLevel       atomic.Value
	clientInfo         atomic.Value
	clientCapabilities atomic.Value
	samplingHandler    SamplingHandler
	elicitationHandler ElicitationHandler
	rootsHandler       RootsHandler
	mu                 sync.RWMutex
}

// NewInProcessSession creates a new InProcessSession for the provided sessionID and samplingHandler.
// The returned session has a buffered notifications channel and its sampling handler set; elicitation and roots handlers remain unset.
func NewInProcessSession(sessionID string, samplingHandler SamplingHandler) *InProcessSession {
	return &InProcessSession{
		sessionID:       sessionID,
		notifications:   make(chan mcp.JSONRPCNotification, 100),
		samplingHandler: samplingHandler,
	}
}

// NewInProcessSessionWithHandlers creates an InProcessSession with the given session ID and handler implementations.
// The session is created with a buffered notifications channel (capacity 100) and the provided sampling, elicitation,
// and roots handlers attached.
func NewInProcessSessionWithHandlers(sessionID string, samplingHandler SamplingHandler, elicitationHandler ElicitationHandler, rootsHandler RootsHandler) *InProcessSession {
	return &InProcessSession{
		sessionID:          sessionID,
		notifications:      make(chan mcp.JSONRPCNotification, 100),
		samplingHandler:    samplingHandler,
		elicitationHandler: elicitationHandler,
		rootsHandler:       rootsHandler,
	}
}

func (s *InProcessSession) SessionID() string {
	return s.sessionID
}

func (s *InProcessSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return s.notifications
}

func (s *InProcessSession) Initialize() {
	s.loggingLevel.Store(mcp.LoggingLevelError)
	s.initialized.Store(true)
}

func (s *InProcessSession) Initialized() bool {
	return s.initialized.Load()
}

func (s *InProcessSession) GetClientInfo() mcp.Implementation {
	if value := s.clientInfo.Load(); value != nil {
		if clientInfo, ok := value.(mcp.Implementation); ok {
			return clientInfo
		}
	}
	return mcp.Implementation{}
}

func (s *InProcessSession) SetClientInfo(clientInfo mcp.Implementation) {
	s.clientInfo.Store(clientInfo)
}

func (s *InProcessSession) GetClientCapabilities() mcp.ClientCapabilities {
	if value := s.clientCapabilities.Load(); value != nil {
		if clientCapabilities, ok := value.(mcp.ClientCapabilities); ok {
			return clientCapabilities
		}
	}
	return mcp.ClientCapabilities{}
}

func (s *InProcessSession) SetClientCapabilities(clientCapabilities mcp.ClientCapabilities) {
	s.clientCapabilities.Store(clientCapabilities)
}

func (s *InProcessSession) SetLogLevel(level mcp.LoggingLevel) {
	s.loggingLevel.Store(level)
}

func (s *InProcessSession) GetLogLevel() mcp.LoggingLevel {
	level := s.loggingLevel.Load()
	if level == nil {
		return mcp.LoggingLevelError
	}
	return level.(mcp.LoggingLevel)
}

func (s *InProcessSession) RequestSampling(ctx context.Context, request mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	s.mu.RLock()
	handler := s.samplingHandler
	s.mu.RUnlock()

	if handler == nil {
		return nil, fmt.Errorf("no sampling handler available")
	}

	return handler.CreateMessage(ctx, request)
}

func (s *InProcessSession) RequestElicitation(ctx context.Context, request mcp.ElicitationRequest) (*mcp.ElicitationResult, error) {
	s.mu.RLock()
	handler := s.elicitationHandler
	s.mu.RUnlock()

	if handler == nil {
		return nil, fmt.Errorf("no elicitation handler available")
	}

	return handler.Elicit(ctx, request)
}

func (s *InProcessSession) ListRoots(ctx context.Context, request mcp.ListRootsRequest) (*mcp.ListRootsResult, error) {
	s.mu.RLock()
	handler := s.rootsHandler
	s.mu.RUnlock()

	if handler == nil {
		return nil, fmt.Errorf("no roots handler available")
	}

	return handler.ListRoots(ctx, request)
}

// GenerateInProcessSessionID returns a session identifier formatted as "inprocess-<n>", where <n> is the current Unix time in nanoseconds, suitable for use as a unique in-process session ID.
func GenerateInProcessSessionID() string {
	return fmt.Sprintf("inprocess-%d", time.Now().UnixNano())
}

// Ensure interface compliance
var (
	_ ClientSession          = (*InProcessSession)(nil)
	_ SessionWithLogging     = (*InProcessSession)(nil)
	_ SessionWithClientInfo  = (*InProcessSession)(nil)
	_ SessionWithSampling    = (*InProcessSession)(nil)
	_ SessionWithElicitation = (*InProcessSession)(nil)
	_ SessionWithRoots       = (*InProcessSession)(nil)
)