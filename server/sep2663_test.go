package server

import (
	"context"
	"encoding/json"
	"maps"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModernTaskRoutingRequiresNegotiation(t *testing.T) {
	tools := []struct {
		name     string
		support  mcp.TaskSupport
		taskOnly bool
	}{
		{name: "optional", support: mcp.TaskSupportOptional},
		{name: "required", support: mcp.TaskSupportRequired},
		{name: "task-only", taskOnly: true},
	}
	declarations := []struct {
		name    string
		option  ServerOption
		enabled bool
	}{
		{name: "server undeclared"},
		{name: "helper declaration", option: WithTasksExtension(), enabled: true},
		{name: "map declaration", option: WithExtensions(map[string]any{mcp.ExtensionTasks: map[string]any{}}), enabled: true},
	}
	for _, tool := range tools {
		for _, declaration := range declarations {
			for _, clientDeclared := range []bool{false, true} {
				clientName := "client undeclared"
				if clientDeclared {
					clientName = "client declared"
				}
				t.Run(tool.name+"/"+declaration.name+"/"+clientName, func(t *testing.T) {
					options := []ServerOption{WithTaskCapabilities(true, true, true)}
					if declaration.option != nil {
						options = append(options, declaration.option)
					}
					srv := NewMCPServer("test", "1.0.0", options...)
					_, advertised := srv.serverCapabilitiesSnapshot(true).Extensions[mcp.ExtensionTasks]
					assert.Equal(t, declaration.enabled, advertised)
					called := make(chan struct{}, 1)
					if tool.taskOnly {
						srv.AddTaskTool(mcp.NewTool("work"), func(context.Context, mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
							called <- struct{}{}
							return &mcp.CreateTaskResult{Content: []mcp.Content{mcp.NewTextContent("done")}}, nil
						})
					} else {
						srv.AddTool(mcp.NewTool("work", mcp.WithTaskSupport(tool.support)), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
							called <- struct{}{}
							return mcp.NewToolResultText("done"), nil
						})
					}
					caps := &mcp.ClientCapabilities{}
					if clientDeclared {
						caps.Extensions = map[string]any{mcp.ExtensionTasks: map[string]any{}}
					}
					ctx := WithRequestProtocolInfo(t.Context(), &RequestProtocolInfo{Modern: true, ProtocolVersion: "2026-07-28", ClientCapabilities: caps})
					result, reqErr := srv.handleToolCall(ctx, 1, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "work", Task: &mcp.TaskParams{}}})
					requiresTask := tool.taskOnly || tool.support == mcp.TaskSupportRequired
					if requiresTask && (!declaration.enabled || !clientDeclared) {
						require.NotNil(t, reqErr)
						expected := mcp.METHOD_NOT_FOUND
						if declaration.enabled {
							expected = mcp.MISSING_REQUIRED_CLIENT_CAPABILITY
							assert.Equal(t, missingExtensionData(mcp.ExtensionTasks), reqErr.data)
						}
						assert.Equal(t, expected, reqErr.code)
						assert.Nil(t, result)
						assert.Empty(t, called)
						assert.Empty(t, srv.tasks)
						return
					}
					require.Nil(t, reqErr)
					if declaration.enabled && clientDeclared {
						created, ok := result.(*mcp.CreateTaskResult)
						require.True(t, ok, "got %T", result)
						assert.NotEmpty(t, created.Task.TaskId)
						assert.False(t, created.Legacy)
					} else {
						_, ok := result.(*mcp.CallToolResult)
						require.True(t, ok, "got %T", result)
						assert.Empty(t, srv.tasks)
					}
					select {
					case <-called:
					case <-time.After(5 * time.Second):
						t.Fatal("handler was not called")
					}
				})
			}
		}
	}
}

func TestWithTasksExtensionPreservesSharedExtensions(t *testing.T) {
	tests := []struct {
		name       string
		extensions map[string]any
	}{
		{name: "nil map"},
		{name: "empty map", extensions: map[string]any{}},
		{name: "existing settings", extensions: map[string]any{
			"example.com/other": map[string]any{"enabled": true},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := maps.Clone(tt.extensions)
			sharedOption := WithExtensions(tt.extensions)
			other := NewMCPServer("without-tasks", "1.0.0", sharedOption)
			tasks := NewMCPServer("with-tasks", "1.0.0", sharedOption, WithTasksExtension())

			assert.Equal(t, original, tt.extensions, "the caller's map must remain unchanged")
			otherExtensions := other.serverCapabilitiesSnapshot(true).Extensions
			assert.Equal(t, original, otherExtensions)
			assert.NotContains(t, otherExtensions, mcp.ExtensionTasks)
			taskExtensions := tasks.serverCapabilitiesSnapshot(true).Extensions
			require.Contains(t, taskExtensions, mcp.ExtensionTasks)
			assert.Equal(t, map[string]any{}, taskExtensions[mcp.ExtensionTasks])
			assert.Len(t, taskExtensions, len(original)+1)
			for name, settings := range original {
				assert.Equal(t, settings, taskExtensions[name], "existing extension %s", name)
			}
		})
	}
}

