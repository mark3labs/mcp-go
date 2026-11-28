package client

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mark3labs/mcp-go/tracing"
)

// namedSpan is a span that a namedSpanTracer places in the context.
type namedSpan struct {
	recordingSpan
	name string
}

// namedSpanTracer starts spans that spanNameMetaPropagator can identify.
type namedSpanTracer struct{}

func (namedSpanTracer) Start(ctx context.Context, name string, _ tracing.SpanKind, _ ...tracing.Attribute) (context.Context, tracing.Span) {
	span := namedSpan{name: name}
	return tracing.ContextWithSpan(ctx, span), span
}

// spanNameMetaPropagator writes the name of the context's active span into
// _meta, standing in for the span ID a W3C traceparent carries.
type spanNameMetaPropagator struct{}

func (spanNameMetaPropagator) InjectMeta(ctx context.Context, meta *mcp.Meta) *mcp.Meta {
	name := "none"
	if span, ok := tracing.SpanFromContext(ctx).(namedSpan); ok {
		name = span.name
	}
	if meta == nil {
		meta = &mcp.Meta{}
	}
	meta.SetMetaField("traceparent", name)
	return meta
}

func (spanNameMetaPropagator) ExtractMeta(ctx context.Context, _ *mcp.Meta) context.Context {
	return ctx
}

type paramsCapturingTransport struct {
	transport.Interface
	params map[string]json.RawMessage
}

func (p *paramsCapturingTransport) SendRequest(ctx context.Context, request transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	encoded, err := json.Marshal(request.Params)
	if err != nil {
		return nil, err
	}
	var params map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &params); err != nil {
		return nil, err
	}
	p.params = params
	return p.Interface.SendRequest(ctx, request)
}

func (p *paramsCapturingTransport) metaField(t *testing.T, key string) any {
	t.Helper()
	var meta map[string]any
	require.NoError(t, json.Unmarshal(p.params["_meta"], &meta))
	return meta[key]
}

func TestWithMetaPropagator_InjectsClientSpanContext(t *testing.T) {
	srv := server.NewMCPServer("trace-srv", "1.0")
	srv.AddTool(mcp.Tool{Name: "echo"}, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	})
	srv.AddPrompt(mcp.NewPrompt("greet"), func(context.Context, mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return mcp.NewGetPromptResult("greet", nil), nil
	})
	srv.AddResource(mcp.NewResource("test://doc", "doc"), func(context.Context, mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		return []mcp.ResourceContents{mcp.TextResourceContents{URI: "test://doc", Text: "hi"}}, nil
	})

	tests := []struct {
		method string
		send   func(c *Client, meta *mcp.Meta) error
	}{
		{
			method: string(mcp.MethodToolsCall),
			send: func(c *Client, meta *mcp.Meta) error {
				request := mcp.CallToolRequest{}
				request.Params.Name = "echo"
				request.Params.Meta = meta
				_, err := c.CallTool(t.Context(), request)
				return err
			},
		},
		{
			method: string(mcp.MethodPromptsGet),
			send: func(c *Client, meta *mcp.Meta) error {
				request := mcp.GetPromptRequest{}
				request.Params.Name = "greet"
				request.Params.Meta = meta
				_, err := c.GetPrompt(t.Context(), request)
				return err
			},
		},
		{
			method: string(mcp.MethodResourcesRead),
			send: func(c *Client, meta *mcp.Meta) error {
				request := mcp.ReadResourceRequest{}
				request.Params.URI = "test://doc"
				request.Params.Meta = meta
				_, err := c.ReadResource(t.Context(), request)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			wrapped := &paramsCapturingTransport{Interface: transport.NewInProcessTransport(srv)}
			c := NewClient(wrapped, WithTracer(namedSpanTracer{}), WithMetaPropagator(spanNameMetaPropagator{}))
			require.NoError(t, c.Start(t.Context()))
			t.Cleanup(func() { _ = c.Close() })

			initReq := mcp.InitializeRequest{}
			initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
			initReq.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "1"}
			_, err := c.Initialize(t.Context(), initReq)
			require.NoError(t, err)

			meta := &mcp.Meta{AdditionalFields: map[string]any{"tenant": "acme"}}
			require.NoError(t, tt.send(c, meta))

			// The server's span must be a child of the client's span for
			// this request, so _meta has to carry that span's context.
			assert.Equal(t, "mcp."+tt.method, wrapped.metaField(t, "traceparent"))
			assert.Equal(t, "acme", wrapped.metaField(t, "tenant"))
			assert.Equal(t, map[string]any{"tenant": "acme"}, meta.AdditionalFields)
		})
	}
}
