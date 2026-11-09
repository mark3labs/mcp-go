package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestTaskResultRetainsToolContentArray(t *testing.T) {
	tests := []struct {
		name    string
		content []mcp.Content
		want    string
	}{
		{"nil", nil, `[]`},
		{"empty", []mcp.Content{}, `[]`},
		{"nonempty", []mcp.Content{mcp.NewTextContent("done")}, `[{"type":"text","text":"done"}]`},
	}
	for _, tt := range tests {
		for _, taskTool := range []bool{false, true} {
			name := "tool/" + tt.name
			if taskTool {
				name = "task_tool/" + tt.name
			}
			t.Run(name, func(t *testing.T) {
				s := NewMCPServer("test", "1", WithTaskCapabilities(true, true, true))
				entry, err := s.createTask(t.Context(), "task", "tool", nil, nil)
				require.NoError(t, err)
				if taskTool {
					s.completeTask(entry, &mcp.CreateTaskResult{Content: tt.content, StructuredContent: map[string]any{"ok": true}}, nil)
				} else {
					s.completeTask(entry, &mcp.CallToolResult{Content: tt.content, StructuredContent: map[string]any{"ok": true}}, nil)
				}
				response := s.HandleMessage(t.Context(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tasks/result","params":{"taskId":"task"}}`))
				data, err := json.Marshal(response)
				require.NoError(t, err)
				var wire struct {
					Result map[string]json.RawMessage `json:"result"`
				}
				require.NoError(t, json.Unmarshal(data, &wire))
				require.NotNil(t, wire.Result, string(data))
				assert.JSONEq(t, tt.want, string(wire.Result["content"]))
				assert.JSONEq(t, `{"ok":true}`, string(wire.Result["structuredContent"]))
				assert.Contains(t, string(wire.Result["_meta"]), `"taskId":"task"`)
			})
		}
	}
}
