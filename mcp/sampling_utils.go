package mcp

import (
	"encoding/json"
	"fmt"
)

// Helper functions for sampling

// NewSamplingMessage creates a new SamplingMessage with the given role and content
func NewSamplingMessage(role Role, content Content) SamplingMessage {
	return SamplingMessage{
		Role:    role,
		Content: content,
	}
}

// NewCreateMessageRequest creates a new CreateMessageRequest with the given messages
func NewCreateMessageRequest(messages []SamplingMessage) *CreateMessageRequest {
	return &CreateMessageRequest{
		Request: Request{
			Method: string(MethodSamplingCreateMessage),
		},
		CreateMessageParams: CreateMessageParams{
			Messages: messages,
		},
	}
}

// NewCreateMessageResult creates a new CreateMessageResult with the given parameters
func NewCreateMessageResult(role Role, content Content, model string) *CreateMessageResult {
	return &CreateMessageResult{
		Role:    role,
		Content: content,
		Model:   model,
	}
}

// NewModelPreferences creates a new ModelPreferences with the given hints
func NewModelPreferences(hints ...string) *ModelPreferences {
	preferences := &ModelPreferences{}
	for _, hint := range hints {
		preferences.Hints = append(preferences.Hints, ModelHint{Name: hint})
	}
	return preferences
}

// WithSystemPrompt adds a system prompt to CreateMessageParams
func (p *CreateMessageParams) WithSystemPrompt(prompt string) *CreateMessageParams {
	p.SystemPrompt = &prompt
	return p
}

// WithTemperature adds temperature to CreateMessageParams
func (p *CreateMessageParams) WithTemperature(temp float64) *CreateMessageParams {
	p.Temperature = &temp
	return p
}

// WithMaxTokens adds max tokens to CreateMessageParams
func (p *CreateMessageParams) WithMaxTokens(max int) *CreateMessageParams {
	p.MaxTokens = &max
	return p
}

// WithModelPreferences adds model preferences to CreateMessageParams
func (p *CreateMessageParams) WithModelPreferences(preferences *ModelPreferences) *CreateMessageParams {
	p.ModelPreferences = preferences
	return p
}

// WithContext adds context inclusion to CreateMessageParams
func (p *CreateMessageParams) WithContext(context ContextInclusion) *CreateMessageParams {
	p.IncludeContext = &context
	return p
}

// WithStopSequences adds stop sequences to CreateMessageParams
func (p *CreateMessageParams) WithStopSequences(sequences ...string) *CreateMessageParams {
	p.StopSequences = sequences
	return p
}

// WithMetadata adds metadata to CreateMessageParams
func (p *CreateMessageParams) WithMetadata(metadata map[string]any) *CreateMessageParams {
	p.Metadata = metadata
	return p
}

// Parsing helpers for sampling

// ParseCreateMessageRequest parses a raw JSON message into a CreateMessageRequest
func ParseCreateMessageRequest(rawMessage *json.RawMessage) (*CreateMessageRequest, error) {
	if rawMessage == nil {
		return nil, fmt.Errorf("message is nil")
	}

	var request CreateMessageRequest
	if err := json.Unmarshal(*rawMessage, &request); err != nil {
		return nil, fmt.Errorf("failed to unmarshal sampling request: %w", err)
	}

	return &request, nil
}

// ParseCreateMessageResult parses a raw JSON message into a CreateMessageResult
func ParseCreateMessageResult(rawMessage *json.RawMessage) (*CreateMessageResult, error) {
	if rawMessage == nil {
		return nil, fmt.Errorf("message is nil")
	}

	var result CreateMessageResult
	if err := json.Unmarshal(*rawMessage, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal sampling result: %w", err)
	}

	return &result, nil
}

// Helper function to create a simple text sampling message
func NewTextSamplingMessage(role Role, text string) SamplingMessage {
	return NewSamplingMessage(role, NewTextContent(text))
}

// Helper function to create a simple text CreateMessageRequest
func NewTextCreateMessageRequest(role Role, text string) *CreateMessageRequest {
	return NewCreateMessageRequest([]SamplingMessage{
		NewTextSamplingMessage(role, text),
	})
}

// Helper function to create a simple text CreateMessageResult
func NewTextCreateMessageResult(text, model string) *CreateMessageResult {
	return NewCreateMessageResult(RoleAssistant, NewTextContent(text), model)
}