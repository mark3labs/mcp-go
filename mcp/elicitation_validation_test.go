package mcp_test

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestElicitationParams_Validate(t *testing.T) {
	tests := []struct {
		name    string
		params  mcp.ElicitationParams
		wantErr bool
	}{
		{
			name: "Valid Form Mode",
			params: mcp.ElicitationParams{
				Mode:            mcp.ElicitationModeForm,
				Message:         "Fill this form",
				RequestedSchema: map[string]any{"type": "object"},
			},
			wantErr: false,
		},
		{
			name: "Valid URL Mode",
			params: mcp.ElicitationParams{
				Mode:          mcp.ElicitationModeURL,
				Message:       "Click this link",
				ElicitationID: "123",
				URL:           "https://example.com/auth",
			},
			wantErr: false,
		},
		{
			name: "Implicit Form Form Mode (Default)",
			params: mcp.ElicitationParams{
				Mode:            "",
				Message:         "Fill this form",
				RequestedSchema: map[string]any{"type": "object"},
			},
			wantErr: false, // Should default to form and validate schema
		},
		{
			name: "Invalid Mode",
			params: mcp.ElicitationParams{
				Mode: "invalid-mode",
			},
			wantErr: true,
		},
		{
			name: "Form Mode Missing Schema",
			params: mcp.ElicitationParams{
				Mode:    mcp.ElicitationModeForm,
				Message: "Missing schema",
			},
			wantErr: true,
		},
		{
			name: "URL Mode Missing URL",
			params: mcp.ElicitationParams{
				Mode:          mcp.ElicitationModeURL,
				ElicitationID: "123",
				Message:       "Missing URL",
			},
			wantErr: true,
		},
		{
			name: "URL Mode Missing ElicitationID",
			params: mcp.ElicitationParams{
				Mode:    mcp.ElicitationModeURL,
				URL:     "https://example.com",
				Message: "Missing ID",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.params.Validate()
			if tt.wantErr {
				require.Error(t, err, "expected error for test case: %s", tt.name)
			} else {
				require.NoError(t, err, "unexpected error for test case: %s", tt.name)
			}
		})
	}
}
