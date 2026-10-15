package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewMetaFromMapDoesNotMutateInput(t *testing.T) {
	input := map[string]any{"progressToken": "progress", "traceparent": "original"}
	meta := NewMetaFromMap(input)
	assert.Equal(t, "progress", input["progressToken"])
	meta.SetMetaField("traceparent", "changed")
	assert.Equal(t, "original", input["traceparent"])
	input["traceparent"] = "caller changed"
	assert.Equal(t, "changed", meta.GetMetaField("traceparent"))
	assert.Equal(t, "progress", meta.ProgressToken)
	assert.NotContains(t, meta.AdditionalFields, "progressToken")
}
