package otel_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	otelmcp "github.com/mark3labs/mcp-go/otel"
	"github.com/mark3labs/mcp-go/server"
)

const attrProtocolVersion = "mcp.protocol.version"

type headerCapturingTransport struct {
	transport.Interface
	last http.Header
}

func (h *headerCapturingTransport) SendRequest(ctx context.Context, request transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	h.last = request.Header.Clone()
	return h.Interface.SendRequest(ctx, request)
}

func newTracedInProcessClient(t *testing.T, mcpSrv *server.MCPServer, tracer trace.Tracer) (*client.Client, *headerCapturingTransport) {
	t.Helper()
	wrapped := &headerCapturingTransport{Interface: transport.NewInProcessTransport(mcpSrv)}
	c := client.NewClient(wrapped, otelmcp.WithClientTracing(tracer))
	require.NoError(t, c.Start(t.Context()))
	t.Cleanup(func() { _ = c.Close() })
	return c, wrapped
}

func TestClientTracing_EmitsClientSpan(t *testing.T) {
	tracer, rec := newTestTracer(t)

	srv := server.NewMCPServer("trace-srv", "1.0")
	srv.AddTool(mcp.Tool{Name: "echo"}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	})

	c, _ := newTracedInProcessClient(t, srv, tracer)

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "1"}
	_, err := c.Initialize(t.Context(), initReq)
	require.NoError(t, err)

	since := spansSince(rec)

	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = "echo"
	_, err = c.CallTool(t.Context(), callReq)
	require.NoError(t, err)

	spans := since()
	require.Len(t, spans, 1)
	assert.Equal(t, "mcp.tools/call", spans[0].Name())
	assert.Equal(t, trace.SpanKindClient, spans[0].SpanKind())
}

func TestClientTracing_InjectsTraceparent(t *testing.T) {
	tracer, _ := newTestTracer(t)

	srv := server.NewMCPServer("trace-srv", "1.0")
	c, captured := newTracedInProcessClient(t, srv, tracer)

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "1"}
	_, err := c.Initialize(t.Context(), initReq)
	require.NoError(t, err)

	require.NotNil(t, captured.last, "transport did not capture headers")
	assert.NotEmpty(t, captured.last.Get("traceparent"), "expected traceparent header to be injected")
}

func TestClientTracing_ErrorResponseMarksSpanErrored(t *testing.T) {
	tracer, rec := newTestTracer(t)

	srv := server.NewMCPServer("trace-srv", "1.0")
	c, _ := newTracedInProcessClient(t, srv, tracer)

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "1"}
	_, err := c.Initialize(t.Context(), initReq)
	require.NoError(t, err)
	since := spansSince(rec)

	listReq := mcp.ListToolsRequest{}
	_, err = c.ListTools(t.Context(), listReq)
	require.Error(t, err)

	spans := since()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
}

func TestClientTracing_NoOptionDoesNotInjectTraceparent(t *testing.T) {
	srv := server.NewMCPServer("trace-srv", "1.0")
	wrapped := &headerCapturingTransport{Interface: transport.NewInProcessTransport(srv)}
	c := client.NewClient(wrapped)
	require.NoError(t, c.Start(t.Context()))
	t.Cleanup(func() { _ = c.Close() })

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "1"}
	_, err := c.Initialize(t.Context(), initReq)
	require.NoError(t, err)

	assert.Empty(t, wrapped.last.Get("traceparent"))
}

func TestRoundTrip_TraceparentSpansClientAndServer(t *testing.T) {
	clientTracer, clientRec := newTestTracer(t)
	serverTracer, serverRec := newTestTracer(t)

	srv := server.NewMCPServer("trace-srv", "1.0", otelmcp.WithServerTracing(serverTracer))
	srv.AddTool(mcp.Tool{Name: "echo"}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	})

	httpSrv := server.NewTestStreamableHTTPServer(srv)
	t.Cleanup(httpSrv.Close)

	streamable, err := transport.NewStreamableHTTP(httpSrv.URL)
	require.NoError(t, err)
	c := client.NewClient(streamable, otelmcp.WithClientTracing(clientTracer))
	require.NoError(t, c.Start(t.Context()))
	t.Cleanup(func() { _ = c.Close() })

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "1"}
	_, err = c.Initialize(t.Context(), initReq)
	require.NoError(t, err)

	clientSince := spansSince(clientRec)
	serverSince := spansSince(serverRec)

	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = "echo"
	_, err = c.CallTool(t.Context(), callReq)
	require.NoError(t, err)

	clientSpans := clientSince()
	require.Len(t, clientSpans, 1)
	clientSpan := clientSpans[0]
	assert.Equal(t, "mcp.tools/call", clientSpan.Name())
	assert.Equal(t, trace.SpanKindClient, clientSpan.SpanKind())

	serverSpans := serverSince()
	parentMcp := findSpan(serverSpans, "mcp.tools/call")
	require.NotNil(t, parentMcp, "server should have produced an mcp.tools/call span")
	childTool := findSpan(serverSpans, "tool.echo")
	require.NotNil(t, childTool, "server should have produced a tool.echo span")

	assert.Equal(t, clientSpan.SpanContext().TraceID(), parentMcp.SpanContext().TraceID(),
		"server span should join the client trace")
	assert.Equal(t, clientSpan.SpanContext().SpanID(), parentMcp.Parent().SpanID(),
		"server span parent should be the client span")

	if v, ok := attrValue(parentMcp, attrProtocolVersion); assert.True(t, ok, "server span should carry the negotiated protocol version") {
		assert.Equal(t, mcp.LATEST_PROTOCOL_VERSION, v)
	}
}

