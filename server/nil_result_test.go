package server

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A prompt handler that returns neither a result nor an error used to crash
// the dispatcher with a nil pointer dereference. Over streamable HTTP the
// client saw the connection drop; with the in-process client the panic reached
// the caller.
func TestGetPromptNilResultIsAnError(t *testing.T) {
	srv := NewMCPServer("test-server", "1.0.0", WithPromptCapabilities(true))
	srv.AddPrompt(mcp.NewPrompt("broken"), func(context.Context, mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return nil, nil
	})

	var response mcp.JSONRPCMessage
	require.NotPanics(t, func() {
		response = srv.HandleMessage(t.Context(), []byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "prompts/get",
			"params": {"name": "broken"}
		}`))
	})

	errorResponse, ok := response.(mcp.JSONRPCError)
	require.True(t, ok, "expected an error response, got %T", response)
	assert.Equal(t, mcp.INTERNAL_ERROR, errorResponse.Error.Code)
	assert.Equal(t, "prompt 'broken' handler returned no result", errorResponse.Error.Message)
}

// The retry a legacy client's request is bridged through can return nil too,
// after the first call asked for input.
func TestGetPromptNilResultOnBridgedRetryIsAnError(t *testing.T) {
	srv := NewMCPServer("test-server", "1.0.0", WithPromptCapabilities(true), WithElicitation())
	srv.AddPrompt(mcp.NewPrompt("askThenFail"), func(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		if ElicitationResponse(request.Params.InputResponses, "topic") != nil {
			return nil, nil
		}
		return NewInputRequestBuilder("step=1").
			Elicit("topic", mcp.ElicitationParams{
				Mode:    mcp.ElicitationModeForm,
				Message: "Which topic?",
				RequestedSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{"topic": map[string]any{"type": "string"}},
				},
			}).
			PromptResult(), nil
	})

	session := newMRTRSession("legacy")
	session.response = &mcp.ElicitationResult{
		ElicitationResponse: mcp.ElicitationResponse{
			Action:  mcp.ElicitationResponseActionAccept,
			Content: map[string]any{"topic": "go"},
		},
	}
	ctx := srv.WithContext(t.Context(), session)
	ctx = WithRequestProtocolInfo(ctx, &RequestProtocolInfo{})

	result, reqErr := srv.handleGetPrompt(ctx, 1, mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{Name: "askThenFail"},
	})
	assert.Nil(t, result)
	require.NotNil(t, reqErr)
	assert.Equal(t, mcp.INTERNAL_ERROR, reqErr.code)
	assert.EqualError(t, reqErr.err, "prompt 'askThenFail' handler returned no result")
	assert.Equal(t, 1, session.calls, "the server should have elicited on the handler's behalf")
}

// A tool handler that returns neither a result nor an error used to be
// answered with a null result, which clients reject as malformed.
func TestCallToolNilResultIsAnError(t *testing.T) {
	srv := NewMCPServer("test-server", "1.0.0", WithToolCapabilities(true))
	srv.AddTool(mcp.NewTool("broken"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return nil, nil
	})

	response := srv.HandleMessage(t.Context(), []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "tools/call",
		"params": {"name": "broken"}
	}`))

	errorResponse, ok := response.(mcp.JSONRPCError)
	require.True(t, ok, "expected an error response, got %T", response)
	assert.Equal(t, mcp.INTERNAL_ERROR, errorResponse.Error.Code)
	assert.Equal(t, "tool 'broken' handler returned no result", errorResponse.Error.Message)
}

// As with prompts, the retry a legacy client's call is bridged through can
// return nil after the first call asked for input.
func TestCallToolNilResultOnBridgedRetryIsAnError(t *testing.T) {
	srv := NewMCPServer("test-server", "1.0.0", WithToolCapabilities(true), WithElicitation())
	srv.AddTool(mcp.NewTool("askThenFail"), func(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if ElicitationResponse(request.Params.InputResponses, "confirm") != nil {
			return nil, nil
		}
		return NewInputRequestBuilder("step=1").
			Elicit("confirm", mcp.ElicitationParams{
				Mode:    mcp.ElicitationModeForm,
				Message: "Continue?",
				RequestedSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{"confirmed": map[string]any{"type": "boolean"}},
				},
			}).
			ToolResult(), nil
	})

	session := newMRTRSession("legacy")
	session.response = &mcp.ElicitationResult{
		ElicitationResponse: mcp.ElicitationResponse{
			Action:  mcp.ElicitationResponseActionAccept,
			Content: map[string]any{"confirmed": true},
		},
	}
	ctx := srv.WithContext(t.Context(), session)
	ctx = WithRequestProtocolInfo(ctx, &RequestProtocolInfo{})

	result, reqErr := srv.handleToolCall(ctx, 1, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "askThenFail"},
	})
	assert.Nil(t, result)
	require.NotNil(t, reqErr)
	assert.Equal(t, mcp.INTERNAL_ERROR, reqErr.code)
	assert.EqualError(t, reqErr.err, "tool 'askThenFail' handler returned no result")
	assert.Equal(t, 1, session.calls, "the server should have elicited on the handler's behalf")
}

// Run as a task, the same handler used to complete the task with a nil
// result, and tasks/result then panicked reading it. The task fails instead.
func TestTaskNilResultFailsTheTask(t *testing.T) {
	tests := []struct {
		name     string
		register func(srv *MCPServer)
	}{
		{
			name: "task tool",
			register: func(srv *MCPServer) {
				srv.AddTaskTool(mcp.NewTool("broken", mcp.WithTaskSupport(mcp.TaskSupportRequired)),
					func(context.Context, mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
						return nil, nil
					})
			},
		},
		{
			name: "regular tool run as a task",
			register: func(srv *MCPServer) {
				srv.AddTool(mcp.NewTool("broken", mcp.WithTaskSupport(mcp.TaskSupportOptional)),
					func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						return nil, nil
					})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewMCPServer("test-server", "1.0.0", WithTaskCapabilities(true, true, true))
			tt.register(srv)

			created, reqErr := srv.handleToolCall(t.Context(), 1, mcp.CallToolRequest{
				Params: mcp.CallToolParams{Name: "broken", Task: &mcp.TaskParams{}},
			})
			require.Nil(t, reqErr)
			taskID := created.(*mcp.CreateTaskResult).Task.TaskId

			var result *mcp.TaskResultResult
			require.NotPanics(t, func() {
				result, reqErr = srv.handleTaskResult(t.Context(), 2, mcp.TaskResultRequest{
					Params: mcp.TaskResultParams{TaskId: taskID},
				})
			})
			assert.Nil(t, result)
			require.NotNil(t, reqErr)
			assert.Equal(t, mcp.INTERNAL_ERROR, reqErr.code)
			assert.EqualError(t, reqErr.err, "tool 'broken' handler returned no result")

			task, _, err := srv.getTask(t.Context(), taskID)
			require.NoError(t, err)
			assert.Equal(t, mcp.TaskStatusFailed, task.Status)
		})
	}
}
