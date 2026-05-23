package otel

import (
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/server"
)

// WithServerMetrics installs an OpenTelemetry meter on the server. The
// server emits mcp.request.calls / mcp.request.duration and
// mcp.tool.calls / mcp.tool.duration; see server.WithMeter for the full
// attribute schema.
func WithServerMetrics(m otelmetric.Meter) server.ServerOption {
	return server.WithMeter(NewMeter(m))
}

// WithServerTracing installs an OpenTelemetry tracer and a W3C TraceContext
// propagator on the server.
func WithServerTracing(t trace.Tracer) server.ServerOption {
	tracer := NewTracer(t)
	propagator := NewPropagator()
	return func(s *server.MCPServer) {
		server.WithTracer(tracer)(s)
		server.WithPropagator(propagator)(s)
	}
}

// WithServerTracingPropagator is WithServerTracing with a caller-supplied
// TextMapPropagator.
func WithServerTracingPropagator(t trace.Tracer, p propagation.TextMapPropagator) server.ServerOption {
	tracer := NewTracer(t)
	propagator := WrapPropagator(p)
	return func(s *server.MCPServer) {
		server.WithTracer(tracer)(s)
		server.WithPropagator(propagator)(s)
	}
}

// WithClientTracing installs an OpenTelemetry tracer and a W3C TraceContext
// propagator on the client.
func WithClientTracing(t trace.Tracer) client.ClientOption {
	tracer := NewTracer(t)
	propagator := NewPropagator()
	return func(c *client.Client) {
		client.WithTracer(tracer)(c)
		client.WithPropagator(propagator)(c)
	}
}

// WithClientTracingPropagator is WithClientTracing with a caller-supplied
// TextMapPropagator.
func WithClientTracingPropagator(t trace.Tracer, p propagation.TextMapPropagator) client.ClientOption {
	tracer := NewTracer(t)
	propagator := WrapPropagator(p)
	return func(c *client.Client) {
		client.WithTracer(tracer)(c)
		client.WithPropagator(propagator)(c)
	}
}