// modernCallParams builds a _meta block for a modern protocol
// request that declares the tasks extension.
func modernTaskMeta() map[string]any {
	return map[string]any{
		"io.modelcontextprotocol/protocolVersion": "2026-07-28",
		"io.modelcontextprotocol/clientCapabilities": map[string]any{
			"extensions": map[string]any{
				"io.modelcontextprotocol/tasks": map[string]any{},
			},
		},
	}
}

// TestSEP2663_ServerDirectedTaskCreation verifies that a modern client can
// trigger task creation without setting the legacy "task" param on the request.
func TestSEP2663_ServerDirectedTaskCreation(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
		WithTasksExtension(),
	)

	tool := mcp.NewTool("async_work",
		mcp.WithDescription("Async tool"),
		mcp.WithTaskSupport(mcp.TaskSupportOptional),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("sync result"), nil
	})

	metaBytes, _ := json.Marshal(modernTaskMeta())
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"async_work","_meta":` + string(metaBytes) + `}}`

	response := srv.HandleMessage(t.Context(), []byte(req))
	resp, ok := response.(mcp.JSONRPCResponse)
	require.True(t, ok, "Expected JSONRPCResponse, got %T", response)

	result, ok := resp.Result.(*mcp.CreateTaskResult)
	require.True(t, ok, "Expected *CreateTaskResult (server-directed), got %T", resp.Result)
	assert.Equal(t, mcp.ResultTypeTask, result.GetResultType())
	assert.NotEmpty(t, result.Task.TaskId)
	assert.Equal(t, mcp.TaskStatusWorking, result.Task.Status)
	assert.False(t, result.Legacy, "Modern client should get SEP-2663 format")
}

// TestSEP2663_CreateTaskResultWireFormat verifies the JSON wire shape for
// modern clients matches the SEP-2663 spec (inline fields, resultType:"task").
func TestSEP2663_CreateTaskResultWireFormat(t *testing.T) {
	result := mcp.CreateTaskResult{
		Task: mcp.Task{
			TaskId:        "abc123",
			Status:        mcp.TaskStatusWorking,
			CreatedAt:     "2026-09-04T12:00:00Z",
			LastUpdatedAt: "2026-09-04T12:00:00Z",
		},
	}
	result.SetResultType(mcp.ResultTypeTask)

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))

	assert.Equal(t, "task", m["resultType"])
	assert.Equal(t, "abc123", m["taskId"])
	assert.Equal(t, "working", m["status"])
	assert.Equal(t, "2026-09-04T12:00:00Z", m["createdAt"])
	assert.NotContains(t, m, "task", "task fields must be inline, not wrapped")
}

// TestSEP2663_LegacyCreateTaskResultWireFormat verifies the JSON wire shape
// for legacy 2025-11-25 clients uses the old "task" wrapper key.
func TestSEP2663_LegacyCreateTaskResultWireFormat(t *testing.T) {
	result := mcp.CreateTaskResult{
		Task: mcp.Task{
			TaskId:        "abc123",
			Status:        mcp.TaskStatusWorking,
			CreatedAt:     "2026-09-04T12:00:00Z",
			LastUpdatedAt: "2026-09-04T12:00:00Z",
		},
		Legacy: true,
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))

	assert.Contains(t, m, "task", "legacy format must wrap under 'task' key")
	assert.NotContains(t, m, "resultType")
	taskMap, ok := m["task"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "abc123", taskMap["taskId"])
}

// TestSEP2663_GetTaskInlinesResult verifies that tasks/get for a completed
// task inlines the result field in the response for modern clients.
func TestSEP2663_GetTaskInlinesResult(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	modernCtx := buildModernContext(t.Context(), srv)
	ttl := int64(60000)
	entry, err := srv.createTask(modernCtx, "task-inline", "tool", &ttl, nil)
	require.NoError(t, err)

	// Complete the task with a tool result
	toolResult := &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent("hello world")},
	}
	srv.completeTask(entry, toolResult, nil)

	// Wait for completion
	for range 100 {
		t, _, _ := srv.getTask(modernCtx, "task-inline")
		if t.Status.IsTerminal() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	getResult, reqErr := srv.handleGetTask(modernCtx, 1, mcp.GetTaskRequest{
		Params: mcp.GetTaskParams{TaskId: "task-inline"},
	})
	require.Nil(t, reqErr)
	require.NotNil(t, getResult)

	assert.Equal(t, mcp.TaskStatusCompleted, getResult.Status)
	assert.NotNil(t, getResult.TaskResult, "completed task must have inlined result")
}

// TestSEP2663_CancelTaskAckOnly verifies that tasks/cancel for a modern
// client returns an ack-only result, including for an already-terminal task.
func TestSEP2663_CancelTaskAckOnly(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	modernCtx := buildModernContext(t.Context(), srv)
	// Create under the same modern context so the task's protocol origin matches
	// the canceller (cross-origin isolation rejects mismatches).
	entry, err := srv.createTask(modernCtx, "task-cancel-ack", "tool", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, mcp.TaskStatusWorking, entry.task.Status)

	result, reqErr := srv.handleCancelTask(modernCtx, 1, mcp.CancelTaskRequest{
		Params: mcp.CancelTaskParams{TaskId: "task-cancel-ack"},
	})
	require.Nil(t, reqErr)
	require.NotNil(t, result)

	// Verify it serializes as an empty object (no task fields).
	data, _ := json.Marshal(result)
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	assert.NotContains(t, m, "taskId", "ack-only result must not have task fields")

	// Cancelling an already-terminal task on the modern path must still ack silently, not error.
	result, reqErr = srv.handleCancelTask(modernCtx, 2, mcp.CancelTaskRequest{
		Params: mcp.CancelTaskParams{TaskId: "task-cancel-ack"},
	})
	require.Nil(t, reqErr, "modern cancel of a terminal task must ack, not error")
	require.NotNil(t, result)
}

// TestSEP2663_TasksUpdateAck verifies that tasks/update returns an ack.
func TestSEP2663_TasksUpdateAck(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0",
		WithTaskCapabilities(true, true, true),
	)

	modernCtx := buildModernContext(t.Context(), srv)
	entry, err := srv.createTask(modernCtx, "task-update-ack", "tool", nil, nil)
	require.NoError(t, err)

	// tasks/update is only valid when the task is in input_required state.
	srv.tasksMu.Lock()
	entry.task.Status = mcp.TaskStatusInputRequired
	srv.tasksMu.Unlock()

	updateResult, reqErr := srv.handleUpdateTask(modernCtx, 1, mcp.UpdateTaskRequest{
		Params: mcp.UpdateTaskParams{
			TaskId:         "task-update-ack",
			InputResponses: mcp.InputResponses{},
		},
	})
	require.Nil(t, reqErr)
	require.NotNil(t, updateResult)
	assert.Equal(t, mcp.ResultTypeComplete, updateResult.GetResultType())
}

// TestSEP2663_HasExtension tests the ClientCapabilities.HasExtension helper.
func TestSEP2663_HasExtension(t *testing.T) {
	t.Run("nil caps", func(t *testing.T) {
		var caps *mcp.ClientCapabilities
		assert.False(t, caps.HasExtension(mcp.ExtensionTasks))
	})
	t.Run("no extensions", func(t *testing.T) {
		caps := &mcp.ClientCapabilities{}
		assert.False(t, caps.HasExtension(mcp.ExtensionTasks))
	})
	t.Run("extension present", func(t *testing.T) {
		caps := &mcp.ClientCapabilities{
			Extensions: map[string]any{mcp.ExtensionTasks: map[string]any{}},
		}
		assert.True(t, caps.HasExtension(mcp.ExtensionTasks))
	})
	t.Run("different extension", func(t *testing.T) {
		caps := &mcp.ClientCapabilities{
			Extensions: map[string]any{"some/other": map[string]any{}},
		}
		assert.False(t, caps.HasExtension(mcp.ExtensionTasks))
	})
}

// buildModernContext creates a context that simulates a modern protocol request
// with the tasks extension declared.
func buildModernContext(ctx context.Context, _ *MCPServer) context.Context {
	return WithRequestProtocolInfo(ctx, &RequestProtocolInfo{
		Modern:          true,
		ProtocolVersion: "2026-07-28",
		ClientCapabilities: &mcp.ClientCapabilities{
			Extensions: map[string]any{mcp.ExtensionTasks: map[string]any{}},
		},
	})
}
