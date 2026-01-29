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
}

func TestWithRelatedTask(t *testing.T) {
	taskID := "task-456"
	meta := WithRelatedTask(taskID)

	assert.NotNil(t, meta)
	assert.NotNil(t, meta.AdditionalFields)

	// Verify the related task metadata is set correctly
	relatedTaskMeta, ok := meta.AdditionalFields[RelatedTaskMetaKey]
	assert.True(t, ok, "RelatedTaskMetaKey should exist in AdditionalFields")

	// The value should be a map with taskId
	metaMap, ok := relatedTaskMeta.(map[string]any)
	assert.True(t, ok, "Related task metadata should be a map")
	assert.Equal(t, taskID, metaMap["taskId"])
}

func TestRelatedTaskMetaKey(t *testing.T) {
	// Verify the constant matches the MCP specification
	assert.Equal(t, "io.modelcontextprotocol/related-task", RelatedTaskMetaKey)
}

func TestWithRelatedTask_Integration(t *testing.T) {
	// Test that WithRelatedTask can be used in a real scenario
	taskID := "integration-task-789"

	result := &CallToolResult{
		Result: Result{
			Meta: WithRelatedTask(taskID),
		},
		Content: []Content{
			TextContent{
				Type: "text",
				Text: "Task result",
			},
		},
	}

	assert.NotNil(t, result.Meta)
	assert.NotNil(t, result.Meta.AdditionalFields)

	relatedTaskMeta, ok := result.Meta.AdditionalFields[RelatedTaskMetaKey]
	assert.True(t, ok)

	metaMap := relatedTaskMeta.(map[string]any)
	assert.Equal(t, taskID, metaMap["taskId"])
}
