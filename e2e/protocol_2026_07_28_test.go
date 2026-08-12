package e2e

import (
	"context"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// elicitOnce records the elicitation requests it is asked to fulfil and always
// accepts, answering with a fixed name.
type elicitOnce struct {
	calls atomic.Int32
	name  string
}

func (e *elicitOnce) Elicit(_ context.Context, request mcp.ElicitationRequest) (*mcp.ElicitationResult, error) {
	e.calls.Add(1)
	return &mcp.ElicitationResult{
		ElicitationResponse: mcp.ElicitationResponse{
			Action:  mcp.ElicitationResponseActionAccept,
			Content: map[string]any{"name": e.name},
		},
	}, nil
}

// newGreetServer builds a server whose greet tool asks the caller's client for
// a name before it can answer, using the multi round-trip pattern.
func newGreetServer(t *testing.T, opts ...server.ServerOption) *server.MCPServer {
	t.Helper()

	base := []server.ServerOption{
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, true),
		server.WithElicitation(),
		server.WithInstructions("greets people"),
	}
	srv := server.NewMCPServer("e2e-greet", "1.0.0", append(base, opts...)...)

	srv.AddTool(
		mcp.NewTool("greet", mcp.WithDescription("greets the user by name")),
		func(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if answer := server.ElicitationResponse(request.Params.InputResponses, "who"); answer != nil {
				content, _ := answer.Content.(map[string]any)
				name, _ := content["name"].(string)
				return mcp.NewToolResultText("hello " + name), nil
			}
			return server.NewInputRequestBuilder("awaiting-name").
				Elicit("who", mcp.ElicitationParams{
					Mode:    mcp.ElicitationModeForm,
					Message: "What is your name?",
					RequestedSchema: map[string]any{
						"type":       "object",
						"properties": map[string]any{"name": map[string]any{"type": "string"}},
					},
				}).
				ToolResult(), nil
		},
	)

	srv.AddTool(
		mcp.NewTool("echo", mcp.WithString("text", mcp.Description("what to echo"))),
		func(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText(request.GetString("text", "")), nil
		},
	)

	return srv
}

// startHTTPClient connects a client to a Streamable HTTP server and returns it
// initialized.
func startHTTPClient(t *testing.T, url string, opts ...client.ClientOption) (*client.Client, *mcp.InitializeResult) {
	t.Helper()

	httpTransport, err := transport.NewStreamableHTTP(url)
	require.NoError(t, err)

	c := client.NewClient(httpTransport, opts...)
	require.NoError(t, c.Start(t.Context()))
	t.Cleanup(func() { _ = c.Close() })

	var request mcp.InitializeRequest
	request.Params.ClientInfo = mcp.Implementation{Name: "e2e-client", Version: "1.0.0"}
	result, err := c.Initialize(t.Context(), request)
	require.NoError(t, err)

	return c, result
}

func TestE2E_ModernProtocol_NegotiatesViaDiscover(t *testing.T) {
	httpServer := httptest.NewServer(server.NewStreamableHTTPServer(newGreetServer(t)))
	defer httpServer.Close()

	c, result := startHTTPClient(t, httpServer.URL)

	// The connection settled on the stateless protocol core, without an
	// initialize handshake.
	assert.Equal(t, mcp.ProtocolVersion20260728, c.ProtocolVersion())
	assert.Equal(t, mcp.ProtocolVersion20260728, result.ProtocolVersion)
	assert.Equal(t, "e2e-greet", result.ServerInfo.Name)
	assert.Equal(t, "greets people", result.Instructions)
	assert.NotNil(t, result.Capabilities.Tools)
}

func TestE2E_ModernProtocol_ListAndCallTools(t *testing.T) {
	httpServer := httptest.NewServer(server.NewStreamableHTTPServer(
		newGreetServer(t, server.WithCacheHints(30000, mcp.CacheScopePublic)),
	))
	defer httpServer.Close()

	c, _ := startHTTPClient(t, httpServer.URL)

	tools, err := c.ListTools(t.Context(), mcp.ListToolsRequest{})
	require.NoError(t, err)
	require.Len(t, tools.Tools, 2)

	// List results carry the caching hints required from 2026-07-28.
	ttl, ok := tools.TTL()
	require.True(t, ok, "tools/list must carry a ttlMs hint")
	assert.Equal(t, int64(30000), ttl)
	assert.Equal(t, mcp.CacheScopePublic, tools.CacheScope)
	assert.True(t, tools.ResultType.IsComplete())

	var call mcp.CallToolRequest
	call.Params.Name = "echo"
	call.Params.Arguments = map[string]any{"text": "round trip"}

	echoed, err := c.CallTool(t.Context(), call)
	require.NoError(t, err)
	require.Len(t, echoed.Content, 1)
	assert.Equal(t, "round trip", echoed.Content[0].(mcp.TextContent).Text)
}

func TestE2E_ModernProtocol_MultiRoundTripIsTransparent(t *testing.T) {
	httpServer := httptest.NewServer(server.NewStreamableHTTPServer(newGreetServer(t)))
	defer httpServer.Close()

	handler := &elicitOnce{name: "Ada"}
	c, _ := startHTTPClient(t, httpServer.URL, client.WithElicitationHandler(handler))
	require.Equal(t, mcp.ProtocolVersion20260728, c.ProtocolVersion())

	var call mcp.CallToolRequest
	call.Params.Name = "greet"

	// The tool needs a name. The client fulfils the server's input request
	// with its elicitation handler and retries, all inside CallTool.
	result, err := c.CallTool(t.Context(), call)
	require.NoError(t, err)

	assert.False(t, result.NeedsInput(), "CallTool must return a final result")
	require.Len(t, result.Content, 1)
	assert.Equal(t, "hello Ada", result.Content[0].(mcp.TextContent).Text)
	assert.Equal(t, int32(1), handler.calls.Load(), "the client should have elicited exactly once")
}

