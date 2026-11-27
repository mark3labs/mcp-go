package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestClient_HandleIncomingRequest_InvalidParams(t *testing.T) {
	tests := []struct {
		name   string
		method mcp.MCPMethod
		params any
	}{
		{
			name:   "elicitation with unknown mode",
			method: mcp.MethodElicitationCreate,
			params: map[string]any{"mode": "bogus", "message": "hi"},
		},
		{
			name:   "form elicitation without requestedSchema",
			method: mcp.MethodElicitationCreate,
			params: map[string]any{"message": "hi"},
		},
		{
			name:   "url elicitation without url",
			method: mcp.MethodElicitationCreate,
			params: map[string]any{"mode": "url", "message": "hi", "elicitationId": "1"},
		},
		{
			name:   "elicitation params of the wrong type",
			method: mcp.MethodElicitationCreate,
			params: map[string]any{"message": 5},
		},
		{
			name:   "sampling params of the wrong type",
			method: mcp.MethodSamplingCreateMessage,
			params: map[string]any{"messages": "not-an-array"},
		},
		{
			name:   "sampling message with unknown content type",
			method: mcp.MethodSamplingCreateMessage,
			params: map[string]any{
				"messages": []any{
					map[string]any{"role": "user", "content": map[string]any{"type": "bogus"}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{
				elicitationHandler: &mockElicitationHandler{result: &mcp.ElicitationResult{}},
				samplingHandler:    &mockSamplingHandler{result: &mcp.CreateMessageResult{}},
			}

			response, err := c.handleIncomingRequest(t.Context(), transport.JSONRPCRequest{
				JSONRPC: mcp.JSONRPC_VERSION,
				ID:      mcp.NewRequestId(int64(1)),
				Method:  string(tt.method),
				Params:  tt.params,
			})

			// A Go error would make the transport answer with an internal error.
			require.NoError(t, err)
			require.NotNil(t, response)
			require.NotNil(t, response.Error)
			assert.Equal(t, mcp.INVALID_PARAMS, response.Error.Code)
			assert.Equal(t, mcp.NewRequestId(int64(1)), response.ID)
		})
	}
}
