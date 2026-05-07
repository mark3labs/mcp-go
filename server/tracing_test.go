package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/tracing"
)

func TestWithTracer_NilFallsBackToNoop(t *testing.T) {
	s := NewMCPServer("trace-srv", "1.0", WithTracer(nil))
	require.NotNil(t, s.tracer)
}

func TestWithPropagator_NilFallsBackToNoop(t *testing.T) {
	s := NewMCPServer("trace-srv", "1.0", WithPropagator(nil))
	require.NotNil(t, s.propagator)
}

type recordingTracer struct {
	spans []string
}

func (r *recordingTracer) Start(ctx context.Context, name string, _ tracing.SpanKind, _ ...tracing.Attribute) (context.Context, tracing.Span) {
	r.spans = append(r.spans, name)
	return ctx, recordingSpan{}
}

type recordingSpan struct{}

func (recordingSpan) SetAttributes(...tracing.Attribute)   {}
func (recordingSpan) RecordError(error)                    {}
func (recordingSpan) SetStatus(tracing.StatusCode, string) {}
func (recordingSpan) End()                                 {}

func TestWithTracer_DispatcherStartsServerSpan(t *testing.T) {
	tr := &recordingTracer{}
	s := NewMCPServer("trace-srv", "1.0", WithTracer(tr))

	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + mcp.LATEST_PROTOCOL_VERSION + `","clientInfo":{"name":"test","version":"1"},"capabilities":{}}}`)
	resp := s.HandleMessage(t.Context(), body)
	require.NotNil(t, resp)
	assert.Contains(t, tr.spans, "mcp.initialize")
}

func TestWithTracer_RegistersToolMiddleware(t *testing.T) {
	tr := &recordingTracer{}
	s := NewMCPServer("trace-srv", "1.0", WithTracer(tr))
	s.AddTool(mcp.Tool{Name: "echo"}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	})

	initBody := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + mcp.LATEST_PROTOCOL_VERSION + `","clientInfo":{"name":"test","version":"1"},"capabilities":{}}}`)
	require.NotNil(t, s.HandleMessage(t.Context(), initBody))

	callBody := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{}}}`)
	require.NotNil(t, s.HandleMessage(t.Context(), callBody))

	assert.Contains(t, tr.spans, "mcp.tools/call")
	assert.Contains(t, tr.spans, "tool.echo")
}

