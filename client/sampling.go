package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// SamplingHandler processes sampling requests from servers
type SamplingHandler interface {
	HandleSampling(ctx context.Context, req *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error)
}

// SamplingHandlerFunc is a functional adapter for SamplingHandler
type SamplingHandlerFunc func(context.Context, *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error)

func (f SamplingHandlerFunc) HandleSampling(ctx context.Context, req *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	return f(ctx, req)
}

// SimpleSamplingHandler provides a simple string-based sampling interface
type SimpleSamplingHandler func(messages []mcp.SamplingMessage) (string, error)

// ToSamplingHandler converts a SimpleSamplingHandler to a SamplingHandler
func (s SimpleSamplingHandler) ToSamplingHandler() SamplingHandler {
	return SamplingHandlerFunc(func(ctx context.Context, req *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
		response, err := s(req.Messages)
		if err != nil {
			return nil, err
		}

		return &mcp.CreateMessageResult{
			Role:    mcp.RoleAssistant,
			Content: mcp.NewTextContent(response),
			Model:   "unknown", // Client should set appropriate model name
		}, nil
	})
}

// SamplingError represents errors that can occur during sampling
type SamplingError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *SamplingError) Error() string {
	return e.Message
}

// Error codes for sampling
const (
	ErrCodeSamplingNotSupported = -32001
	ErrCodeSamplingFailed       = -32002
	ErrCodeSamplingRejected     = -32003
	ErrCodeSamplingTimeout      = -32004
)

// NewSamplingError creates a new SamplingError with the given code and message
func NewSamplingError(code int, message string) *SamplingError {
	return &SamplingError{Code: code, Message: message}
}

// Common sampling errors
var (
	ErrSamplingNotSupported = NewSamplingError(ErrCodeSamplingNotSupported, "sampling not supported")
	ErrSamplingFailed       = NewSamplingError(ErrCodeSamplingFailed, "sampling failed")
	ErrSamplingRejected     = NewSamplingError(ErrCodeSamplingRejected, "sampling request rejected")
	ErrSamplingTimeout      = NewSamplingError(ErrCodeSamplingTimeout, "sampling request timed out")
)