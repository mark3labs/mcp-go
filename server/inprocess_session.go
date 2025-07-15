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

type InProcessSession struct {
	sessionID       string
	notifications   chan mcp.JSONRPCNotification
	initialized     atomic.Bool
	loggingLevel    atomic.Value
	clientInfo      atomic.Value
	samplingHandler SamplingHandler
	mu              sync.RWMutex
}

func NewInProcessSession(sessionID string, samplingHandler SamplingHandler) *InProcessSession {
	return &InProcessSession{
		sessionID:       sessionID,
		notifications:   make(chan mcp.JSONRPCNotification, 100),
		samplingHandler: samplingHandler,
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

// GenerateInProcessSessionID generates a unique session ID for inprocess clients
func GenerateInProcessSessionID() string {
	return fmt.Sprintf("inprocess-%d", time.Now().UnixNano())
}

// Ensure interface compliance
var (
	_ ClientSession         = (*InProcessSession)(nil)
	_ SessionWithLogging    = (*InProcessSession)(nil)
	_ SessionWithClientInfo = (*InProcessSession)(nil)
	_ SessionWithSampling   = (*InProcessSession)(nil)
)
