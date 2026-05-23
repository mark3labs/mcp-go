package server

import (
	"context"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/metrics"
)

const (
	metricRequestCalls     = "mcp.request.calls"
	metricRequestDuration  = "mcp.request.duration"
	metricToolCalls        = "mcp.tool.calls"
	metricToolDuration     = "mcp.tool.duration"
	attrKeyMethod          = "mcp.method"
	attrKeyToolName        = "mcp.tool.name"
	attrKeySessionID       = "mcp.session.id"
	attrKeyProtocolVersion = "mcp.protocol.version"
	attrKeyOutcome         = "outcome"

	metricOutcomeOK          = "ok"
	metricOutcomeError       = "error"
	metricOutcomeErrorResult = "error_result"
)

// WithMeter installs a metric meter on the server. The server emits:
//
//   - mcp.request.calls (counter, unit "{call}") with attributes
//     mcp.method, mcp.session.id (when set), mcp.protocol.version
//     (from the Mcp-Protocol-Version header), outcome (ok|error).
//   - mcp.request.duration (histogram, unit "s") with the same attributes.
//   - mcp.tool.calls (counter, unit "{call}") with attributes
//     mcp.tool.name, outcome (ok|error|error_result).
//   - mcp.tool.duration (histogram, unit "s") with the same attributes.
//
// Histogram observations attach exemplars carrying the active span's
// TraceID/SpanID when a Tracer is installed via WithTracer, enabling
// "click latency bucket → jump to trace" pivots in Grafana / Tempo.
// A nil meter is treated as a no-op.
func WithMeter(meter metrics.Meter) ServerOption {
	if meter == nil {
		meter = metrics.NoopMeter()
	}
	return func(s *MCPServer) {
		s.meter = meter
		s.requestCallsCounter = meter.Counter(metricRequestCalls,
			"Number of JSON-RPC requests dispatched by the MCP server.", "{call}")
		s.requestDurationHistogram = meter.Histogram(metricRequestDuration,
			"Duration of JSON-RPC requests dispatched by the MCP server.", "s")
		toolCalls := meter.Counter(metricToolCalls,
			"Number of MCP tool handler invocations.", "{call}")
		toolDuration := meter.Histogram(metricToolDuration,
			"Duration of MCP tool handler invocations.", "s")
		s.toolMiddlewareMu.Lock()
		s.toolHandlerMiddlewares = append(s.toolHandlerMiddlewares,
			toolMetricsMiddleware(toolCalls, toolDuration))
		s.toolMiddlewareMu.Unlock()
	}
}

// startMessageMetric opens a per-request measurement scope and returns a
// finalizer that records the request counter + duration histogram keyed by
// the method outcome. When no meter is installed the finalizer is a no-op.
func (s *MCPServer) startMessageMetric(
	ctx context.Context,
	method string,
	protocolVersion string,
) func(mcp.JSONRPCMessage) {
	calls := s.requestCallsCounter
	duration := s.requestDurationHistogram
	if calls == nil || duration == nil {
		return func(mcp.JSONRPCMessage) {}
	}
	start := time.Now()

	attrs := []metrics.Attribute{metrics.String(attrKeyMethod, method)}
	if session := ClientSessionFromContext(ctx); session != nil {
		if id := session.SessionID(); id != "" {
			attrs = append(attrs, metrics.String(attrKeySessionID, id))
		}
	}
	if protocolVersion != "" {
		attrs = append(attrs, metrics.String(attrKeyProtocolVersion, protocolVersion))
	}

	return func(resp mcp.JSONRPCMessage) {
		outcome := metricOutcomeOK
		if _, ok := resp.(mcp.JSONRPCError); ok {
			outcome = metricOutcomeError
		}
		final := append(attrs[:len(attrs):len(attrs)],
			metrics.String(attrKeyOutcome, outcome),
		)
		calls.Add(ctx, 1, final...)
		duration.Record(ctx, time.Since(start).Seconds(), final...)
	}
}

func toolMetricsMiddleware(calls metrics.Counter, duration metrics.Histogram) ToolHandlerMiddleware {
	return func(next ToolHandlerFunc) ToolHandlerFunc {
		return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			start := time.Now()
			result, err := next(ctx, request)
			outcome := metricOutcomeOK
			switch {
			case err != nil:
				outcome = metricOutcomeError
			case result != nil && result.IsError:
				outcome = metricOutcomeErrorResult
			}
			attrs := []metrics.Attribute{
				metrics.String(attrKeyToolName, request.Params.Name),
				metrics.String(attrKeyOutcome, outcome),
			}
			calls.Add(ctx, 1, attrs...)
			duration.Record(ctx, time.Since(start).Seconds(), attrs...)
			return result, err
		}
	}
}