func TestE2E_ModernProtocol_NoSessionState(t *testing.T) {
	httpServer := httptest.NewServer(server.NewStreamableHTTPServer(newGreetServer(t)))
	defer httpServer.Close()

	httpTransport, err := transport.NewStreamableHTTP(httpServer.URL)
	require.NoError(t, err)

	c := client.NewClient(httpTransport)
	require.NoError(t, c.Start(t.Context()))
	defer c.Close()

	var request mcp.InitializeRequest
	request.Params.ClientInfo = mcp.Implementation{Name: "e2e-client", Version: "1.0.0"}
	_, err = c.Initialize(t.Context(), request)
	require.NoError(t, err)

	// Protocol version 2026-07-28 has no protocol-level session, so nothing
	// was minted for the client to carry.
	assert.Empty(t, httpTransport.GetSessionId())

	// Requests still work: each one is self-describing, so any of them could
	// land on any instance behind a load balancer.
	for range 3 {
		_, err := c.ListTools(t.Context(), mcp.ListToolsRequest{})
		require.NoError(t, err)
	}
}

func TestE2E_LegacyClientAgainstModernServer(t *testing.T) {
	httpServer := httptest.NewServer(server.NewStreamableHTTPServer(newGreetServer(t)))
	defer httpServer.Close()

	// A client pinned to an earlier revision skips the discovery probe and
	// performs the initialize handshake instead.
	httpTransport, err := transport.NewStreamableHTTP(httpServer.URL)
	require.NoError(t, err)

	handler := &elicitOnce{name: "Grace"}
	c := client.NewClient(httpTransport,
		client.WithElicitationHandler(handler),
		client.WithProtocolVersion(mcp.ProtocolVersion20251125),
	)
	require.NoError(t, c.Start(t.Context()))
	defer c.Close()

	var request mcp.InitializeRequest
	request.Params.ClientInfo = mcp.Implementation{Name: "legacy-client", Version: "1.0.0"}
	result, err := c.Initialize(t.Context(), request)
	require.NoError(t, err)

	assert.Equal(t, mcp.ProtocolVersion20251125, result.ProtocolVersion)
	assert.NotEmpty(t, httpTransport.GetSessionId(), "legacy clients still get a session")

	// The same greet tool works: the server fulfils its own input request by
	// issuing the elicitation/create this client understands.
	var call mcp.CallToolRequest
	call.Params.Name = "greet"

	greeting, err := c.CallTool(t.Context(), call)
	require.NoError(t, err)
	require.Len(t, greeting.Content, 1)
	assert.Equal(t, "hello Grace", greeting.Content[0].(mcp.TextContent).Text)
	assert.Equal(t, int32(1), handler.calls.Load())
}

func TestE2E_ModernClientAgainstLegacyOnlyServer(t *testing.T) {
	// A deployment that depends on protocol-level session state pins itself to
	// the legacy revisions.
	httpServer := httptest.NewServer(server.NewStreamableHTTPServer(
		newGreetServer(t),
		server.WithStreamableHTTPProtocolVersions(mcp.LegacyProtocolVersions()...),
	))
	defer httpServer.Close()

	handler := &elicitOnce{name: "Alan"}
	c, result := startHTTPClient(t, httpServer.URL, client.WithElicitationHandler(handler))

	// The client probed with server/discover, was told the version it wanted
	// is unavailable, and negotiated down to the handshake.
	assert.Equal(t, mcp.LATEST_LEGACY_PROTOCOL_VERSION, result.ProtocolVersion)
	assert.Equal(t, mcp.LATEST_LEGACY_PROTOCOL_VERSION, c.ProtocolVersion())

	var call mcp.CallToolRequest
	call.Params.Name = "greet"

	greeting, err := c.CallTool(t.Context(), call)
	require.NoError(t, err)
	require.Len(t, greeting.Content, 1)
	assert.Equal(t, "hello Alan", greeting.Content[0].(mcp.TextContent).Text)
}

func TestE2E_ModernProtocol_InProcess(t *testing.T) {
	handler := &elicitOnce{name: "Edsger"}

	inProcess := transport.NewInProcessTransportWithOptions(
		newGreetServer(t),
		transport.WithElicitationHandler(handler),
	)

	c := client.NewClient(inProcess, client.WithElicitationHandler(handler))
	require.NoError(t, c.Start(t.Context()))
	defer c.Close()

	var request mcp.InitializeRequest
	request.Params.ClientInfo = mcp.Implementation{Name: "e2e-client", Version: "1.0.0"}
	result, err := c.Initialize(t.Context(), request)
	require.NoError(t, err)
	assert.Equal(t, mcp.ProtocolVersion20260728, result.ProtocolVersion)

	var call mcp.CallToolRequest
	call.Params.Name = "greet"

	greeting, err := c.CallTool(t.Context(), call)
	require.NoError(t, err)
	require.Len(t, greeting.Content, 1)
	assert.Equal(t, "hello Edsger", greeting.Content[0].(mcp.TextContent).Text)
}
