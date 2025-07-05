package server

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// SamplingClient defines the interface for servers to request sampling from clients.
type SamplingClient interface {
	// RequestSampling sends a sampling request to the client and returns the result.
	RequestSampling(ctx context.Context, request mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error)
}

// EnableSampling enables sampling capabilities for the server.
// This allows the server to send sampling requests to clients that support it.
func (s *MCPServer) EnableSampling() {
	s.capabilitiesMu.Lock()
	defer s.capabilitiesMu.Unlock()

	if s.capabilities.sampling == nil {
		s.capabilities.sampling = &samplingCapabilities{}
	}
	s.capabilities.sampling.enabled = true
}

// RequestSampling sends a sampling request to the client.
// The client must have declared sampling capability during initialization.
func (s *MCPServer) RequestSampling(ctx context.Context, request mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	s.capabilitiesMu.RLock()
	enabled := s.capabilities.sampling != nil && s.capabilities.sampling.enabled
	s.capabilitiesMu.RUnlock()

	if !enabled {
		return nil, fmt.Errorf("sampling not enabled on server")
	}

	session := ClientSessionFromContext(ctx)
	if session == nil {
		return nil, fmt.Errorf("no active session")
	}

	// Check if the session supports sampling requests
	if samplingSession, ok := session.(SessionWithSampling); ok {
		return samplingSession.RequestSampling(ctx, request)
	}

	return nil, fmt.Errorf("session does not support sampling")
}

// SessionWithSampling extends ClientSession to support sampling requests.
type SessionWithSampling interface {
	ClientSession
	RequestSampling(ctx context.Context, request mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error)
}
