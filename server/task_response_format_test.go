package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTaskAugmentedToolCall_ResponseFormat verifies that task-augmented tool calls
// return the correct response format per MCP spec 2025-11-25.
// The spec requires task to be a direct field of result, NOT nested in _meta.
func TestTaskAugmentedToolCall_ResponseFormat(t *testing.T) {
	server := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	tool := mcp.NewTool("async_op",
		mcp.WithDescription("An async operation"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)
	server.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("should not be called directly"), nil
	})

	// Call with task param
	response := server.HandleMessage(context.Background(), []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "tools/call",
		"params": {
			"name": "async_op",
			"task": {}
		}
	}`))

	// Parse response
	jsonResp, ok := response.(mcp.JSONRPCResponse)
	require.True(t, ok, "Expected JSONRPCResponse")

	// Verify structure: result.task exists (not result._meta.task)
	createTaskResult, ok := jsonResp.Result.(*mcp.CreateTaskResult)
	require.True(t, ok, "Expected *CreateTaskResult, got: %T", jsonResp.Result)
	require.NotNil(t, createTaskResult.Task, "task should be direct field of result")

	// Verify task has required fields per spec
	assert.NotEmpty(t, createTaskResult.Task.TaskId)
	assert.Equal(t, mcp.TaskStatusWorking, createTaskResult.Task.Status)
	assert.NotEmpty(t, createTaskResult.Task.CreatedAt)
	assert.NotEmpty(t, createTaskResult.Task.LastUpdatedAt)
}

// TestTaskAugmentedToolCall_SpecCompliance verifies that the JSON structure
// matches the spec example exactly.
func TestTaskAugmentedToolCall_SpecCompliance(t *testing.T) {
	server := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	tool := mcp.NewTool("async_op",
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)
	server.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("should not be called"), nil
	})

	// Call with task param
	response := server.HandleMessage(context.Background(), []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "tools/call",
		"params": {
			"name": "async_op",
			"task": {
				"ttl": 60000,
				"pollInterval": 5000
			}
		}
	}`))

	// Marshal to JSON to verify structure
	jsonBytes, err := json.MarshalIndent(response, "", "  ")
	require.NoError(t, err)

	// Parse as generic map to check structure
	var parsed map[string]any
	err = json.Unmarshal(jsonBytes, &parsed)
	require.NoError(t, err)

	// Verify top-level fields
	assert.Equal(t, "2.0", parsed["jsonrpc"])
	assert.NotNil(t, parsed["id"])
	assert.NotNil(t, parsed["result"])

	// Verify result structure
	result, ok := parsed["result"].(map[string]any)
	require.True(t, ok, "result should be an object")

	// Verify task is a direct field of result (not in _meta)
	assert.Contains(t, result, "task", "task should be direct field of result")
	assert.NotContains(t, result, "_meta", "task should NOT be in _meta")

	// Verify task structure
	task, ok := result["task"].(map[string]any)
	require.True(t, ok, "task should be an object")

	assert.Contains(t, task, "taskId")
	assert.Contains(t, task, "status")
	assert.Equal(t, "working", task["status"])
	assert.Contains(t, task, "createdAt")
	assert.Contains(t, task, "lastUpdatedAt")
	assert.Contains(t, task, "ttl")
	assert.Equal(t, float64(60000), task["ttl"])
	// pollInterval is optional per spec, only check if provided
	// The server sets it to nil when not specified, so it won't appear in JSON
	// This is correct behavior per JSON omitempty
}
