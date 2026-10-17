# OpenTelemetry Tracing

End-to-end OpenTelemetry tracing across an mcp-go server and client, wired
through the `github.com/mark3labs/mcp-go/otel` submodule.

## What this demonstrates

- `otel.WithServerTracing(tracer)` installs an OpenTelemetry-backed tracer on
  the server plus a W3C TraceContext propagator. The server emits
  `mcp.<method>` server spans for every dispatched JSON-RPC method, plus a
  `tool.<name>` child span around each tool invocation.
- `otel.WithClientTracing(tracer)` installs the same on the client. Outgoing
  spans are `mcp.<method>` client-kind, and the client injects W3C
  `traceparent` into the request headers.
- The streamable-HTTP transport carries the `traceparent` header so the
  server middleware extracts it and the server span chains as a child of the
  client span — same trace ID end-to-end.

## Running

```bash
go run ./otel/example
```

Spans are printed to stderr via a tiny inline `SpanProcessor` so the
example has no exporter dependency. To send spans to a real OTLP
collector, replace the processor with a batcher around the OTLP
exporter:

```go
exp, err := otlptracegrpc.New(ctx)
if err != nil {
    log.Fatal(err)
}
tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp))
```

See <https://opentelemetry.io/docs/languages/go/exporters/>.

## Span attributes

| Attribute              | Where             | Description                         |
| ---------------------- | ----------------- | ----------------------------------- |
| `mcp.method`           | every span        | JSON-RPC method name                |
| `mcp.tool.name`        | `tools/call` only | tool being invoked                  |
| `mcp.session.id`       | server spans      | client session ID, when available   |
| `mcp.protocol.version` | server spans      | `Mcp-Protocol-Version` header value |

## Custom propagator

The default propagator is W3C TraceContext. Pass a composite (e.g.
TraceContext + Baggage) with `otel.WithServerTracingPropagator` /
`otel.WithClientTracingPropagator`.

## Limitations

stdio transports do not carry trace context today. Spans created over stdio
start as roots — there is nowhere on the JSON-RPC envelope to attach
`traceparent` without a spec change.
