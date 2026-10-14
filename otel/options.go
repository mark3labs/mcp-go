package otel

import (
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/server"
)

// WithServerTracing installs an OpenTelemetry tracer, a W3C TraceContext header
// propagator, and a W3C TraceContext _meta propagator (SEP-414) on the server.
func WithServerTracing(t trace.Tracer) server.ServerOption {
	tracer := NewTracer(t)
	propagator := NewPropagator()
	metaPropagator := NewMetaPropagator()
	return func(s *server.MCPServer) {
		server.WithTracer(tracer)(s)
		server.WithPropagator(propagator)(s)
		server.WithMetaPropagator(metaPropagator)(s)
	}
}

// WithServerTracingPropagator is WithServerTracing with a caller-supplied
// TextMapPropagator used for both HTTP header and _meta propagation.
func WithServerTracingPropagator(t trace.Tracer, p propagation.TextMapPropagator) server.ServerOption {
	tracer := NewTracer(t)
	propagator := WrapPropagator(p)
	metaPropagator := WrapMetaPropagator(p)
	return func(s *server.MCPServer) {
		server.WithTracer(tracer)(s)
		server.WithPropagator(propagator)(s)
		server.WithMetaPropagator(metaPropagator)(s)
	}
}

// WithServerMetaPropagator installs a _meta propagator (SEP-414) with a
// caller-supplied TextMapPropagator on the server.
func WithServerMetaPropagator(p propagation.TextMapPropagator) server.ServerOption {
	metaPropagator := WrapMetaPropagator(p)
	return server.WithMetaPropagator(metaPropagator)
}

// WithClientTracing installs an OpenTelemetry tracer, a W3C TraceContext header
// propagator, and a W3C TraceContext _meta propagator (SEP-414) on the client.
func WithClientTracing(t trace.Tracer) client.ClientOption {
	tracer := NewTracer(t)
	propagator := NewPropagator()
	metaPropagator := NewMetaPropagator()
	return func(c *client.Client) {
		client.WithTracer(tracer)(c)
		client.WithPropagator(propagator)(c)
		client.WithMetaPropagator(metaPropagator)(c)
	}
}

// WithClientTracingPropagator is WithClientTracing with a caller-supplied
// TextMapPropagator used for both HTTP header and _meta propagation.
func WithClientTracingPropagator(t trace.Tracer, p propagation.TextMapPropagator) client.ClientOption {
	tracer := NewTracer(t)
	propagator := WrapPropagator(p)
	metaPropagator := WrapMetaPropagator(p)
	return func(c *client.Client) {
		client.WithTracer(tracer)(c)
		client.WithPropagator(propagator)(c)
		client.WithMetaPropagator(metaPropagator)(c)
	}
}

// WithClientMetaPropagator installs a _meta propagator (SEP-414) with a
// caller-supplied TextMapPropagator on the client.
func WithClientMetaPropagator(p propagation.TextMapPropagator) client.ClientOption {
	metaPropagator := WrapMetaPropagator(p)
	return client.WithMetaPropagator(metaPropagator)
}
