package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestTaskResultPreservesRawStructuredContent(t *testing.T) {
	s := NewMCPServer("test", "1")
	entry, err := s.createTask(t.Context(), "task", "tool", nil, nil)
	require.NoError(t, err)
	var stored mcp.CallToolResult
	require.NoError(t, json.Unmarshal([]byte(`{"content":[],"structuredContent":{"id":9007199254740993}}`), &stored))
	s.completeTask(entry, &stored, nil)
	request := mcp.TaskResultRequest{}
	request.Params.TaskId = "task"
	result, requestErr := s.handleTaskResult(t.Context(), 1, request)
	require.Nil(t, requestErr)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"id":9007199254740993`)
}
