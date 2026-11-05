package mcp

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallToolRequest_IntegerSlicePrecision(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("large integer arguments require a 64-bit int")
	}
	tests := []struct {
		name      string
		arguments string
		want      []string
	}{
		{"integers", `{"ids":[9007199254740993,-9007199254740993,9223372036854775807]}`, []string{"9007199254740993", "-9007199254740993", "9223372036854775807"}},
		{"decimal and exponent", `{"ids":[9007199254740993.0,9.007199254740993e15]}`, []string{"9007199254740993", "9007199254740993"}},
		{"mixed conversions", `{"ids":[1,"2",3.9,9007199254740993]}`, []string{"1", "2", "3", "9007199254740993"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var request CallToolRequest
			require.NoError(t, json.Unmarshal([]byte(`{"params":{"name":"lookup","arguments":`+tt.arguments+`}}`), &request))
			want := make([]int, len(tt.want))
			for i, value := range tt.want {
				var err error
				want[i], err = strconv.Atoi(value)
				require.NoError(t, err)
			}
			assert.Equal(t, want, request.GetIntSlice("ids", nil))
			got, err := request.RequireIntSlice("ids")
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestCallToolRequest_IntegerSliceFallback(t *testing.T) {
	tests := []struct {
		name      string
		arguments any
		raw       string
		want      []int
		wantErr   string
	}{
		{"typed slice", map[string]any{"ids": []int{1, 2}}, "", []int{1, 2}, ""},
		{"mixed slice", map[string]any{"ids": []any{1, "2", 3.9}}, "", []int{1, 2, 3}, ""},
		{"malformed raw", map[string]any{"ids": []any{1.9}}, "{", []int{1}, ""},
		{"missing raw key", map[string]any{"ids": []any{1.9}}, `{}`, []int{1}, ""},
		{"non-array raw", map[string]any{"ids": []any{1.9}}, `{"ids":1}`, []int{1}, ""},
		{"short raw array", map[string]any{"ids": []any{1.9, 2.9}}, `{"ids":[1.9]}`, []int{1, 2}, ""},
		{"invalid string", map[string]any{"ids": []any{1.0, "bad", 3.9}}, `{"ids":[1,"bad",3.9]}`, []int{1, 3}, `item 1 in argument "ids" cannot be converted to int`},
		{"invalid type", map[string]any{"ids": []any{1.0, true, 3.9}}, `{"ids":[1,true,3.9]}`, []int{1, 3}, `item 1 in argument "ids" is not an int`},
		{"empty array", map[string]any{"ids": []any{}}, `{"ids":[]}`, []int{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := CallToolRequest{Params: CallToolParams{Arguments: tt.arguments, RawArguments: json.RawMessage(tt.raw)}}
			assert.Equal(t, tt.want, request.GetIntSlice("ids", []int{-1}))
			got, err := request.RequireIntSlice("ids")
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
