// otel demonstrates wiring OpenTelemetry tracing into an mcp-go server and
// client. Spans are printed to stderr; swap in an OTLP exporter to ship them.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http/httptest"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	otelmcp "github.com/mark3labs/mcp-go/otel"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(stderrPrinter{}))
	otel.SetTracerProvider(tp)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tp.Shutdown(shutdownCtx)
	}()

	tracer := otel.Tracer("github.com/mark3labs/mcp-go/otel/example")

	mcpServer := server.NewMCPServer(
		"otel-example", "1.0.0",
		server.WithToolCapabilities(false),
		otelmcp.WithServerTracing(tracer),
	)
	mcpServer.AddTool(mcp.NewTool("greet",
		mcp.WithDescription("Returns a greeting"),
		mcp.WithString("name", mcp.Required()),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, _ := req.RequireString("name")
		return mcp.NewToolResultText(fmt.Sprintf("hello, %s", name)), nil
	})

	httpSrv := httptest.NewServer(server.NewStreamableHTTPServer(mcpServer))
	defer httpSrv.Close()

	streamable, err := transport.NewStreamableHTTP(httpSrv.URL)
	if err != nil {
		log.Fatalf("create transport: %v", err)
	}
	mcpClient := client.NewClient(streamable, otelmcp.WithClientTracing(tracer))
	if err := mcpClient.Start(context.Background()); err != nil {
		log.Fatalf("start client: %v", err)
	}
	defer mcpClient.Close()

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "otel-example-client", Version: "1.0.0"}
	if _, err := mcpClient.Initialize(context.Background(), initReq); err != nil {
		log.Fatalf("initialize: %v", err)
	}

	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = "greet"
	callReq.Params.Arguments = map[string]any{"name": "trace"}
	res, err := mcpClient.CallTool(context.Background(), callReq)
	if err != nil {
		log.Fatalf("call tool: %v", err)
	}
	if len(res.Content) > 0 {
		if tc, ok := res.Content[0].(mcp.TextContent); ok {
			fmt.Fprintf(os.Stdout, "tool result: %s\n", tc.Text)
		}
	}
}

type stderrPrinter struct{}

func (stderrPrinter) OnStart(context.Context, sdktrace.ReadWriteSpan) {}

func (stderrPrinter) OnEnd(s sdktrace.ReadOnlySpan) {
	sc := s.SpanContext()
	parent := s.Parent().SpanID().String()
	fmt.Fprintf(os.Stderr, "span: %-22s kind=%s trace=%s span=%s parent=%s duration=%s\n",
		s.Name(), s.SpanKind(), sc.TraceID(), sc.SpanID(), parent, s.EndTime().Sub(s.StartTime()))
}

func (stderrPrinter) Shutdown(context.Context) error   { return nil }
func (stderrPrinter) ForceFlush(context.Context) error { return nil }
