package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToolWithDeferLoading(t *testing.T) {
	tool := NewTool("deferred-tool",
		WithDescription("A tool with deferred loading"),
		WithDeferLoading(true),
	)

	data, err := json.Marshal(tool)
	assert.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	assert.NoError(t, err)

	assert.Equal(t, true, result["defer_loading"])
}

func TestToolWithoutDeferLoading(t *testing.T) {
	tool := NewTool("regular-tool",
		WithDescription("A regular tool"),
	)

	data, err := json.Marshal(tool)
	assert.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	assert.NoError(t, err)

	_, exists := result["defer_loading"]
	assert.False(t, exists)
}
