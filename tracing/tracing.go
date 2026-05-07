// Package tracing defines the tracing interfaces used by the mcp-go client
// and server. Concrete implementations live in adapter modules; an
// OpenTelemetry adapter ships at github.com/mark3labs/mcp-go/otel.
package tracing

import (
	"context"
	"net/http"
)

// SpanKind identifies the role of a span in a trace.
type SpanKind int

const (
	SpanKindUnspecified SpanKind = iota
	SpanKindServer
	SpanKindClient
	SpanKindInternal
)

// StatusCode describes the final outcome status of a span.
type StatusCode int

const (
	StatusUnset StatusCode = iota
	StatusOK
	StatusError
)

// Attribute is a string key/value pair attached to a span.
type Attribute struct {
	Key, Value string
}

// String returns an Attribute with the given key and value.
func String(key, value string) Attribute {
	return Attribute{Key: key, Value: value}
}

// Tracer starts spans. Start returns a context carrying the new span and the
// span itself.
type Tracer interface {
	Start(ctx context.Context, name string, kind SpanKind, attrs ...Attribute) (context.Context, Span)
}

// Span is an in-flight tracing span. End must be called exactly once.
type Span interface {
	SetAttributes(attrs ...Attribute)
	RecordError(err error)
	SetStatus(code StatusCode, description string)
	End()
}

// Propagator carries tracing context across HTTP requests.
type Propagator interface {
	Inject(ctx context.Context, headers http.Header)
	Extract(ctx context.Context, headers http.Header) context.Context
}

type spanContextKey struct{}

// ContextWithSpan returns ctx annotated with span so SpanFromContext can
// later retrieve it.
func ContextWithSpan(ctx context.Context, span Span) context.Context {
	return context.WithValue(ctx, spanContextKey{}, span)
}

// SpanFromContext returns the active Span, or a non-recording noop if none
// is present.
func SpanFromContext(ctx context.Context) Span {
	if s, ok := ctx.Value(spanContextKey{}).(Span); ok {
		return s
	}
	return noopSpan{}
}

// NoopTracer returns a Tracer that records nothing.
func NoopTracer() Tracer { return noopTracer{} }

// NoopPropagator returns a Propagator whose methods are no-ops.
func NoopPropagator() Propagator { return noopPropagator{} }

type noopTracer struct{}

func (noopTracer) Start(ctx context.Context, _ string, _ SpanKind, _ ...Attribute) (context.Context, Span) {
	return ctx, noopSpan{}
}

type noopSpan struct{}

func (noopSpan) SetAttributes(...Attribute)   {}
func (noopSpan) RecordError(error)            {}
func (noopSpan) SetStatus(StatusCode, string) {}
func (noopSpan) End()                         {}

type noopPropagator struct{}

func (noopPropagator) Inject(context.Context, http.Header)                       {}
func (noopPropagator) Extract(ctx context.Context, _ http.Header) context.Context { return ctx }
