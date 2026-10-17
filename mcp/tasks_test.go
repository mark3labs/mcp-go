package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRelatedTaskMeta(t *testing.T) {
	taskID := "task-123"
	meta := RelatedTaskMeta(taskID)

	assert.NotNil(t, meta)
	assert.Equal(t, taskID, meta["taskId"])
	assert.Len(t, meta, 1)
}

func TestWithRelatedTask(t *testing.T) {
	taskID := "task-456"
	meta := WithRelatedTask(taskID)

	assert.NotNil(t, meta)
	assert.NotNil(t, meta.AdditionalFields)

	// Check that the related task metadata is properly nested
	relatedTask, ok := meta.AdditionalFields[RelatedTaskMetaKey]
	assert.True(t, ok, "RelatedTaskMetaKey should exist in AdditionalFields")

	relatedTaskMap, ok := relatedTask.(map[string]any)
	assert.True(t, ok, "Related task metadata should be a map[string]any")
	assert.Equal(t, taskID, relatedTaskMap["taskId"])
}

func TestRelatedTaskMetaKey(t *testing.T) {
	// Verify the constant matches the spec
	assert.Equal(t, "io.modelcontextprotocol/related-task", RelatedTaskMetaKey)
}

func TestWithModelImmediateResponse(t *testing.T) {
	message := "Processing your request. This may take a few minutes."
	meta := WithModelImmediateResponse(message)

	assert.NotNil(t, meta)
	assert.NotNil(t, meta.AdditionalFields)

	// Check that the immediate response message is properly set
	immediateResponse, ok := meta.AdditionalFields[ModelImmediateResponseMetaKey]
	assert.True(t, ok, "ModelImmediateResponseMetaKey should exist in AdditionalFields")

	responseMsg, ok := immediateResponse.(string)
	assert.True(t, ok, "Immediate response should be a string")
	assert.Equal(t, message, responseMsg)
}

func TestModelImmediateResponseMetaKey(t *testing.T) {
	// Verify the constant matches the spec
	assert.Equal(t, "io.modelcontextprotocol/model-immediate-response", ModelImmediateResponseMetaKey)
}

func TestCreateTaskResultWithModelImmediateResponse(t *testing.T) {
	// Test that CreateTaskResult can include model immediate response metadata
	task := NewTask("task-789")
	message := "Your request is being processed in the background."

	result := CreateTaskResult{
		Task: task,
		Result: Result{
			Meta: WithModelImmediateResponse(message),
		},
	}

	assert.Equal(t, task.TaskId, result.Task.TaskId)
	assert.NotNil(t, result.Meta)
	assert.NotNil(t, result.Meta.AdditionalFields)

	immediateResponse, ok := result.Meta.AdditionalFields[ModelImmediateResponseMetaKey]
	assert.True(t, ok)
	assert.Equal(t, message, immediateResponse)
}
