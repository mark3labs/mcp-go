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
