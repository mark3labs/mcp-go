package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// SamplingContext provides sampling capabilities to server tools
type SamplingContext interface {
	// Sample requests LLM completion with flexible input types
	Sample(ctx context.Context, input SampleInput, opts ...SampleOption) (*SampleResult, error)

	// SampleWithParams provides direct parameter control
	SampleWithParams(ctx context.Context, req *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error)
}

// SampleInput represents flexible input types for sampling
type SampleInput interface {
	toSamplingRequest() (*mcp.CreateMessageRequest, error)
}

// SampleResult wraps the sampling response for easier access
type SampleResult struct {
	response *mcp.CreateMessageResult
}

// Text returns the text content from the sampling result
func (r *SampleResult) Text() string {
	if textContent, ok := mcp.AsTextContent(r.response.Content); ok {
		return textContent.Text
	}
	return ""
}

// Content returns the raw content from the sampling result
func (r *SampleResult) Content() mcp.Content {
	return r.response.Content
}

// Model returns the model name used for sampling
func (r *SampleResult) Model() string {
	return r.response.Model
}

// StopReason returns the reason sampling stopped
func (r *SampleResult) StopReason() string {
	if r.response.StopReason != nil {
		return *r.response.StopReason
	}
	return ""
}

// Response returns the raw CreateMessageResult
func (r *SampleResult) Response() *mcp.CreateMessageResult {
	return r.response
}

// SampleOption configures sampling requests
type SampleOption func(*mcp.CreateMessageParams)

// Common sample options
func WithTemperature(temp float64) SampleOption {
	return func(p *mcp.CreateMessageParams) {
		p.Temperature = &temp
	}
}

func WithMaxTokens(max int) SampleOption {
	return func(p *mcp.CreateMessageParams) {
		p.MaxTokens = &max
	}
}

func WithModel(hints ...string) SampleOption {
	return func(p *mcp.CreateMessageParams) {
		if p.ModelPreferences == nil {
			p.ModelPreferences = &mcp.ModelPreferences{}
		}
		for _, hint := range hints {
			p.ModelPreferences.Hints = append(p.ModelPreferences.Hints, mcp.ModelHint{Name: hint})
		}
	}
}

func WithSystemPrompt(prompt string) SampleOption {
	return func(p *mcp.CreateMessageParams) {
		p.SystemPrompt = &prompt
	}
}

func WithContext(include mcp.ContextInclusion) SampleOption {
	return func(p *mcp.CreateMessageParams) {
		p.IncludeContext = &include
	}
}

func WithStopSequences(sequences ...string) SampleOption {
	return func(p *mcp.CreateMessageParams) {
		p.StopSequences = sequences
	}
}

// Input type implementations
type StringInput string

func (s StringInput) toSamplingRequest() (*mcp.CreateMessageRequest, error) {
	return &mcp.CreateMessageRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodSamplingCreateMessage),
		},
		CreateMessageParams: mcp.CreateMessageParams{
			Messages: []mcp.SamplingMessage{
				{
					Role:    mcp.RoleUser,
					Content: mcp.NewTextContent(string(s)),
				},
			},
		},
	}, nil
}

type MessagesInput []mcp.SamplingMessage

func (m MessagesInput) toSamplingRequest() (*mcp.CreateMessageRequest, error) {
	return &mcp.CreateMessageRequest{
		Request: mcp.Request{
			Method: string(mcp.MethodSamplingCreateMessage),
		},
		CreateMessageParams: mcp.CreateMessageParams{
			Messages: []mcp.SamplingMessage(m),
		},
	}, nil
}

type RequestInput struct {
	*mcp.CreateMessageRequest
}

func (r RequestInput) toSamplingRequest() (*mcp.CreateMessageRequest, error) {
	return r.CreateMessageRequest, nil
}

// Errors
var (
	ErrSamplingNotSupported = errors.New("sampling not supported by client")
	ErrSamplingNotEnabled   = errors.New("sampling capability not enabled on server")
	ErrSamplingTimeout      = errors.New("sampling request timed out")
	ErrSamplingRejected     = errors.New("sampling request rejected by client")
)

// samplingContext implements SamplingContext
type samplingContext struct {
	session ClientSession
	server  *MCPServer
}

func (sc *samplingContext) Sample(ctx context.Context, input SampleInput, opts ...SampleOption) (*SampleResult, error) {
	req, err := input.toSamplingRequest()
	if err != nil {
		return nil, fmt.Errorf("failed to convert input to sampling request: %w", err)
	}

	// Apply options
	for _, opt := range opts {
		opt(&req.CreateMessageParams)
	}

	result, err := sc.SampleWithParams(ctx, req)
	if err != nil {
		return nil, err
	}

	return &SampleResult{response: result}, nil
}

func (sc *samplingContext) SampleWithParams(ctx context.Context, req *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	// Check if client supports sampling
	if !sc.supportsClientSampling() {
		return nil, ErrSamplingNotSupported
	}

	// Check if server has sampling capability enabled
	if !sc.server.hasSamplingCapability() {
		return nil, ErrSamplingNotEnabled
	}

	// Send sampling request to client (this will be implemented in Phase 3)
	return sc.sendSamplingRequest(ctx, req)
}

func (sc *samplingContext) supportsClientSampling() bool {
	// This would check the client's capabilities during initialization
	// Implementation depends on session capability tracking
	// For now, we'll assume it's supported if the server has sampling enabled
	return sc.server.hasSamplingCapability()
}

func (sc *samplingContext) sendSamplingRequest(ctx context.Context, req *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	// Check if the session supports bidirectional requests
	sessionWithRequests, ok := sc.session.(SessionWithRequests)
	if !ok {
		return nil, fmt.Errorf("session does not support bidirectional requests - sampling requires SessionWithRequests interface")
	}

	// Send the sampling request to the client
	response, err := sessionWithRequests.SendRequest(ctx, string(mcp.MethodSamplingCreateMessage), req.Params)
	if err != nil {
		return nil, fmt.Errorf("failed to send sampling request: %w", err)
	}

	// Check for JSON-RPC error
	if response.Error != nil {
		return nil, fmt.Errorf("sampling request failed: %s (code %d)", response.Error.Message, response.Error.Code)
	}

	// Parse the result
	var result mcp.CreateMessageResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal sampling result: %w", err)
	}

	return &result, nil
}

// Context key for sampling context
type samplingContextKey struct{}

// SamplingContextFromContext retrieves the sampling context from the context
func SamplingContextFromContext(ctx context.Context) SamplingContext {
	if sc, ok := ctx.Value(samplingContextKey{}).(SamplingContext); ok {
		return sc
	}
	return nil
}
