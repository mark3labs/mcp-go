package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONRPCErrorDetails_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantCode    int
		wantMessage string
		wantErr     bool
	}{
		{
			name:        "standard object",
			input:       `{"code": -32600, "message": "invalid request"}`,
			wantCode:    -32600,
			wantMessage: "invalid request",
		},
		{
			name:        "string error from non-compliant server",
			input:       `"cursor_invalid"`,
			wantCode:    INTERNAL_ERROR,
			wantMessage: "cursor_invalid",
		},
		{
			name:        "object with data field",
			input:       `{"code": -32603, "message": "something failed", "data": {"detail": "more info"}}`,
			wantCode:    -32603,
			wantMessage: "something failed",
		},
		{
			name:    "number is rejected",
			input:   `42`,
			wantErr: true,
		},
		{
			name:    "array is rejected",
			input:   `["bad"]`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var details JSONRPCErrorDetails
			err := json.Unmarshal([]byte(tt.input), &details)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantCode, details.Code)
			assert.Equal(t, tt.wantMessage, details.Message)
		})
	}
}

// Verify that a full JSON-RPC response with a string error field unmarshals correctly.
func TestJSONRPCResponse_StringError(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"error":"cursor_invalid"}`

	type response struct {
		JSONRPC string               `json:"jsonrpc"`
		ID      int                  `json:"id"`
		Error   *JSONRPCErrorDetails `json:"error"`
	}

	var resp response
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))
	require.NotNil(t, resp.Error)
	assert.Equal(t, INTERNAL_ERROR, resp.Error.Code)
	assert.Equal(t, "cursor_invalid", resp.Error.Message)
}
