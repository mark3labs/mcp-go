package mcp

import (
	"encoding/json"
	"testing"
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
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if details.Code != tt.wantCode {
				t.Errorf("code = %d, want %d", details.Code, tt.wantCode)
			}
			if details.Message != tt.wantMessage {
				t.Errorf("message = %q, want %q", details.Message, tt.wantMessage)
			}
		})
	}
}

// Verify that a full JSON-RPC response with a string error field unmarshals correctly.
func TestJSONRPCResponse_StringError(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"error":"cursor_invalid"}`

	type response struct {
		JSONRPC string                `json:"jsonrpc"`
		ID      int                   `json:"id"`
		Error   *JSONRPCErrorDetails  `json:"error"`
	}

	var resp response
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error to be non-nil")
	}
	if resp.Error.Code != INTERNAL_ERROR {
		t.Errorf("code = %d, want %d", resp.Error.Code, INTERNAL_ERROR)
	}
	if resp.Error.Message != "cursor_invalid" {
		t.Errorf("message = %q, want %q", resp.Error.Message, "cursor_invalid")
	}
}
