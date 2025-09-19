package transport

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestNewJSONRPCErrorResponse(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		id      mcp.RequestId
		code    int
		message string
		data    any
		want    *JSONRPCResponse
	}{
		"basic error response": {
			id:      mcp.NewRequestId(1),
			code:    mcp.METHOD_NOT_FOUND,
			message: "Method not found",
			data:    nil,
			want: &JSONRPCResponse{
				JSONRPC: mcp.JSONRPC_VERSION,
				ID:      mcp.NewRequestId(1),
				Result:  nil,
				Error: &mcp.JSONRPCErrorDetails{
					Code:    mcp.METHOD_NOT_FOUND,
					Message: "Method not found",
					Data:    nil,
				},
			},
		},
		"error response with data": {
			id:      mcp.NewRequestId("test"),
			code:    mcp.INVALID_PARAMS,
			message: "Invalid parameters",
			data:    map[string]any{"field": "value"},
			want: &JSONRPCResponse{
				JSONRPC: mcp.JSONRPC_VERSION,
				ID:      mcp.NewRequestId("test"),
				Result:  nil,
				Error: &mcp.JSONRPCErrorDetails{
					Code:    mcp.INVALID_PARAMS,
					Message: "Invalid parameters",
					Data:    map[string]any{"field": "value"},
				},
			},
		},
		"error response with empty message": {
			id:      mcp.NewRequestId(42),
			code:    mcp.INTERNAL_ERROR,
			message: "",
			data:    nil,
			want: &JSONRPCResponse{
				JSONRPC: mcp.JSONRPC_VERSION,
				ID:      mcp.NewRequestId(42),
				Result:  nil,
				Error: &mcp.JSONRPCErrorDetails{
					Code:    mcp.INTERNAL_ERROR,
					Message: "",
					Data:    nil,
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := NewJSONRPCErrorResponse(tc.id, tc.code, tc.message, tc.data)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestNewJSONRPCResultResponse(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		id     mcp.RequestId
		result json.RawMessage
		want   *JSONRPCResponse
	}{
		"basic result response": {
			id:     mcp.NewRequestId(1),
			result: json.RawMessage(`{"success": true}`),
			want: &JSONRPCResponse{
				JSONRPC: mcp.JSONRPC_VERSION,
				ID:      mcp.NewRequestId(1),
				Result:  json.RawMessage(`{"success": true}`),
				Error:   nil,
			},
		},
		"result response with string ID": {
			id:     mcp.NewRequestId("test-id"),
			result: json.RawMessage(`"simple string result"`),
			want: &JSONRPCResponse{
				JSONRPC: mcp.JSONRPC_VERSION,
				ID:      mcp.NewRequestId("test-id"),
				Result:  json.RawMessage(`"simple string result"`),
				Error:   nil,
			},
		},
		"result response with empty result": {
			id:     mcp.NewRequestId(0),
			result: json.RawMessage(`{}`),
			want: &JSONRPCResponse{
				JSONRPC: mcp.JSONRPC_VERSION,
				ID:      mcp.NewRequestId(0),
				Result:  json.RawMessage(`{}`),
				Error:   nil,
			},
		},
		"result response with null result": {
			id:     mcp.NewRequestId(999),
			result: json.RawMessage(`null`),
			want: &JSONRPCResponse{
				JSONRPC: mcp.JSONRPC_VERSION,
				ID:      mcp.NewRequestId(999),
				Result:  json.RawMessage(`null`),
				Error:   nil,
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := NewJSONRPCResultResponse(tc.id, tc.result)
			require.Equal(t, tc.want, got)
		})
	}
}
