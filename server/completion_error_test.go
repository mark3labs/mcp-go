package server

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
)

func TestCompletionProviderHandlerErrors(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		code    int
		message string
		data    any
	}{
		{
			name: "marked", err: NewHandlerError(testHandlerJSONRPCError{}),
			code: mcp.INVALID_PARAMS, message: "invalid customer ID", data: map[string]any{"field": "customer_id"},
		},
		{
			name: "wrapped marked", err: fmt.Errorf("completing: %w", NewHandlerError(testHandlerJSONRPCError{})),
			code: mcp.INVALID_PARAMS, message: "invalid customer ID", data: map[string]any{"field": "customer_id"},
		},
		{
			name: "unmarked", err: testHandlerJSONRPCError{},
			code: mcp.INTERNAL_ERROR, message: "private diagnostic",
		},
		{
			name: "ordinary", err: errors.New("backend unavailable"),
			code: mcp.INTERNAL_ERROR, message: "backend unavailable",
		},
		{
			name: "URL elicitation", err: urlElicitationRequired,
			code: mcp.URL_ELICITATION_REQUIRED, message: "URL elicitation required: 1 elicitation(s) needed",
			data: map[string]any{"elicitations": urlElicitationRequired.Elicitations},
		},
		{
			name: "wrapped marked URL elicitation", err: fmt.Errorf("completing: %w", NewHandlerError(&urlElicitationRequired)),
			code: mcp.URL_ELICITATION_REQUIRED, message: "completing: URL elicitation required: 1 elicitation(s) needed",
			data: map[string]any{"elicitations": urlElicitationRequired.Elicitations},
		},
	}
	for _, tt := range tests {
		for _, ref := range []string{
			`{"type":"ref/prompt","name":"customers"}`,
			`{"type":"ref/resource","uri":"customer://{customer_id}"}`,
		} {
			for _, version := range []string{"2025-11-25", "2026-07-28"} {
				t.Run(tt.name+"/"+ref+"/"+version, func(t *testing.T) {
					srv := NewMCPServer("test-server", "1.0.0", WithCompletions(),
						WithPromptCompletionProvider(promptCompletionProviderFunc(func(context.Context, string, mcp.CompleteArgument, mcp.CompleteContext) (*mcp.Completion, error) {
							return nil, tt.err
						})),
						WithResourceCompletionProvider(resourceCompletionProviderFunc(func(context.Context, string, mcp.CompleteArgument, mcp.CompleteContext) (*mcp.Completion, error) {
							return nil, tt.err
						})),
					)
					response := handleForError(t, srv, fmt.Sprintf(`{
						"jsonrpc":"2.0","id":7,"method":"completion/complete","params":{
							"ref":%s,"argument":{"name":"customer_id","value":"?"},
							"_meta":{"io.modelcontextprotocol/protocolVersion":%q,"io.modelcontextprotocol/clientCapabilities":{}}
						}}`, ref, version))
					code, data := tt.code, tt.data
					if code == mcp.URL_ELICITATION_REQUIRED && version == "2026-07-28" {
						code, data = mcp.INTERNAL_ERROR, nil
					}
					assert.Equal(t, code, response.Error.Code)
					assert.Equal(t, tt.message, response.Error.Message)
					assert.Equal(t, data, response.Error.Data)
				})
			}
		}
	}
}
