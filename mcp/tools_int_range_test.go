package mcp

import (
	"encoding/json"
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCallToolRequest_IntOutOfRange verifies that integer accessors do not
// silently overflow when a JSON number lies outside the int range.
func TestCallToolRequest_IntOutOfRange(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("out-of-range integer tests require a 64-bit int")
	}

	const defaultVal = -999

	tests := []struct {
		name          string
		arguments     string // JSON value for the "n" key
		wantGet       int
		wantRequireOK bool
		wantRequire   int
	}{
		{"max int64", `9223372036854775807`, math.MaxInt, true, math.MaxInt},
		{"min int64", `-9223372036854775808`, math.MinInt, true, math.MinInt},
		{"above max int64", `9223372036854775808`, defaultVal, false, 0},
		{"below min int64", `-9223372036854775809`, defaultVal, false, 0},
		{"large positive overflow", `1e20`, defaultVal, false, 0},
		{"large negative overflow", `-1e20`, defaultVal, false, 0},
		{"non-integer truncates", `3.14`, 3, true, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req CallToolRequest
			require.NoError(t, json.Unmarshal(
				[]byte(`{"params":{"name":"t","arguments":{"n":`+tt.arguments+`}}}`),
				&req,
			))

			assert.Equal(t, tt.wantGet, req.GetInt("n", defaultVal))

			got, err := req.RequireInt("n")
			if tt.wantRequireOK {
				require.NoError(t, err)
				assert.Equal(t, tt.wantRequire, got)
			} else {
				require.Error(t, err)
			}
		})
	}
}
