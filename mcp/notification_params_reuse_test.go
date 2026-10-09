package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotificationParamsReuseClearsOmittedFields(t *testing.T) {
	var params NotificationParams
	require.NoError(t, json.Unmarshal([]byte(`{"_meta":{"subscriptionId":"old"},"uri":"file:///old","progress":1}`), &params))
	require.NoError(t, json.Unmarshal([]byte(`{"uri":"file:///new"}`), &params))
	assert.Empty(t, params.Meta)
	assert.Equal(t, map[string]any{"uri": "file:///new"}, params.AdditionalFields)
	require.NoError(t, json.Unmarshal([]byte(`{}`), &params))
	assert.Empty(t, params.AdditionalFields)

	params.AdditionalFields["uri"] = "file:///retained"
	require.NoError(t, json.Unmarshal([]byte(`null`), &params))
	assert.Equal(t, "file:///retained", params.AdditionalFields["uri"])
}
