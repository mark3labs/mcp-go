package otel

import (
	"log/slog"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	otellog "go.opentelemetry.io/otel/log"

	"github.com/mark3labs/mcp-go/server"
)

// NewSlogLogger returns a *slog.Logger that emits records as OTEL log
// records through the provided LoggerProvider. The handler is the
// official OpenTelemetry slog bridge, so each record carries the active
// span's TraceID/SpanID automatically and the slog attributes become OTEL
// log record attributes.
//
// A nil provider falls back to OpenTelemetry's global log provider. The
// lookup is deferred to record-emission time by the bridge, so a provider
// registered after this call is still picked up.
func NewSlogLogger(provider otellog.LoggerProvider, scope string) *slog.Logger {
	var opts []otelslog.Option
	if provider != nil {
		opts = append(opts, otelslog.WithLoggerProvider(provider))
	}
	return slog.New(otelslog.NewHandler(scope, opts...))
}

// WithServerLogging installs an OTEL-bridged *slog.Logger on the server.
// Use this when you want the server's mcp.request / mcp.tool emission
// (see server.WithLogger) to flow through OTEL's log pipeline alongside
// the spans emitted via WithServerTracing.
//
// The scope is passed to the slog bridge as the OTEL instrumentation
// scope (typically "github.com/mark3labs/mcp-go" or the host service
// name). A nil provider falls back to the global OTEL log provider;
// the global lookup happens at record-emission time, so a provider
// registered after this option is applied is still picked up.
//
// Equivalent to:
//
//	server.WithLogger(otel.NewSlogLogger(provider, scope))
//
// Example:
//
//	srv := server.NewMCPServer("svc", "1.0",
//	    otel.WithServerTracing(tp.Tracer("mcp")),
//	    otel.WithServerLogging(lp, "mcp"),
//	)
//
// Callers using a non-OTEL logging stack should pass their own
// *slog.Logger to server.WithLogger directly instead.
func WithServerLogging(provider otellog.LoggerProvider, scope string) server.ServerOption {
	return server.WithLogger(NewSlogLogger(provider, scope))
}
