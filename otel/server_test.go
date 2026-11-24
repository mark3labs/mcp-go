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
	"github.com/mark3labs/mcp-go/tracing"
)

const (
	attrMethod        = "mcp.method"
	attrMethodName    = "mcp.method.name"
	attrToolName      = "mcp.tool.name"
	attrGenAIToolName = "gen_ai.tool.name"
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
	for _, key := range []string{attrMethodName, attrMethod} {
		if v, ok := attrValue(parent, key); assert.True(t, ok, key) {
			assert.Equal(t, "tools/call", v)
		}
	}
	for _, key := range []string{attrGenAIToolName, attrToolName} {
		if v, ok := attrValue(parent, key); assert.True(t, ok, key) {
			assert.Equal(t, "echo", v)
		}
	}

	child := findSpan(spans, "tool.echo")
	require.NotNil(t, child, "missing tool.echo span")
	assert.Equal(t, trace.SpanKindInternal, child.SpanKind())
	assert.Equal(t, parent.SpanContext().SpanID(), child.Parent().SpanID())
	for _, key := range []string{attrMethodName, attrMethod} {
		_, ok := attrValue(child, key)
		assert.False(t, ok, key)
	}
	for _, key := range []string{attrGenAIToolName, attrToolName} {
		if v, ok := attrValue(child, key); assert.True(t, ok, key) {
			assert.Equal(t, "echo", v)
		}
	}
}

func TestServerTracing_RejectedToolCallsHaveToolName(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*server.MCPServer)
		errorCode int
	}{
		{
			name:      "unknown tool",
			configure: func(*server.MCPServer) {},
			errorCode: mcp.INVALID_PARAMS,
		},
		{
			name: "filtered tool",
			configure: func(mcpServer *server.MCPServer) {
				server.WithToolFilter(func(context.Context, []mcp.Tool) []mcp.Tool { return nil })(mcpServer)
				mcpServer.AddTool(mcp.NewTool("rejected"), nil)
			},
			errorCode: mcp.INVALID_PARAMS,
		},
		{
			name: "required task without task params",
			configure: func(mcpServer *server.MCPServer) {
				mcpServer.AddTaskTool(mcp.NewTool("rejected", mcp.WithTaskSupport(mcp.TaskSupportRequired)), nil)
			},
			errorCode: mcp.METHOD_NOT_FOUND,
		},
		{
			name: "task-only tool called synchronously",
			configure: func(mcpServer *server.MCPServer) {
				mcpServer.AddTaskTool(mcp.NewTool("rejected", mcp.WithTaskSupport(mcp.TaskSupportOptional)), nil)
			},
			errorCode: mcp.METHOD_NOT_FOUND,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tracer, recorder := newTestTracer(t)
			mcpServer := server.NewMCPServer("trace-srv", "1.0", otelmcp.WithServerTracing(tracer), server.WithToolCapabilities(false))
			test.configure(mcpServer)
			dispatchInitialized(t, mcpServer)
			since := spansSince(recorder)
			response := mcpServer.HandleMessage(t.Context(), []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"rejected","arguments":{}}}`))
			errorResponse, ok := response.(mcp.JSONRPCError)
			require.True(t, ok, "expected JSON-RPC error, got %T", response)
			assert.Equal(t, test.errorCode, errorResponse.Error.Code)

			spans := since()
			require.Len(t, spans, 1)
			span := spans[0]
			assert.Equal(t, "mcp.tools/call", span.Name())
			assert.Equal(t, trace.SpanKindServer, span.SpanKind())
			assert.Equal(t, codes.Error, span.Status().Code)
			for key, expected := range map[string]string{
				attrMethodName: "tools/call", attrMethod: "tools/call",
				attrGenAIToolName: "rejected", attrToolName: "rejected",
			} {
				value, ok := attrValue(span, key)
				require.True(t, ok, key)
				assert.Equal(t, expected, value, key)
			}
		})
	}
}

func TestServerTracing_NoopDoesNotAnnotateParent(t *testing.T) {
	tests := []struct {
		name    string
		options []server.ServerOption
	}{
		{name: "default"},
		{name: "nil tracer", options: []server.ServerOption{server.WithTracer(nil)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tracer, recorder := newTestTracer(t)
			ctx, parent := otelmcp.NewTracer(tracer).Start(t.Context(), "parent", tracing.SpanKindInternal)
			mcpServer := server.NewMCPServer("trace-srv", "1.0", test.options...)
			response := mcpServer.HandleMessage(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"missing"}}`))
			require.IsType(t, mcp.JSONRPCError{}, response)
			parent.End()
			spans := recorder.Ended()
			require.Len(t, spans, 1)
			for _, key := range []string{attrGenAIToolName, attrToolName} {
				_, ok := attrValue(spans[0], key)
				assert.False(t, ok, key)
			}
		})
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
