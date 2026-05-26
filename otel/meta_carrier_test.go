package otel_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	otelmcp "github.com/mark3labs/mcp-go/otel"
	"github.com/mark3labs/mcp-go/server"
)

// paramsCapturingTransport records the raw params of the most recent outgoing request.
type paramsCapturingTransport struct {
	transport.Interface
	last json.RawMessage
}

func (p *paramsCapturingTransport) SendRequest(ctx context.Context, req transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	if req.Params != nil {
		raw, _ := json.Marshal(req.Params)
		p.last = raw
	}
	return p.Interface.SendRequest(ctx, req)
}

// startTracedClient builds a client over an in-process server with the given options,
// initializes it, and returns it alongside the capturing transport.
func startTracedClient(t *testing.T, srv *server.MCPServer, opts ...client.ClientOption) (*client.Client, *paramsCapturingTransport) {
	t.Helper()
	inner := transport.NewInProcessTransport(srv)
	cap := &paramsCapturingTransport{Interface: inner}
	c := client.NewClient(cap, opts...)
	require.NoError(t, c.Start(t.Context()))
	t.Cleanup(func() { _ = c.Close() })

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "1"}
	_, err := c.Initialize(t.Context(), initReq)
	require.NoError(t, err)
	return c, cap
}

// TestMetaPropagator_InjectCreatesTraceparent checks that injecting into a nil
// *mcp.Meta allocates one and sets traceparent.
func TestMetaPropagator_InjectCreatesTraceparent(t *testing.T) {
	p := otelmcp.WrapMetaPropagator(propagation.TraceContext{})

	tracer, _ := newTestTracer(t)
	ctx, span := tracer.Start(t.Context(), "parent", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	got := p.InjectMeta(ctx, nil)
	require.NotNil(t, got)
	require.NotNil(t, got.AdditionalFields)
	tp, ok := got.AdditionalFields["traceparent"]
	require.True(t, ok, "traceparent must be present in AdditionalFields")
	s, _ := tp.(string)
	assert.NotEmpty(t, s)
	assert.Equal(t, span.SpanContext().TraceID().String(), traceIDFromTraceparent(t, s))
}

// TestMetaPropagator_ExtractPopulatesContext checks that a traceparent in
// AdditionalFields is extracted into the returned context.
func TestMetaPropagator_ExtractPopulatesContext(t *testing.T) {
	tracer, rec := newTestTracer(t)
	ctx, root := tracer.Start(t.Context(), "root")
	defer root.End()

	// Inject so we have a well-formed traceparent.
	p := otelmcp.WrapMetaPropagator(propagation.TraceContext{})
	meta := p.InjectMeta(ctx, nil)
	root.End()
	since := spansSince(rec)

	// Extract into a fresh background ctx.
	extracted := p.ExtractMeta(t.Context(), meta)

	// Starting a span in the extracted ctx should parent to the injected span.
	_, child := tracer.Start(extracted, "child")
	child.End()

	spans := since()
	childSpan := findSpan(spans, "child")
	require.NotNil(t, childSpan)
	assert.Equal(t, root.SpanContext().TraceID(), childSpan.SpanContext().TraceID(),
		"child span should join the injected trace")
	assert.Equal(t, root.SpanContext().SpanID(), childSpan.Parent().SpanID(),
		"child span parent should be the injected span")
}

// TestMetaPropagator_NilEdgeCases checks nil-safety for both extract and inject.
func TestMetaPropagator_NilEdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		propagator     propagation.TextMapPropagator
		wantNonNilCtx  bool
		wantNilMeta    bool
	}{
		{
			name:          "extract nil meta returns non-nil context",
			propagator:    propagation.TraceContext{},
			wantNonNilCtx: true,
		},
		{
			name:        "nil propagator inject returns nil meta",
			propagator:  nil,
			wantNilMeta: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := otelmcp.WrapMetaPropagator(tc.propagator)
			if tc.wantNonNilCtx {
				ctx := p.ExtractMeta(t.Context(), nil)
				assert.NotNil(t, ctx)
			}
			if tc.wantNilMeta {
				meta := p.InjectMeta(t.Context(), nil)
				assert.Nil(t, meta)
			}
		})
	}
}

// TestClientMetaPropagator_CallToolInjectsMeta confirms the client injects
// traceparent into the outgoing tools/call _meta when a MetaPropagator is installed.
func TestClientMetaPropagator_CallToolInjectsMeta(t *testing.T) {
	tracer, _ := newTestTracer(t)

	srv := server.NewMCPServer("srv", "1.0")
	srv.AddTool(mcp.Tool{Name: "echo"}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	})

	c, cap := startTracedClient(t, srv, otelmcp.WithClientTracing(tracer))

	parentCtx, parentSpan := tracer.Start(t.Context(), "parent")
	defer parentSpan.End()

	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = "echo"
	_, err := c.CallTool(parentCtx, callReq)
	require.NoError(t, err)

	require.NotNil(t, cap.last)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(cap.last, &raw))

	metaRaw, ok := raw["_meta"]
	require.True(t, ok, "_meta must be present in outgoing params")
	var meta map[string]any
	require.NoError(t, json.Unmarshal(metaRaw, &meta))
	tp, ok := meta["traceparent"]
	require.True(t, ok, "traceparent must be present in _meta")
	tpStr, _ := tp.(string)
	assert.NotEmpty(t, tpStr)
	assert.Equal(t, parentSpan.SpanContext().TraceID().String(), traceIDFromTraceparent(t, tpStr))
}

