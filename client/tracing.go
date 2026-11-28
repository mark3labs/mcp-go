package client

import (
	"context"
	"maps"
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/tracing"
)

// attrMethodName is the OpenTelemetry MCP semantic convention name; the
// deprecated attrMethod is set alongside it.
const (
	attrMethodName    = "mcp.method.name"
	attrMethod        = "mcp.method"
	attrGenAIToolName = "gen_ai.tool.name"
)

// WithTracer installs a tracer on the client. The tracer starts a client-kind
// span around every outgoing JSON-RPC method ("mcp.<method>"). For end-to-end
// propagation install a Propagator with WithPropagator as well. A nil tracer
// is treated as a no-op.
func WithTracer(tracer tracing.Tracer) ClientOption {
	if tracer == nil {
		tracer = tracing.NoopTracer()
	}
	return func(c *Client) {
		c.tracer = tracer
	}
}

// WithPropagator installs a propagator that injects trace context into
// outgoing request headers. A nil propagator is treated as a no-op.
func WithPropagator(p tracing.Propagator) ClientOption {
	if p == nil {
		p = tracing.NoopPropagator()
	}
	return func(c *Client) {
		c.propagator = p
	}
}

// WithMetaPropagator installs a propagator that injects trace context into
// the MCP _meta property bag of outgoing requests (per SEP-414). It covers
// all transports including stdio, where HTTP headers are unavailable.
// A nil propagator is treated as a no-op.
func WithMetaPropagator(p tracing.MetaPropagator) ClientOption {
	if p == nil {
		p = tracing.NoopMetaPropagator()
	}
	return func(c *Client) {
		c.metaPropagator = p
	}
}

// injectTraceMeta writes the trace context of ctx into the _meta of
// tools/call, prompts/get and resources/read params. It writes into a copy of
// _meta, so the caller's request is left unchanged.
func (c *Client) injectTraceMeta(ctx context.Context, params any) any {
	p := c.metaPropagator
	if p == nil {
		return params
	}
	switch params := params.(type) {
	case mcp.CallToolParams:
		params.Meta = p.InjectMeta(ctx, copyMeta(params.Meta))
		return params
	case mcp.GetPromptParams:
		params.Meta = p.InjectMeta(ctx, copyMeta(params.Meta))
		return params
	case mcp.ReadResourceParams:
		params.Meta = p.InjectMeta(ctx, copyMeta(params.Meta))
		return params
	default:
		return params
	}
}

// copyMeta returns a copy of meta whose AdditionalFields can be written
// without changing meta's.
func copyMeta(meta *mcp.Meta) *mcp.Meta {
	if meta == nil {
		return nil
	}
	copied := *meta
	copied.AdditionalFields = maps.Clone(meta.AdditionalFields)
	return &copied
}

func (c *Client) startSendSpan(
	ctx context.Context,
	method string,
	header http.Header,
	toolName string,
) (context.Context, http.Header, tracing.Span) {
	tracer := c.tracer
	if tracer == nil {
		tracer = tracing.NoopTracer()
	}
	propagator := c.propagator
	if propagator == nil {
		propagator = tracing.NoopPropagator()
	}

	attributes := []tracing.Attribute{
		tracing.String(attrMethodName, method),
		tracing.String(attrMethod, method),
	}
	if method == string(mcp.MethodToolsCall) {
		attributes = append(attributes, tracing.String(attrGenAIToolName, toolName))
	}
	ctx, span := tracer.Start(ctx, "mcp."+method, tracing.SpanKindClient, attributes...)

	if header == nil {
		header = make(http.Header)
	} else {
		header = header.Clone()
	}
	propagator.Inject(ctx, header)

	return ctx, header, span
}

func endSendSpan(span tracing.Span, err error) {
	if err != nil {
		span.SetStatus(tracing.StatusError, err.Error())
		span.RecordError(err)
	}
	span.End()
}
