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
	"github.com/mark3labs/mcp-go/server/servertest"
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
	for _, key := range []string{attrMethodName, attrMethod} {
		if v, ok := attrValue(spans[0], key); assert.True(t, ok, key) {
			assert.Equal(t, "tools/call", v)
		}
	}
	toolName, ok := attrValue(spans[0], attrGenAIToolName)
	require.True(t, ok)
	assert.Equal(t, "echo", toolName)
	_, ok = attrValue(spans[0], attrToolName)
	assert.False(t, ok, "server tracing is disabled")
}

func TestClientTracing_ToolNameOnFailedCalls(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		protocol := "modern"
		if legacy {
			protocol = "legacy"
		}
		t.Run(protocol, func(t *testing.T) {
			tracer, recorder := newTestTracer(t)
			options := []client.ClientOption{otelmcp.WithClientTracing(tracer)}
			if legacy {
				options = append(options, client.WithLegacyProtocolOnly())
			}
			mcpServer := server.NewMCPServer("trace-srv", "1.0", server.WithToolCapabilities(false))
			mcpClient := client.NewClient(transport.NewInProcessTransport(mcpServer), options...)
			require.NoError(t, mcpClient.Start(t.Context()))
			t.Cleanup(func() { _ = mcpClient.Close() })
			initialize := mcp.InitializeRequest{}
			initialize.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
			initialize.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "1"}
			_, err := mcpClient.Initialize(t.Context(), initialize)
			require.NoError(t, err)

			_, err = mcpClient.ListTools(t.Context(), mcp.ListToolsRequest{})
			require.NoError(t, err)
			for _, span := range recorder.Ended() {
				_, ok := attrValue(span, attrGenAIToolName)
				assert.False(t, ok, span.Name())
			}

			for _, cancelled := range []bool{false, true} {
				name := "unknown tool"
				if cancelled {
					name = "cancelled request"
				}
				t.Run(name, func(t *testing.T) {
					ctx := t.Context()
					if cancelled {
						var cancel context.CancelFunc
						ctx, cancel = context.WithCancel(ctx)
						cancel()
					}
					since := spansSince(recorder)
					_, err := mcpClient.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "missing"}})
					require.Error(t, err)
					spans := since()
					require.Len(t, spans, 1)
					span := spans[0]
					assert.Equal(t, "mcp.tools/call", span.Name())
					assert.Equal(t, codes.Error, span.Status().Code)
					for key, expected := range map[string]string{
						attrMethodName: "tools/call", attrMethod: "tools/call", attrGenAIToolName: "missing",
					} {
						value, ok := attrValue(span, key)
						require.True(t, ok, key)
						assert.Equal(t, expected, value, key)
					}
				})
			}
		})
	}
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

	httpSrv := servertest.NewTestStreamableHTTPServer(srv)
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
	toolName, ok := attrValue(clientSpan, attrGenAIToolName)
	require.True(t, ok)
	assert.Equal(t, "echo", toolName)

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