// TestClientMetaPropagator_NoTracerDoesNotInjectMeta checks that with a
// propagator installed but no active span, no traceparent is injected into _meta.
func TestClientMetaPropagator_NoTracerDoesNotInjectMeta(t *testing.T) {
	tracer, _ := newTestTracer(t)
	srv := server.NewMCPServer("srv", "1.0")
	srv.AddTool(mcp.Tool{Name: "echo"}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	})

	// Propagator installed but context has no active span — must not inject an invalid traceparent.
	c, cap := startTracedClient(t, srv, otelmcp.WithClientTracing(tracer))

	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = "echo"
	_, err := c.CallTool(t.Context(), callReq)
	require.NoError(t, err)

	if cap.last != nil {
		var raw map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(cap.last, &raw))
		if metaRaw, ok := raw["_meta"]; ok {
			var meta map[string]any
			require.NoError(t, json.Unmarshal(metaRaw, &meta))
			assert.Empty(t, meta["traceparent"], "traceparent must not be injected without an active span")
		}
	}
}

// TestServerMetaPropagator_ExtractsInboundTraceparent verifies that the server
// joins the trace carried in _meta.traceparent (SEP-414 path, transport-agnostic).
func TestServerMetaPropagator_ExtractsInboundTraceparent(t *testing.T) {
	serverTracer, serverRec := newTestTracer(t)
	clientTracer, _ := newTestTracer(t)

	srv := server.NewMCPServer("srv", "1.0", otelmcp.WithServerTracing(serverTracer))
	srv.AddTool(mcp.Tool{Name: "echo"}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	})
	dispatchInitialized(t, srv)

	// Build the inbound traceparent from a real span.
	clientCtx, clientSpan := clientTracer.Start(t.Context(), "caller")
	defer clientSpan.End()

	p := otelmcp.WrapMetaPropagator(propagation.TraceContext{})
	meta := p.InjectMeta(clientCtx, nil)
	traceparent, _ := meta.AdditionalFields["traceparent"].(string)
	require.NotEmpty(t, traceparent)

	since := spansSince(serverRec)

	body := fmt.Appendf(nil,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{},"_meta":{"traceparent":%q}}}`,
		traceparent,
	)
	resp := srv.HandleMessage(t.Context(), body)
	require.NotNil(t, resp)

	spans := since()
	serverSpan := findSpan(spans, "mcp.tools/call")
	require.NotNil(t, serverSpan, "server must produce mcp.tools/call span")

	assert.Equal(t, clientSpan.SpanContext().TraceID(), serverSpan.SpanContext().TraceID(),
		"server span must join the trace from _meta.traceparent")
}

// TestServerMetaPropagator_RoundTrip verifies end-to-end: client injects into
// _meta, server extracts and server span joins the client trace.
func TestServerMetaPropagator_RoundTrip(t *testing.T) {
	clientTracer, _ := newTestTracer(t)
	serverTracer, serverRec := newTestTracer(t)

	srv := server.NewMCPServer("srv", "1.0", otelmcp.WithServerTracing(serverTracer))
	srv.AddTool(mcp.Tool{Name: "echo"}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	})

	c, _ := startTracedClient(t, srv, otelmcp.WithClientTracing(clientTracer))
	since := spansSince(serverRec)

	parentCtx, parentSpan := clientTracer.Start(t.Context(), "caller", trace.WithSpanKind(trace.SpanKindClient))
	defer parentSpan.End()

	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = "echo"
	_, err := c.CallTool(parentCtx, callReq)
	require.NoError(t, err)

	spans := since()
	serverSpan := findSpan(spans, "mcp.tools/call")
	require.NotNil(t, serverSpan, "server must produce mcp.tools/call span")

	assert.Equal(t, parentSpan.SpanContext().TraceID(), serverSpan.SpanContext().TraceID(),
		"server span must join the client trace propagated through _meta")
}

// traceIDFromTraceparent extracts the 32-hex trace-id segment from a W3C traceparent.
func traceIDFromTraceparent(t *testing.T, tp string) string {
	t.Helper()
	// format: 00-<32hex>-<16hex>-<2hex>
	parts := splitTraceparent(tp)
	require.Len(t, parts, 4, "malformed traceparent: %q", tp)
	return parts[1]
}

func splitTraceparent(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '-' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

