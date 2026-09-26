package server

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var urlElicitationRequired = mcp.URLElicitationRequiredError{
	Elicitations: []mcp.ElicitationParams{{
		Mode:          mcp.ElicitationModeURL,
		ElicitationID: "auth-1",
		URL:           "https://example.com/authorize?id=auth-1",
		Message:       "Authorization is required to access this resource.",
	}},
}

func newHandlerErrorServer() *MCPServer {
	srv := NewMCPServer("test-server", "1.0.0",
		WithToolCapabilities(true),
		WithPromptCapabilities(true),
		WithResourceCapabilities(false, false),
		WithElicitation(),
	)
	srv.AddTool(mcp.NewTool("protected_action"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Wrapped, as a handler that adds context to the error would.
		return nil, fmt.Errorf("checking access: %w", urlElicitationRequired)
	})
	srv.AddTool(mcp.NewTool("protected_pointer_action"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return nil, fmt.Errorf("checking access: %w", &urlElicitationRequired)
	})
	srv.AddTool(mcp.NewTool("failing_action"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return nil, errors.New("backend unavailable")
	})
	srv.AddTool(mcp.NewTool("gateway_action"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// An error from an upstream server that happens to have a JSON-RPC
		// form of its own is still this server's internal error.
		return nil, fmt.Errorf("upstream: %w", mcp.UnsupportedProtocolVersionError{Version: "2099-01-01"})
	})
	srv.AddPrompt(mcp.NewPrompt("protected_prompt"), func(context.Context, mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return nil, urlElicitationRequired
	})
	srv.AddResource(mcp.NewResource("test://protected", "protected"), func(context.Context, mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		return nil, urlElicitationRequired
	})
	srv.AddResourceTemplate(mcp.NewResourceTemplate("test://protected/{id}", "protected items"), func(context.Context, mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		return nil, urlElicitationRequired
	})
	return srv
}

func handleForError(t *testing.T, srv *MCPServer, request string) mcp.JSONRPCError {
	t.Helper()
	response := srv.HandleMessage(t.Context(), []byte(request))
	errorResponse, ok := response.(mcp.JSONRPCError)
	require.True(t, ok, "expected an error response, got %T", response)
	assert.Equal(t, mcp.NewRequestId(float64(7)), errorResponse.ID)
	return errorResponse
}

// A handler returns mcp.URLElicitationRequiredError to tell the client that it
// must complete a URL elicitation first. Protocol version 2025-11-25 reports
// that as -32042 carrying the elicitations; the client can act on nothing less.
func TestHandlerURLElicitationRequiredError(t *testing.T) {
	srv := newHandlerErrorServer()

	tests := []struct {
		name    string
		request string
		message string
	}{
		{
			name:    "tools/call",
			request: `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"protected_action"}}`,
			message: "checking access: URL elicitation required: 1 elicitation(s) needed",
		},
		{
			name:    "tools/call returning a pointer",
			request: `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"protected_pointer_action"}}`,
			message: "checking access: URL elicitation required: 1 elicitation(s) needed",
		},
		{
			name:    "prompts/get",
			request: `{"jsonrpc":"2.0","id":7,"method":"prompts/get","params":{"name":"protected_prompt"}}`,
			message: "URL elicitation required: 1 elicitation(s) needed",
		},
		{
			name:    "resources/read",
			request: `{"jsonrpc":"2.0","id":7,"method":"resources/read","params":{"uri":"test://protected"}}`,
			message: "URL elicitation required: 1 elicitation(s) needed",
		},
		{
			name:    "resources/read through a template",
			request: `{"jsonrpc":"2.0","id":7,"method":"resources/read","params":{"uri":"test://protected/1"}}`,
			message: "URL elicitation required: 1 elicitation(s) needed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errorResponse := handleForError(t, srv, tt.request)
			assert.Equal(t, mcp.URL_ELICITATION_REQUIRED, errorResponse.Error.Code)
			assert.Equal(t, tt.message, errorResponse.Error.Message)

			// Decode it the way the client does.
			var got mcp.URLElicitationRequiredError
			require.ErrorAs(t, errorResponse.Error.AsError(), &got)
			assert.Equal(t, urlElicitationRequired.Elicitations, got.Elicitations)
		})
	}
}

// Protocol version 2026-07-28 reserves -32042 and asks for a URL elicitation
// through a multi round-trip request instead, so a modern client still gets
// an internal error.
func TestHandlerURLElicitationRequiredErrorForModernClients(t *testing.T) {
	srv := newHandlerErrorServer()

	errorResponse := handleForError(t, srv, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{
		"name":"protected_action",
		"_meta":{
			"io.modelcontextprotocol/protocolVersion":"2026-07-28",
			"io.modelcontextprotocol/clientCapabilities":{}
		}
	}}`)
	assert.Equal(t, mcp.INTERNAL_ERROR, errorResponse.Error.Code)
	assert.Nil(t, errorResponse.Error.Data)
}

// Any other handler error is an internal error, including one that carries a
// JSON-RPC form of its own from somewhere else.
func TestHandlerErrorsAreOtherwiseInternal(t *testing.T) {
	srv := newHandlerErrorServer()

	for _, name := range []string{"failing_action", "gateway_action"} {
		t.Run(name, func(t *testing.T) {
			errorResponse := handleForError(t, srv, fmt.Sprintf(
				`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":%q}}`, name))
			assert.Equal(t, mcp.INTERNAL_ERROR, errorResponse.Error.Code)
			assert.Nil(t, errorResponse.Error.Data)
		})
	}
}
