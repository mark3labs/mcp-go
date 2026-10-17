package otel

import (
	"context"

	"go.opentelemetry.io/otel/propagation"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/tracing"
)

// NewMetaPropagator returns a tracing.MetaPropagator backed by W3C TraceContext
// that reads and writes trace context in the MCP _meta property bag per SEP-414.
func NewMetaPropagator() tracing.MetaPropagator {
	return WrapMetaPropagator(propagation.TraceContext{})
}

// WrapMetaPropagator adapts any TextMapPropagator as a tracing.MetaPropagator
// that operates on the MCP _meta property bag.
func WrapMetaPropagator(p propagation.TextMapPropagator) tracing.MetaPropagator {
	if p == nil {
		return tracing.NoopMetaPropagator()
	}
	return otelMetaPropagator{p: p}
}

type otelMetaPropagator struct {
	p propagation.TextMapPropagator
}

func (o otelMetaPropagator) InjectMeta(ctx context.Context, meta *mcp.Meta) *mcp.Meta {
	if meta == nil {
		meta = &mcp.Meta{}
	}
	o.p.Inject(ctx, &metaCarrier{meta: meta})
	return meta
}

func (o otelMetaPropagator) ExtractMeta(ctx context.Context, meta *mcp.Meta) context.Context {
	if meta == nil {
		return ctx
	}
	return o.p.Extract(ctx, &metaCarrier{meta: meta})
}

// metaCarrier adapts mcp.Meta.AdditionalFields as a propagation.TextMapCarrier.
type metaCarrier struct {
	meta *mcp.Meta
}

func (c *metaCarrier) Get(key string) string {
	if c.meta.AdditionalFields == nil {
		return ""
	}
	v, ok := c.meta.AdditionalFields[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func (c *metaCarrier) Set(key string, value string) {
	if c.meta.AdditionalFields == nil {
		c.meta.AdditionalFields = make(map[string]any)
	}
	c.meta.AdditionalFields[key] = value
}

func (c *metaCarrier) Keys() []string {
	keys := make([]string, 0, len(c.meta.AdditionalFields))
	for k := range c.meta.AdditionalFields {
		keys = append(keys, k)
	}
	return keys
}
