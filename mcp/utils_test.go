package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewJSONRPCResultResponse(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		id     RequestId
		result any
		want   JSONRPCResponse
	}{
		"string result": {
			id:     NewRequestId(1),
			result: "test result",
			want: JSONRPCResponse{
				JSONRPC: JSONRPC_VERSION,
				ID:      NewRequestId(1),
				Result:  "test result",
			},
		},
		"map result": {
			id:     NewRequestId("test-id"),
			result: map[string]any{"key": "value"},
			want: JSONRPCResponse{
				JSONRPC: JSONRPC_VERSION,
				ID:      NewRequestId("test-id"),
				Result:  map[string]any{"key": "value"},
			},
		},
		"nil result": {
			id:     NewRequestId(42),
			result: nil,
			want: JSONRPCResponse{
				JSONRPC: JSONRPC_VERSION,
				ID:      NewRequestId(42),
				Result:  nil,
			},
		},
		"struct result": {
			id:     NewRequestId(0),
			result: struct{ Name string }{Name: "test"},
			want: JSONRPCResponse{
				JSONRPC: JSONRPC_VERSION,
				ID:      NewRequestId(0),
				Result:  struct{ Name string }{Name: "test"},
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

func TestNewJSONRPCResponse(t *testing.T) {
	t.Parallel()

	// Test the existing constructor that takes Result struct
	id := NewRequestId(1)
	result := Result{Meta: &Meta{}}

	got := NewJSONRPCResponse(id, result)
	want := JSONRPCResponse{
		JSONRPC: JSONRPC_VERSION,
		ID:      id,
		Result:  result,
	}

	require.Equal(t, want, got)
}