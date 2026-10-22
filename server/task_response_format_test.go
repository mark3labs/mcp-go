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
	response := server.HandleMessage(t.Context(), []byte(`{
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
		WithExtensions(map[string]any{mcp.ExtensionTasks: map[string]any{}}),
	)

	tool := mcp.NewTool("async_op",
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
	)
	server.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("should not be called"), nil
	})

	// Call with task param
	response := server.HandleMessage(t.Context(), []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "tools/call",
		"params": {
			"name": "async_op",
			"_meta": {
				"io.modelcontextprotocol/protocolVersion": "2026-07-28",
				"io.modelcontextprotocol/clientCapabilities": {
					"extensions": {
						"io.modelcontextprotocol/tasks": {}
					}
				}
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

	// Verify SEP-2663 inline structure
	assert.Equal(t, "task", result["resultType"], "resultType should be 'task'")
	assert.Contains(t, result, "taskId", "taskId should be a direct field of result")

	assert.Contains(t, result, "taskId")
	assert.Contains(t, result, "status")
	assert.Equal(t, "working", result["status"])
	assert.Contains(t, result, "createdAt")
	assert.Contains(t, result, "lastUpdatedAt")
	// ttlMs is null when not specified (JSON: "ttlMs": null)
	assert.Contains(t, result, "ttlMs")
	// pollIntervalMs is optional per spec, only check if provided
	// The server sets it to nil when not specified, so it won't appear in JSON
}
