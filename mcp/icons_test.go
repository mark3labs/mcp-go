package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIconJSONMarshaling(t *testing.T) {
	tests := []struct {
		name     string
		icon     Icon
		expected string
	}{
		{
			name: "basic icon",
			icon: Icon{
				Src: "https://example.com/icon.png",
			},
			expected: `{"src":"https://example.com/icon.png"}`,
		},
		{
			name: "icon with mime type",
			icon: Icon{
				Src:      "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
				MIMEType: "image/png",
			},
			expected: `{"src":"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==","mimeType":"image/png"}`,
		},
		{
			name: "icon with sizes",
			icon: Icon{
				Src:   "https://example.com/icon.svg",
				Sizes: []string{"32x32", "64x64", "any"},
			},
			expected: `{"src":"https://example.com/icon.svg","sizes":["32x32","64x64","any"]}`,
		},
		{
			name: "full icon",
			icon: Icon{
				Src:      "https://example.com/full.png",
				MIMEType: "image/png",
				Sizes:    []string{"128x128"},
			},
			expected: `{"src":"https://example.com/full.png","mimeType":"image/png","sizes":["128x128"]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.icon)
			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(data))

			var unmarshaled Icon
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)
			assert.Equal(t, tt.icon, unmarshaled)
		})
	}
}
