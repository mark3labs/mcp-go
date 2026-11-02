package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotificationParamsReuseDropsOmittedFields(t *testing.T) {
	tests := []struct {
		name           string
		second         string
		wantMeta       map[string]any
		wantAdditional map[string]any
	}{
		{
			name:           "meta and field omitted",
			second:         `{"uri":"file:///new"}`,
			wantMeta:       map[string]any{},
			wantAdditional: map[string]any{"uri": "file:///new"},
		},
		{
			name:           "empty object",
			second:         `{}`,
			wantMeta:       map[string]any{},
			wantAdditional: map[string]any{},
		},
		{
			name:           "new meta replaces old meta",
			second:         `{"_meta":{"traceId":"t"}}`,
			wantMeta:       map[string]any{"traceId": "t"},
			wantAdditional: map[string]any{},
		},
		{
			name:           "non-object meta",
			second:         `{"_meta":null,"level":"info"}`,
			wantMeta:       map[string]any{},
			wantAdditional: map[string]any{"level": "info"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var params NotificationParams
			require.NoError(t, json.Unmarshal([]byte(`{"_meta":{"subscriptionId":"old"},"uri":"file:///old","progress":1}`), &params))
			require.NoError(t, json.Unmarshal([]byte(tt.second), &params))
			assert.Equal(t, tt.wantMeta, params.Meta)
			assert.Equal(t, tt.wantAdditional, params.AdditionalFields)
		})
	}
}

func TestNotificationParamsReuseKeepsEarlierMaps(t *testing.T) {
	var params NotificationParams
	require.NoError(t, json.Unmarshal([]byte(`{"_meta":{"a":1},"uri":"file:///old"}`), &params))
	oldMeta, oldAdditional := params.Meta, params.AdditionalFields

	require.NoError(t, json.Unmarshal([]byte(`{"_meta":{"b":2},"level":"info"}`), &params))
	assert.Equal(t, map[string]any{"a": float64(1)}, oldMeta)
	assert.Equal(t, map[string]any{"uri": "file:///old"}, oldAdditional)
}

func TestNotificationParamsNullAndMalformedKeepValue(t *testing.T) {
	var params NotificationParams
	require.NoError(t, json.Unmarshal([]byte(`{"_meta":{"a":1},"uri":"file:///kept"}`), &params))

	require.NoError(t, json.Unmarshal([]byte(`null`), &params))
	assert.Equal(t, map[string]any{"a": float64(1)}, params.Meta)
	assert.Equal(t, map[string]any{"uri": "file:///kept"}, params.AdditionalFields)

	require.Error(t, json.Unmarshal([]byte(`[1,2]`), &params))
	assert.Equal(t, map[string]any{"a": float64(1)}, params.Meta)
	assert.Equal(t, map[string]any{"uri": "file:///kept"}, params.AdditionalFields)

	var empty NotificationParams
	require.NoError(t, json.Unmarshal([]byte(`null`), &empty))
	assert.NotNil(t, empty.Meta)
	assert.NotNil(t, empty.AdditionalFields)
}

func TestJSONRPCNotificationReuseDropsOmittedParams(t *testing.T) {
	var n JSONRPCNotification
	require.NoError(t, json.Unmarshal([]byte(`{"jsonrpc":"2.0","method":"notifications/progress","params":{"_meta":{"x":1},"progressToken":"p","progress":1}}`), &n))
	require.NoError(t, json.Unmarshal([]byte(`{"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info"}}`), &n))
	assert.Equal(t, "notifications/message", n.Method)
	assert.Empty(t, n.Params.Meta)
	assert.Equal(t, map[string]any{"level": "info"}, n.Params.AdditionalFields)
}
