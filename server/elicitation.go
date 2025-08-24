package server

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// RequestElicitation sends an elicitation request to the client.
// The client must have declared elicitation capability during initialization.
// The session must implement SessionWithElicitation to support this operation.
func (s *MCPServer) RequestElicitation(ctx context.Context, request mcp.ElicitationRequest) (*mcp.ElicitationResult, error) {
	session := ClientSessionFromContext(ctx)
	if session == nil {
		return nil, fmt.Errorf("no active session")
	}

	// Check if the session supports elicitation requests
	if elicitationSession, ok := session.(SessionWithElicitation); ok {
		return elicitationSession.RequestElicitation(ctx, request)
	}

	return nil, fmt.Errorf("session does not support elicitation")
}
