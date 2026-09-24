package server

import (
	"context"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecuteRegularToolAsTask_PanicRecovery mirrors
// TestExecuteTaskTool_PanicRecovery (added in #880) for the sibling code
// path: a regular ServerTool with TaskSupportOptional invoked asynchronously
// via the hybrid task mode. That fix covered executeTaskTool but did not
// touch executeRegularToolAsTask, which still ran without panic recovery --
// an unrecovered panic in this goroutine crashes the whole server process.
func TestExecuteRegularToolAsTask_PanicRecovery(t *testing.T) {
	s := NewMCPServer("test", "1.0.0")

	regularTool := ServerTool{
		Tool: mcp.Tool{
			Name:        "panic-regular-tool",
			Description: "A regular tool that panics when run as a task",
		},
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			panic("deliberate panic in regular tool handler run as a task")
		},
	}

	ctx := t.Context()
	taskID := "test-regular-panic-task"
	entry, err := s.createTask(ctx, taskID, "panic-regular-tool", nil, nil)
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "panic-regular-tool"

	// Execute in a goroutine, same as the production hybrid-mode path.
	go s.executeRegularToolAsTask(ctx, entry, regularTool, request)

	select {
	case <-entry.done:
		// Task completed without crashing the process.
	case <-time.After(5 * time.Second):
		t.Fatal("task did not complete within timeout; panic recovery may have failed")
	}

	s.tasksMu.RLock()
	assert.True(t, entry.completed)
	assert.Equal(t, mcp.TaskStatusFailed, entry.task.Status)
	assert.Contains(t, entry.task.StatusMessage, "panic in task tool handler")
	assert.Contains(t, entry.task.StatusMessage, "deliberate panic in regular tool handler run as a task")
	s.tasksMu.RUnlock()
}
