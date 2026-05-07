# mcp-go OpenTelemetry adapter

OpenTelemetry tracing for [`mcp-go`](https://github.com/mark3labs/mcp-go),
shipped as a separate Go module so the core has zero OpenTelemetry
dependencies.

```go
import otelmcp "github.com/mark3labs/mcp-go/otel"

srv := server.NewMCPServer("svc", "1.0", otelmcp.WithServerTracing(tp.Tracer("mcp")))
cli := client.NewClient(t, otelmcp.WithClientTracing(tp.Tracer("mcp")))
```

`WithServerTracing` / `WithClientTracing` install both an adapter tracer and
the W3C `TraceContext` propagator. Use `WithServerTracingPropagator` /
`WithClientTracingPropagator` to supply a custom `propagation.TextMapPropagator`
(e.g. a composite of `TraceContext{}` and `Baggage{}`).

Spans:

| Span name      | Kind     | Where             |
| -------------- | -------- | ----------------- |
| `mcp.<method>` | Server   | dispatcher        |
| `mcp.<method>` | Client   | outgoing requests |
| `tool.<name>`  | Internal | tool handler      |

Attributes (string-valued):

| Attribute              | Where             |
| ---------------------- | ----------------- |
| `mcp.method`           | every span        |
| `mcp.tool.name`        | `tools/call` only |
| `mcp.session.id`       | server spans      |
| `mcp.protocol.version` | server spans      |

See [example/](example/) for a runnable round-trip with stderr span output.
