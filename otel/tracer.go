// Package otel adapts OpenTelemetry to the mcp-go tracing interfaces.
//
//	srv := server.NewMCPServer("svc", "1.0", otel.WithServerTracing(tp.Tracer("mcp")))
//	cli := client.NewClient(t, otel.WithClientTracing(tp.Tracer("mcp")))
package otel

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/mark3labs/mcp-go/tracing"
)

// NewTracer wraps an OpenTelemetry trace.Tracer as a tracing.Tracer. A nil
// tracer is treated as a no-op.
func NewTracer(t trace.Tracer) tracing.Tracer {
	if t == nil {
		return tracing.NoopTracer()
	}
	return otelTracer{tracer: t}
}

// NewPropagator returns a tracing.Propagator backed by W3C TraceContext.
func NewPropagator() tracing.Propagator {
	return WrapPropagator(propagation.TraceContext{})
}

// WrapPropagator adapts any TextMapPropagator as a tracing.Propagator.
func WrapPropagator(p propagation.TextMapPropagator) tracing.Propagator {
	if p == nil {
		return tracing.NoopPropagator()
	}
	return otelPropagator{p: p}
}

type otelTracer struct {
	tracer trace.Tracer
}

func (o otelTracer) Start(ctx context.Context, name string, kind tracing.SpanKind, attrs ...tracing.Attribute) (context.Context, tracing.Span) {
	opts := []trace.SpanStartOption{trace.WithSpanKind(toOTelKind(kind))}
	if len(attrs) > 0 {
		opts = append(opts, trace.WithAttributes(toOTelAttrs(attrs)...))
	}
	ctx, span := o.tracer.Start(ctx, name, opts...)
	wrapped := otelSpan{span: span}
	return tracing.ContextWithSpan(ctx, wrapped), wrapped
}

type otelSpan struct {
	span trace.Span
}

func (s otelSpan) SetAttributes(attrs ...tracing.Attribute) {
	s.span.SetAttributes(toOTelAttrs(attrs)...)
}

func (s otelSpan) RecordError(err error) { s.span.RecordError(err) }

func (s otelSpan) SetStatus(code tracing.StatusCode, description string) {
	s.span.SetStatus(toOTelStatus(code), description)
}

func (s otelSpan) End() { s.span.End() }

type otelPropagator struct {
	p propagation.TextMapPropagator
}

func (o otelPropagator) Inject(ctx context.Context, headers http.Header) {
	o.p.Inject(ctx, propagation.HeaderCarrier(headers))
}

func (o otelPropagator) Extract(ctx context.Context, headers http.Header) context.Context {
	return o.p.Extract(ctx, propagation.HeaderCarrier(headers))
}

func toOTelKind(k tracing.SpanKind) trace.SpanKind {
	switch k {
	case tracing.SpanKindServer:
		return trace.SpanKindServer
	case tracing.SpanKindClient:
		return trace.SpanKindClient
	case tracing.SpanKindInternal:
		return trace.SpanKindInternal
	default:
		return trace.SpanKindUnspecified
	}
}

func toOTelStatus(s tracing.StatusCode) codes.Code {
	switch s {
	case tracing.StatusOK:
		return codes.Ok
	case tracing.StatusError:
		return codes.Error
	default:
		return codes.Unset
	}
}

func toOTelAttrs(attrs []tracing.Attribute) []attribute.KeyValue {
	out := make([]attribute.KeyValue, len(attrs))
	for i, a := range attrs {
		out[i] = attribute.String(a.Key, a.Value)
	}
	return out
}
