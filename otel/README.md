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

| Attribute              | Where                                  |
| ---------------------- | -------------------------------------- |
| `mcp.method.name`       | `mcp.<method>` request spans            |
| `gen_ai.tool.name`      | `tools/call` requests and tool handlers |
| `mcp.session.id`        | server request spans                   |
| `mcp.protocol.version`  | server request spans                   |

These follow the OpenTelemetry MCP semantic conventions. The earlier
`mcp.method` and the server's `mcp.tool.name` are still set alongside them
and are deprecated.

Tool name attributes contain the requested name verbatim, including on
rejected calls to unknown or filtered tools. Error status descriptions can
also contain these names.

See [example/](example/) for a runnable round-trip with stderr span output.
