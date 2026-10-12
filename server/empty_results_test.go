package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fields such as tools, messages and completion values are required arrays in
// the MCP schema. Go encodes a nil slice as null, which the schema rejects and
// which clients that validate responses refuse to parse. A nil slice reaches
// these fields easily: a filter or handler that builds its result with append
// returns nil when nothing matches.
func TestEmptyResultsAreSentAsArrays(t *testing.T) {
	noTool := func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText(""), nil
	}
	noContents := func(context.Context, mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		return nil, nil
	}
	completions := func(completion *mcp.Completion) PromptCompletionProvider {
		return promptCompletionProviderFunc(func(context.Context, string, mcp.CompleteArgument, mcp.CompleteContext) (*mcp.Completion, error) {
			return completion, nil
		})
	}

	tests := []struct {
		name    string
		server  func() *MCPServer
		request string
		field   []string
	}{
		{
			name: "tools/list with every tool filtered out",
			server: func() *MCPServer {
				s := NewMCPServer("test", "1.0.0",
					WithToolCapabilities(true),
					WithToolFilter(func(context.Context, []mcp.Tool) []mcp.Tool { return nil }),
				)
				s.AddTool(mcp.NewTool("hidden"), noTool)
				return s
			},
			request: `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
			field:   []string{"tools"},
		},
		{
			name: "prompts/list with every prompt filtered out",
			server: func() *MCPServer {
				s := NewMCPServer("test", "1.0.0",
					WithPromptCapabilities(true),
					WithPromptFilter(func(context.Context, []mcp.Prompt) []mcp.Prompt { return nil }),
				)
				s.AddPrompt(mcp.NewPrompt("hidden"), func(context.Context, mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
					return mcp.NewGetPromptResult("", nil), nil
				})
				return s
			},
			request: `{"jsonrpc":"2.0","id":1,"method":"prompts/list"}`,
			field:   []string{"prompts"},
		},
		{
			name: "tasks/list before any task has run",
			server: func() *MCPServer {
				return NewMCPServer("test", "1.0.0", WithTaskCapabilities(true, true, true))
			},
			request: `{"jsonrpc":"2.0","id":1,"method":"tasks/list"}`,
			field:   []string{"tasks"},
		},
		{
			name: "resources/list with no resources",
			server: func() *MCPServer {
				return NewMCPServer("test", "1.0.0", WithResourceCapabilities(false, false))
			},
			request: `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`,
			field:   []string{"resources"},
		},
		{
			name: "completion/complete with a nil completion",
			server: func() *MCPServer {
				return NewMCPServer("test", "1.0.0", WithCompletions(), WithPromptCompletionProvider(completions(nil)))
			},
			request: `{"jsonrpc":"2.0","id":1,"method":"completion/complete","params":{"ref":{"type":"ref/prompt","name":"p"},"argument":{"name":"a","value":"x"}}}`,
			field:   []string{"completion", "values"},
		},
		{
			name: "completion/complete with no matching values",
			server: func() *MCPServer {
				return NewMCPServer("test", "1.0.0", WithCompletions(), WithPromptCompletionProvider(completions(&mcp.Completion{})))
			},
			request: `{"jsonrpc":"2.0","id":1,"method":"completion/complete","params":{"ref":{"type":"ref/prompt","name":"p"},"argument":{"name":"a","value":"x"}}}`,
			field:   []string{"completion", "values"},
		},
		{
			name: "prompts/get with no messages",
			server: func() *MCPServer {
				s := NewMCPServer("test", "1.0.0", WithPromptCapabilities(true))
				s.AddPrompt(mcp.NewPrompt("empty"), func(context.Context, mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
					return &mcp.GetPromptResult{Description: "nothing to say"}, nil
				})
				return s
			},
			request: `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"empty"}}`,
			field:   []string{"messages"},
		},
		{
			name: "resources/read of a resource with no contents",
			server: func() *MCPServer {
				s := NewMCPServer("test", "1.0.0", WithResourceCapabilities(false, false))
				s.AddResource(mcp.NewResource("test://empty", "empty"), noContents)
				return s
			},
			request: `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"test://empty"}}`,
			field:   []string{"contents"},
		},
		{
			name: "resources/read through a template with no contents",
			server: func() *MCPServer {
				s := NewMCPServer("test", "1.0.0", WithResourceCapabilities(false, false))
				s.AddResourceTemplate(mcp.NewResourceTemplate("test://items/{id}", "items"),
					func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
						return noContents(ctx, request)
					})
				return s
			},
			request: `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"test://items/1"}}`,
			field:   []string{"contents"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := tt.server().HandleMessage(t.Context(), []byte(tt.request))
			encoded, err := json.Marshal(response)
			require.NoError(t, err)

			var envelope struct {
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			require.NoError(t, json.Unmarshal(encoded, &envelope))
			require.Nil(t, envelope.Error, "unexpected error response: %s", encoded)

			value := envelope.Result
			for _, key := range tt.field {
				var object map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(value, &object), "in %s", encoded)
				value = object[key]
			}
			assert.Equal(t, "[]", string(value), "in %s", encoded)
		})
	}
}

// A handler may return the same result for every request, so the server must
// not write the empty messages list into it.
func TestGetPromptLeavesTheHandlerResultUnchanged(t *testing.T) {
	shared := &mcp.GetPromptResult{Description: "shared"}
	s := NewMCPServer("test", "1.0.0", WithPromptCapabilities(true))
	s.AddPrompt(mcp.NewPrompt("shared"), func(context.Context, mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return shared, nil
	})

	result, reqErr := s.handleGetPrompt(t.Context(), 1, mcp.GetPromptRequest{Params: mcp.GetPromptParams{Name: "shared"}})
	require.Nil(t, reqErr)
	assert.Equal(t, []mcp.PromptMessage{}, result.Messages)
	assert.Equal(t, "shared", result.Description)
	assert.Nil(t, shared.Messages)
}

// askForTopic is a prompt that needs a topic from the user and has nothing to
// say once it has one.
func askForTopic(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	if ElicitationResponse(request.Params.InputResponses, "topic") != nil {
		return &mcp.GetPromptResult{Description: "no messages for this topic"}, nil
	}
	return NewInputRequestBuilder("step=1").
		Elicit("topic", mcp.ElicitationParams{
			Mode:    mcp.ElicitationModeForm,
			Message: "Which topic?",
			RequestedSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"topic": map[string]any{"type": "string"}},
			},
		}).
		PromptResult(), nil
}

// A prompt asking the client for more input has no messages yet, so it keeps
// the shape it was given.
func TestGetPromptLeavesInputRequestsWithoutMessages(t *testing.T) {
	s := NewMCPServer("test", "1.0.0", WithPromptCapabilities(true), WithElicitation())
	s.AddPrompt(mcp.NewPrompt("ask"), askForTopic)

	ctx := WithRequestProtocolInfo(t.Context(), &RequestProtocolInfo{
		Modern:             true,
		ProtocolVersion:    mcp.ProtocolVersion20260728,
		ClientCapabilities: &mcp.ClientCapabilities{Elicitation: &mcp.ElicitationCapability{}},
	})
	result, reqErr := s.handleGetPrompt(ctx, 1, mcp.GetPromptRequest{Params: mcp.GetPromptParams{Name: "ask"}})
	require.Nil(t, reqErr)
	require.True(t, result.NeedsInput())
	assert.Nil(t, result.Messages)
}

// For a client that predates multi round-trip, the server answers the input
// request itself and retries the handler; the retried result is the one sent.
func TestGetPromptBridgedRetryWithoutMessages(t *testing.T) {
	s := NewMCPServer("test", "1.0.0", WithPromptCapabilities(true), WithElicitation())
	s.AddPrompt(mcp.NewPrompt("ask"), askForTopic)

	session := newMRTRSession("legacy")
	session.response = &mcp.ElicitationResult{
		ElicitationResponse: mcp.ElicitationResponse{
			Action:  mcp.ElicitationResponseActionAccept,
			Content: map[string]any{"topic": "go"},
		},
	}
	ctx := s.WithContext(t.Context(), session)
	ctx = WithRequestProtocolInfo(ctx, &RequestProtocolInfo{})

	result, reqErr := s.handleGetPrompt(ctx, 1, mcp.GetPromptRequest{Params: mcp.GetPromptParams{Name: "ask"}})
	require.Nil(t, reqErr)
	assert.Equal(t, 1, session.calls, "the server should have elicited on the handler's behalf")
	assert.Equal(t, "no messages for this topic", result.Description)
	assert.Equal(t, []mcp.PromptMessage{}, result.Messages)
}
