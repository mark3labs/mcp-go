package client

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInProcessClientWithOptions_WiresAllHostHandlers(t *testing.T) {
	sampling := &inProcessTestSamplingHandler{}
	elicitation := &inProcessTestElicitationHandler{}
	roots := &inProcessTestRootsHandler{}

	mcpServer := server.NewMCPServer(
		"test-server",
		"1.0.0",
		server.WithElicitation(),
		server.WithRoots(),
	)
	mcpServer.EnableSampling()
	mcpServer.AddTool(
		mcp.NewTool("inspect_host"),
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			samplingResult, err := mcpServer.RequestSampling(ctx, mcp.CreateMessageRequest{
				CreateMessageParams: mcp.CreateMessageParams{
					SystemPrompt: "inspect the workspace",
					MaxTokens:    64,
				},
			})
			if err != nil {
				return nil, err
			}
			elicitationResult, err := mcpServer.RequestElicitation(ctx, mcp.ElicitationRequest{
				Params: mcp.ElicitationParams{
					Message:         "confirm inspection",
					RequestedSchema: map[string]any{"type": "object"},
				},
			})
			if err != nil {
				return nil, err
			}
			rootsResult, err := mcpServer.RequestRoots(ctx, mcp.ListRootsRequest{
				Request: mcp.Request{Method: string(mcp.MethodListRoots)},
			})
			if err != nil {
				return nil, err
			}

			text := samplingResult.Content.(mcp.TextContent).Text + "|" +
				string(elicitationResult.Action) + "|" + rootsResult.Roots[0].URI
			return mcp.NewToolResultText(text), nil
		},
	)

	client, err := NewInProcessClientWithOptions(
		mcpServer,
		WithSamplingHandler(sampling),
		WithElicitationHandler(elicitation),
		WithRootsHandler(roots),
		WithMaxInputRoundTrips(3),
	)
	require.NoError(t, err)
	assert.Equal(t, 3, client.maxInputRoundTrips)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	require.NoError(t, client.Start(t.Context()))
	_, err = client.Initialize(t.Context(), mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcp.Implementation{Name: "test-client", Version: "1.0.0"},
		},
	})
	require.NoError(t, err)

	result, err := client.CallTool(t.Context(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "inspect_host"},
	})
	require.NoError(t, err)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Equal(t, "sampled|accept|file:///workspace", text.Text)
	assert.Equal(t, 1, sampling.calls)
	assert.Equal(t, 1, elicitation.calls)
	assert.Equal(t, 1, roots.calls)

	requestAssertions := []struct {
		name   string
		assert func(*testing.T)
	}{
		{
			name: "sampling request",
			assert: func(t *testing.T) {
				assert.Equal(t, "inspect the workspace", sampling.lastRequest.SystemPrompt)
				assert.Equal(t, 64, sampling.lastRequest.MaxTokens)
			},
		},
		{
			name: "elicitation request",
			assert: func(t *testing.T) {
				assert.Equal(t, "confirm inspection", elicitation.lastRequest.Params.Message)
				assert.Equal(t, map[string]any{"type": "object"}, elicitation.lastRequest.Params.RequestedSchema)
			},
		},
		{
			name: "roots request",
			assert: func(t *testing.T) {
				assert.Equal(t, string(mcp.MethodListRoots), roots.lastRequest.Method)
			},
		},
	}
	for _, tt := range requestAssertions {
		t.Run(tt.name, tt.assert)
	}
}

type inProcessTestSamplingHandler struct {
	calls       int
	lastRequest mcp.CreateMessageRequest
}

func (h *inProcessTestSamplingHandler) CreateMessage(
	_ context.Context,
	request mcp.CreateMessageRequest,
) (*mcp.CreateMessageResult, error) {
	h.calls++
	h.lastRequest = request
	return &mcp.CreateMessageResult{
		SamplingMessage: mcp.SamplingMessage{
			Role:    mcp.RoleAssistant,
			Content: mcp.NewTextContent("sampled"),
		},
		Model: "test-model",
	}, nil
}

type inProcessTestElicitationHandler struct {
	calls       int
	lastRequest mcp.ElicitationRequest
}

func (h *inProcessTestElicitationHandler) Elicit(
	_ context.Context,
	request mcp.ElicitationRequest,
) (*mcp.ElicitationResult, error) {
	h.calls++
	h.lastRequest = request
	return &mcp.ElicitationResult{
		ElicitationResponse: mcp.ElicitationResponse{Action: mcp.ElicitationResponseActionAccept},
	}, nil
}

type inProcessTestRootsHandler struct {
	calls       int
	lastRequest mcp.ListRootsRequest
}

func (h *inProcessTestRootsHandler) ListRoots(
	_ context.Context,
	request mcp.ListRootsRequest,
) (*mcp.ListRootsResult, error) {
	h.calls++
	h.lastRequest = request
	return &mcp.ListRootsResult{Roots: []mcp.Root{{URI: "file:///workspace"}}}, nil
}
