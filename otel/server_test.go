package otel_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/mark3labs/mcp-go/mcp"
	otelmcp "github.com/mark3labs/mcp-go/otel"
	"github.com/mark3labs/mcp-go/server"
)

const (
	attrMethod   = "mcp.method"
	attrToolName = "mcp.tool.name"
)

func newTestTracer(t *testing.T) (trace.Tracer, *tracetest.SpanRecorder) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(t.Context()) })
	return tp.Tracer("test"), rec
}

func spansSince(rec *tracetest.SpanRecorder) func() []sdktrace.ReadOnlySpan {
	cursor := len(rec.Ended())
	return func() []sdktrace.ReadOnlySpan {
		ended := rec.Ended()
		return ended[cursor:]
	}
}

func attrValue(span sdktrace.ReadOnlySpan, key string) (string, bool) {
	for _, kv := range span.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.AsString(), true
		}
	}
	return "", false
}

func findSpan(spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	for _, s := range spans {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

func dispatchInitialized(t *testing.T, s *server.MCPServer) {
	t.Helper()
	initBody := fmt.Appendf(nil,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q,"clientInfo":{"name":"test","version":"1"},"capabilities":{}}}`,
		mcp.LATEST_PROTOCOL_VERSION,
	)
	resp := s.HandleMessage(t.Context(), initBody)
	require.NotNil(t, resp)
}

func TestServerTracing_ToolsCallEmitsParentAndChildSpans(t *testing.T) {
	tracer, rec := newTestTracer(t)

	s := server.NewMCPServer("trace-srv", "1.0", otelmcp.WithServerTracing(tracer))
	s.AddTool(mcp.Tool{Name: "echo"}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	})
	dispatchInitialized(t, s)
	since := spansSince(rec)

	body := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{}}}`)
	resp := s.HandleMessage(t.Context(), body)
	require.NotNil(t, resp)

	spans := since()
	require.Len(t, spans, 2, "expected one parent and one child span")

	parent := findSpan(spans, "mcp.tools/call")
	require.NotNil(t, parent, "missing mcp.tools/call span")
	assert.Equal(t, trace.SpanKindServer, parent.SpanKind())
	if v, ok := attrValue(parent, attrMethod); assert.True(t, ok) {
		assert.Equal(t, "tools/call", v)
	}
	if v, ok := attrValue(parent, attrToolName); assert.True(t, ok) {
		assert.Equal(t, "echo", v)
	}

	child := findSpan(spans, "tool.echo")
	require.NotNil(t, child, "missing tool.echo span")
	assert.Equal(t, trace.SpanKindInternal, child.SpanKind())
	assert.Equal(t, parent.SpanContext().SpanID(), child.Parent().SpanID())
	if v, ok := attrValue(child, attrToolName); assert.True(t, ok) {
		assert.Equal(t, "echo", v)
	}
}

func TestServerTracing_ToolsListEmitsServerSpan(t *testing.T) {
	tracer, rec := newTestTracer(t)

	s := server.NewMCPServer("trace-srv", "1.0",
		otelmcp.WithServerTracing(tracer),
		server.WithToolCapabilities(false),
	)
	s.AddTool(mcp.Tool{Name: "echo"}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	})
	dispatchInitialized(t, s)
	since := spansSince(rec)

	body := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	resp := s.HandleMessage(t.Context(), body)
	require.NotNil(t, resp)

	spans := since()
	require.Len(t, spans, 1)
	assert.Equal(t, "mcp.tools/list", spans[0].Name())
	assert.Equal(t, trace.SpanKindServer, spans[0].SpanKind())
}

func TestServerTracing_InitializeEmitsServerSpan(t *testing.T) {
	tracer, rec := newTestTracer(t)

	s := server.NewMCPServer("trace-srv", "1.0", otelmcp.WithServerTracing(tracer))

	dispatchInitialized(t, s)

	spans := rec.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "mcp.initialize", spans[0].Name())
	assert.Equal(t, trace.SpanKindServer, spans[0].SpanKind())
}

func TestServerTracing_ToolHandlerErrorMarksBothSpansErrored(t *testing.T) {
	tracer, rec := newTestTracer(t)

	s := server.NewMCPServer("trace-srv", "1.0", otelmcp.WithServerTracing(tracer))
	s.AddTool(mcp.Tool{Name: "boom"}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return nil, errors.New("kaboom")
	})
	dispatchInitialized(t, s)
	since := spansSince(rec)

	body := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"boom","arguments":{}}}`)
	s.HandleMessage(t.Context(), body)

	spans := since()
	child := findSpan(spans, "tool.boom")
	require.NotNil(t, child)
	assert.Equal(t, codes.Error, child.Status().Code,
		"tool.<name> child span must reflect handler error")

	parent := findSpan(spans, "mcp.tools/call")
	require.NotNil(t, parent)
	assert.Equal(t, codes.Error, parent.Status().Code,
		"mcp.tools/call parent span must reflect handler error via JSONRPCError envelope")
}

func TestServerTracing_ToolErrorResultMarksBothSpansErrored(t *testing.T) {
	tracer, rec := newTestTracer(t)

	s := server.NewMCPServer("trace-srv", "1.0", otelmcp.WithServerTracing(tracer))
	s.AddTool(mcp.Tool{Name: "softfail"}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultError("nope"), nil
	})
	dispatchInitialized(t, s)
	since := spansSince(rec)

	body := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"softfail","arguments":{}}}`)
	s.HandleMessage(t.Context(), body)

	spans := since()
	child := findSpan(spans, "tool.softfail")
	require.NotNil(t, child)
	assert.Equal(t, codes.Error, child.Status().Code,
		"tool.<name> child span must reflect IsError=true")

	parent := findSpan(spans, "mcp.tools/call")
	require.NotNil(t, parent)
	assert.Equal(t, codes.Error, parent.Status().Code,
		"mcp.tools/call parent span must also reflect the tool's logical failure")
}

func TestServerTracing_UnknownMethodMarksParentErrored(t *testing.T) {
	tracer, rec := newTestTracer(t)

	s := server.NewMCPServer("trace-srv", "1.0", otelmcp.WithServerTracing(tracer))
	dispatchInitialized(t, s)
	since := spansSince(rec)

	body := []byte(`{"jsonrpc":"2.0","id":2,"method":"does/not/exist"}`)
	resp := s.HandleMessage(t.Context(), body)
	require.NotNil(t, resp)

	spans := since()
	require.Len(t, spans, 1)
	assert.Equal(t, "mcp.does/not/exist", spans[0].Name())
	assert.Equal(t, codes.Error, spans[0].Status().Code)
}

