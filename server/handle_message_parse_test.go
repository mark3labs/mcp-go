package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestHandleMessage_ParsesEnvelope(t *testing.T) {
	tests := []struct {
		name      string
		message   string
		wantNil   bool
		wantError int
		wantID    any
	}{
		{name: "trailing garbage", message: `{"jsonrpc":"2.0","id":1,"method":"ping"} x`, wantError: mcp.PARSE_ERROR},
		{name: "two values", message: `{"jsonrpc":"2.0","id":1,"method":"ping"}{"a":1}`, wantError: mcp.PARSE_ERROR},
		{name: "not an object", message: `[1,2]`, wantError: mcp.PARSE_ERROR},
		{name: "invalid id", message: `{"jsonrpc":"2.0","id":{,"method":"ping"}`, wantError: mcp.PARSE_ERROR},
		{name: "response with result", message: `{"jsonrpc":"2.0","id":7,"result":{"content":[]}}`, wantNil: true},
		{name: "null result is a request", message: `{"jsonrpc":"2.0","id":3,"result":null,"method":"ping"}`, wantID: float64(3)},
		{name: "string id", message: `{"jsonrpc":"2.0","id":"abc","method":"ping"}`, wantID: "abc"},
		{name: "large id keeps precision", message: `{"jsonrpc":"2.0","id":9007199254740993,"method":"ping"}`, wantID: int64(9007199254740993)},
		{name: "params array is an invalid request", message: `{"jsonrpc":"2.0","id":4,"method":"ping","params":[1]}`, wantError: mcp.INVALID_REQUEST},
		{name: "malformed _meta is an invalid request", message: `{"jsonrpc":"2.0","id":5,"method":"ping","params":{"_meta":"x"}}`, wantError: mcp.INVALID_REQUEST},
		{name: "null params", message: `{"jsonrpc":"2.0","id":6,"method":"ping","params":null}`, wantID: float64(6)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewMCPServer("test", "1.0.0")
			resp := s.HandleMessage(t.Context(), json.RawMessage(tt.message))

			if tt.wantNil {
				assert.Nil(t, resp)
				return
			}
			require.NotNil(t, resp)

			if tt.wantError != 0 {
				errResp, ok := resp.(mcp.JSONRPCError)
				require.True(t, ok, "want JSONRPCError, got %T", resp)
				assert.Equal(t, tt.wantError, errResp.Error.Code)
				return
			}

			okResp, ok := resp.(mcp.JSONRPCResponse)
			require.True(t, ok, "want JSONRPCResponse, got %T", resp)
			assert.Equal(t, tt.wantID, okResp.ID.Value())
		})
	}
}
