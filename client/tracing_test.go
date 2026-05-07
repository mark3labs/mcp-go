package client

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mark3labs/mcp-go/tracing"
)

type headerCapturingTransport struct {
	transport.Interface
	last http.Header
}

func (h *headerCapturingTransport) SendRequest(ctx context.Context, request transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	h.last = request.Header.Clone()
	return h.Interface.SendRequest(ctx, request)
}

func TestWithTracer_NilFallsBackToNoop(t *testing.T) {
	c := NewClient(transport.NewInProcessTransport(server.NewMCPServer("srv", "1")), WithTracer(nil))
	require.NotNil(t, c.tracer)
}

func TestWithPropagator_NilFallsBackToNoop(t *testing.T) {
	c := NewClient(transport.NewInProcessTransport(server.NewMCPServer("srv", "1")), WithPropagator(nil))
	require.NotNil(t, c.propagator)
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

type headerInjectingPropagator struct{}

func (headerInjectingPropagator) Inject(_ context.Context, headers http.Header) {
	headers.Set("X-Test-Propagated", "yes")
}
func (headerInjectingPropagator) Extract(ctx context.Context, _ http.Header) context.Context {
	return ctx
}

func TestWithTracer_EmitsClientSpan(t *testing.T) {
	tr := &recordingTracer{}
	srv := server.NewMCPServer("trace-srv", "1.0")
	srv.AddTool(mcp.Tool{Name: "echo"}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	})

	c := NewClient(transport.NewInProcessTransport(srv), WithTracer(tr))
	require.NoError(t, c.Start(t.Context()))
	t.Cleanup(func() { _ = c.Close() })

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "1"}
	_, err := c.Initialize(t.Context(), initReq)
	require.NoError(t, err)

	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = "echo"
	_, err = c.CallTool(t.Context(), callReq)
	require.NoError(t, err)

	assert.Contains(t, tr.spans, "mcp.initialize")
	assert.Contains(t, tr.spans, "mcp.tools/call")
}

func TestWithPropagator_InjectsHeaders(t *testing.T) {
	srv := server.NewMCPServer("trace-srv", "1.0")
	wrapped := &headerCapturingTransport{Interface: transport.NewInProcessTransport(srv)}

	c := NewClient(wrapped, WithTracer(&recordingTracer{}), WithPropagator(headerInjectingPropagator{}))
	require.NoError(t, c.Start(t.Context()))
	t.Cleanup(func() { _ = c.Close() })

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "1"}
	_, err := c.Initialize(t.Context(), initReq)
	require.NoError(t, err)

	require.NotNil(t, wrapped.last)
	assert.Equal(t, "yes", wrapped.last.Get("X-Test-Propagated"))
}

func TestNoOption_DoesNotInjectHeaders(t *testing.T) {
	srv := server.NewMCPServer("trace-srv", "1.0")
	wrapped := &headerCapturingTransport{Interface: transport.NewInProcessTransport(srv)}

	c := NewClient(wrapped)
	require.NoError(t, c.Start(t.Context()))
	t.Cleanup(func() { _ = c.Close() })

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "1"}
	_, err := c.Initialize(t.Context(), initReq)
	require.NoError(t, err)

	assert.Empty(t, wrapped.last.Get("X-Test-Propagated"))
}
