package server

import (
	"slices"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestSupportedProtocolVersionsFromContext(t *testing.T) {
	tests := []struct {
		name     string
		versions []string
		set      bool
		want     []string
	}{
		{
			name: "defaults when unset",
			want: mcp.ValidProtocolVersions,
		},
		{
			name: "preserves explicit empty list",
			set:  true,
			want: []string{},
		},
		{
			name:     "preserves configured subset",
			versions: []string{"2025-03-26"},
			set:      true,
			want:     []string{"2025-03-26"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			if tt.set {
				ctx = WithSupportedProtocolVersions(ctx, tt.versions)
			}

			got := SupportedProtocolVersionsFromContext(ctx)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("SupportedProtocolVersionsFromContext() = %v, want %v", got, tt.want)
			}
		})
	}
}
