package server

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

// CompletionProvider defines how to provide completion suggestions
type CompletionProvider interface {
	// CompletePromptArgument provides completions for a prompt argument
	CompletePromptArgument(ctx context.Context, promptName string, argument mcp.CompleteArgument) (*mcp.Completion, error)

	// CompleteResourceArgument provides completions for a resource template argument
	CompleteResourceArgument(ctx context.Context, uri string, argument mcp.CompleteArgument) (*mcp.Completion, error)
}

// DefaultCompletionProvider returns no completions (fallback)
type DefaultCompletionProvider struct{}

func (p *DefaultCompletionProvider) CompletePromptArgument(ctx context.Context, promptName string, argument mcp.CompleteArgument) (*mcp.Completion, error) {
	return &mcp.Completion{
		Values: []string{},
	}, nil
}

func (p *DefaultCompletionProvider) CompleteResourceArgument(ctx context.Context, uri string, argument mcp.CompleteArgument) (*mcp.Completion, error) {
	return &mcp.Completion{
		Values: []string{},
	}, nil
}
