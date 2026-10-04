package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCallToolResultUnmarshalReusedReceiver(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"empty content", `{"content":[]}`},
		{"null content", `{"content":null}`},
		{"input required", `{"resultType":"input_required","requestState":"next"}`},
		{"new structured content", `{"content":[],"structuredContent":{"current":2}}`},
		{"null structured content", `{"content":[],"structuredContent":null}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reused CallToolResult
			require.NoError(t, json.Unmarshal([]byte(`{"content":[{"type":"text","text":"previous result"}],"structuredContent":{"previous":1},"isError":true}`), &reused))
			require.NoError(t, json.Unmarshal([]byte(tt.data), &reused))

			var fresh CallToolResult
			require.NoError(t, json.Unmarshal([]byte(tt.data), &fresh))
			require.Equal(t, fresh, reused)
		})
	}
}

func TestToolResultContentUnmarshalReusedReceiver(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"empty content", `{"type":"tool_result","toolUseId":"next","content":[]}`},
		{"null content", `{"type":"tool_result","toolUseId":"next","content":null}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reused ToolResultContent
			require.NoError(t, json.Unmarshal([]byte(`{"type":"tool_result","toolUseId":"previous","content":[{"type":"text","text":"previous result"}],"isError":true}`), &reused))
			require.NoError(t, json.Unmarshal([]byte(tt.data), &reused))

			var fresh ToolResultContent
			require.NoError(t, json.Unmarshal([]byte(tt.data), &fresh))
			require.Equal(t, fresh, reused)
		})
	}
}
