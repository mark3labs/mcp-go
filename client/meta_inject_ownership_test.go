package client

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// traceparentMetaPropagator writes a traceparent into the meta it is handed,
// like a propagator backed by an active span would.
type traceparentMetaPropagator struct{ value string }

func (p traceparentMetaPropagator) InjectMeta(_ context.Context, meta *mcp.Meta) *mcp.Meta {
	if meta == nil {
		meta = &mcp.Meta{}
	}
	meta.SetMetaField("traceparent", p.value)
	return meta
}

func (traceparentMetaPropagator) ExtractMeta(ctx context.Context, _ *mcp.Meta) context.Context {
	return ctx
}

func TestInjectMeta_DoesNotModifyCallerMeta(t *testing.T) {
	c := NewClient(
		transport.NewInProcessTransport(server.NewMCPServer("srv", "1")),
		WithMetaPropagator(traceparentMetaPropagator{value: "00-trace-span-01"}),
	)

	supplied := &mcp.Meta{
		ProgressToken:    "tok",
		AdditionalFields: map[string]any{"custom": "value"},
	}

	got := c.injectMeta(t.Context(), supplied)

	require.NotNil(t, got)
	assert.Equal(t, "00-trace-span-01", got.AdditionalFields["traceparent"])
	assert.Equal(t, "value", got.AdditionalFields["custom"])
	assert.Equal(t, mcp.ProgressToken("tok"), got.ProgressToken)

	assert.NotContains(t, supplied.AdditionalFields, "traceparent",
		"the caller's _meta must not receive the injected trace context")
	assert.Equal(t, map[string]any{"custom": "value"}, supplied.AdditionalFields)
}
