package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallToolRequest_IntegerGettersUseChangedArguments(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		value float64
		want  int
	}{
		{"integer", `7`, 2, 2},
		{"decimal", `7.0`, 2, 2},
		{"exponent", `7e0`, 2, 2},
		{"zero", `7`, 0, 0},
		{"negative fraction", `7`, -2.9, -2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var request CallToolRequest
			require.NoError(t, json.Unmarshal([]byte(`{"params":{"name":"lookup","arguments":{"count":`+tt.raw+`,"ids":[`+tt.raw+`,8]}}}`), &request))
			args := request.GetArguments()
			args["count"] = tt.value
			args["ids"].([]any)[0] = tt.value

			assert.Equal(t, tt.want, request.GetInt("count", -1))
			count, err := request.RequireInt("count")
			require.NoError(t, err)
			assert.Equal(t, tt.want, count)
			assert.Equal(t, []int{tt.want, 8}, request.GetIntSlice("ids", nil))
			ids, err := request.RequireIntSlice("ids")
			require.NoError(t, err)
			assert.Equal(t, []int{tt.want, 8}, ids)
		})
	}
}

func TestCallToolRequest_IntegerSliceUsesChangedOrder(t *testing.T) {
	tests := []struct {
		name string
		ids  []any
		want []int
	}{
		{"reordered", []any{8.0, 7.0}, []int{8, 7}},
		{"shortened", []any{8.0}, []int{8}},
		{"replaced and extended", []any{0.0, -3.9, 10.0}, []int{0, -3, 10}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var request CallToolRequest
			require.NoError(t, json.Unmarshal([]byte(`{"params":{"name":"lookup","arguments":{"ids":[7,8]}}}`), &request))
			request.GetArguments()["ids"] = tt.ids
			assert.Equal(t, tt.want, request.GetIntSlice("ids", nil))
			ids, err := request.RequireIntSlice("ids")
			require.NoError(t, err)
			assert.Equal(t, tt.want, ids)
		})
	}
}

func TestCallToolRequest_BindArgumentsUsesChangedArguments(t *testing.T) {
	var request CallToolRequest
	require.NoError(t, json.Unmarshal([]byte(`{"params":{"name":"lookup","arguments":{"count":7,"ids":[7,8],"name":"old"}}}`), &request))

	args := request.GetArguments()
	args["count"] = float64(2)
	args["ids"].([]any)[0] = float64(2)
	args["name"] = "new"

	var bound struct {
		Count int    `json:"count"`
		IDs   []int  `json:"ids"`
		Name  string `json:"name"`
	}
	require.NoError(t, request.BindArguments(&bound))
	assert.Equal(t, 2, bound.Count)
	assert.Equal(t, []int{2, 8}, bound.IDs)
	assert.Equal(t, "new", bound.Name)

	data, err := json.Marshal(request.Params)
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"lookup","arguments":{"count":2,"ids":[2,8],"name":"new"}}`, string(data))
}

func TestCallToolRequest_ChangedArgumentsRetainUnchangedNumbers(t *testing.T) {
	var request CallToolRequest
	require.NoError(t, json.Unmarshal([]byte(`{"params":{"name":"lookup","arguments":{"name":"old","nested":{"id":9223372036854775807,"remove":true},"ids":[7,9007199254740993]}}}`), &request))
	original := string(request.GetRawArguments().(json.RawMessage))
	args := request.GetArguments()
	args["name"] = "new"
	args["added"] = int64(9007199254740995)
	delete(args["nested"].(map[string]any), "remove")
	args["ids"].([]any)[0] = 2.0

	type boundArguments struct {
		Name   string `json:"name"`
		Added  int64  `json:"added"`
		Nested struct {
			ID     int64 `json:"id"`
			Remove *bool `json:"remove"`
		} `json:"nested"`
		IDs []int64 `json:"ids"`
	}
	var bound boundArguments
	require.NoError(t, request.BindArguments(&bound))
	assert.Equal(t, "new", bound.Name)
	assert.Equal(t, int64(9007199254740995), bound.Added)
	assert.Equal(t, int64(9223372036854775807), bound.Nested.ID)
	assert.Nil(t, bound.Nested.Remove)
	assert.Equal(t, []int64{2, 9007199254740993}, bound.IDs)

	data, err := json.Marshal(request.Params)
	require.NoError(t, err)
	var roundTrip CallToolParams
	require.NoError(t, json.Unmarshal(data, &roundTrip))
	var rebound boundArguments
	require.NoError(t, (CallToolRequest{Params: roundTrip}).BindArguments(&rebound))
	assert.Equal(t, bound, rebound)
	assert.Equal(t, original, string(request.GetRawArguments().(json.RawMessage)))
}

func TestCallToolRequest_ChangedArgumentsBindingAndMarshal(t *testing.T) {
	tests := []struct {
		name      string
		arguments any
		want      string
	}{
		{"nil", nil, `null`},
		{"empty object", map[string]any{}, `{}`},
		{"nil containers", map[string]any{"nested": map[string]any(nil), "ids": []any(nil)}, `{"nested":null,"ids":null}`},
		{"empty containers", map[string]any{"nested": map[string]any{}, "ids": []any{}}, `{"nested":{},"ids":[]}`},
		{"raw replacement", json.RawMessage(`{"name":"new"}`), `{"name":"new"}`},
		{"typed replacement", struct{ Name string }{"new"}, `{"Name":"new"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var request CallToolRequest
			require.NoError(t, json.Unmarshal([]byte(`{"params":{"name":"lookup","arguments":{"nested":{"id":7},"ids":[7,8]}}}`), &request))
			request.Params.Arguments = tt.arguments
			var bound json.RawMessage
			require.NoError(t, request.BindArguments(&bound))
			assert.JSONEq(t, tt.want, string(bound))
			data, err := json.Marshal(request.Params)
			require.NoError(t, err)
			assert.JSONEq(t, `{"name":"lookup","arguments":`+tt.want+`}`, string(data))
		})
	}
}

func TestCallToolRequest_ChangedArgumentsRejectUnencodableValues(t *testing.T) {
	tests := []struct {
		name   string
		change func(map[string]any)
	}{
		{"cycle", func(args map[string]any) { args["nested"] = args }},
		{"unsupported value", func(args map[string]any) { args["added"] = make(chan int) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var request CallToolRequest
			require.NoError(t, json.Unmarshal([]byte(`{"params":{"name":"lookup","arguments":{"nested":{}}}}`), &request))
			tt.change(request.GetArguments())
			var bound map[string]any
			require.Error(t, request.BindArguments(&bound))
			_, err := json.Marshal(request.Params)
			require.Error(t, err)
		})
	}
}
