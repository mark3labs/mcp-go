package server

import (
	"context"
	"fmt"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStructuredToolHandlerURLElicitationRequiredError(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "value", err: urlElicitationRequired},
		{name: "pointer", err: &urlElicitationRequired},
		{name: "wrapped value", err: fmt.Errorf("checking access: %w", urlElicitationRequired)},
		{name: "wrapped pointer", err: fmt.Errorf("checking access: %w", &urlElicitationRequired)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newHandlerErrorServer()
			srv.AddTool(mcp.NewTool("protected_structured_action"), mcp.NewStructuredToolHandler(
				func(context.Context, mcp.CallToolRequest, struct{}) (struct{}, error) {
					return struct{}{}, tt.err
				},
			))

			errorResponse := handleForError(t, srv, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"protected_structured_action","arguments":{}}}`)
			assert.Equal(t, mcp.URL_ELICITATION_REQUIRED, errorResponse.Error.Code)
			assert.Equal(t, tt.err.Error(), errorResponse.Error.Message)

			var got mcp.URLElicitationRequiredError
			require.ErrorAs(t, errorResponse.Error.AsError(), &got)
			assert.Equal(t, urlElicitationRequired.Elicitations, got.Elicitations)
		})
	}
}

func TestStructuredToolHandlerURLElicitationRequiredErrorForModernClients(t *testing.T) {
	srv := newHandlerErrorServer()
	srv.AddTool(mcp.NewTool("protected_structured_action"), mcp.NewStructuredToolHandler(
		func(context.Context, mcp.CallToolRequest, struct{}) (struct{}, error) {
			return struct{}{}, urlElicitationRequired
		},
	))

	errorResponse := handleForError(t, srv, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{
		"name":"protected_structured_action",
		"arguments":{},
		"_meta":{
			"io.modelcontextprotocol/protocolVersion":"2026-07-28",
			"io.modelcontextprotocol/clientCapabilities":{}
		}
	}}`)
	assert.Equal(t, mcp.INTERNAL_ERROR, errorResponse.Error.Code)
	assert.Nil(t, errorResponse.Error.Data)
}
