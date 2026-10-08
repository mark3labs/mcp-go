package client

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The transport path answers a server's request with what the handler
// returns. A handler that returned no result used to be answered with a null
// result, which servers reject; like the multi round-trip path, it is an
// error now.
func TestHandleIncomingRequestRejectsNilHandlerResult(t *testing.T) {
	tests := []struct {
		name    string
		client  *Client
		method  mcp.MCPMethod
		params  any
		wantErr string
	}{
		{
			name:    "sampling",
			client:  &Client{samplingHandler: nilSamplingHandler{}},
			method:  mcp.MethodSamplingCreateMessage,
			params:  mcp.CreateMessageParams{MaxTokens: 1},
			wantErr: "sampling handler returned no result",
		},
		{
			name:   "elicitation",
			client: &Client{elicitationHandler: nilElicitationHandler{}},
			method: mcp.MethodElicitationCreate,
			params: mcp.ElicitationParams{
				Message:         "hi",
				RequestedSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			},
			wantErr: "elicitation handler returned no result",
		},
		{
			name:    "roots",
			client:  &Client{rootsHandler: nilRootsHandler{}},
			method:  mcp.MethodListRoots,
			wantErr: "roots handler returned no result",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := tt.client.handleIncomingRequest(t.Context(), transport.JSONRPCRequest{
				JSONRPC: mcp.JSONRPC_VERSION,
				ID:      mcp.NewRequestId(1),
				Method:  string(tt.method),
				Params:  tt.params,
			})
			assert.Nil(t, response)
			require.EqualError(t, err, tt.wantErr)
		})
	}
}

type noRootsHandler struct{}

func (noRootsHandler) ListRoots(context.Context, mcp.ListRootsRequest) (*mcp.ListRootsResult, error) {
	return &mcp.ListRootsResult{}, nil
}

// A client without roots answers with an empty array, which the schema
// requires, rather than null.
func TestListRootsWithoutRootsSendsEmptyArray(t *testing.T) {
	client := &Client{rootsHandler: noRootsHandler{}}

	response, err := client.handleIncomingRequest(t.Context(), transport.JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(1),
		Method:  string(mcp.MethodListRoots),
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"roots": []}`, string(response.Result))

	// The same goes for the answer to a roots input request.
	inputResponse, err := client.fulfillInputRequest(t.Context(), mcp.NewRootsInputRequest())
	require.NoError(t, err)
	encoded, err := json.Marshal(inputResponse)
	require.NoError(t, err)
	assert.JSONEq(t, `{"roots": []}`, string(encoded))
}

// An in-process client hands the server what its handler returns without
// encoding it, so a missing result reached server code as a nil result
// without an error. It is an error there too.
func TestInProcessClientRejectsNilHandlerResult(t *testing.T) {
	mcpServer := server.NewMCPServer("test-server", "1.0.0", server.WithElicitation(), server.WithRoots())
	mcpServer.EnableSampling()
	mcpServer.AddTool(mcp.NewTool("ask"), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var err error
		switch request.GetString("what", "") {
		case "sampling":
			_, err = mcpServer.RequestSampling(ctx, mcp.CreateMessageRequest{CreateMessageParams: mcp.CreateMessageParams{MaxTokens: 1}})
		case "elicitation":
			_, err = mcpServer.RequestElicitation(ctx, mcp.ElicitationRequest{Params: mcp.ElicitationParams{
				Message:         "hi",
				RequestedSchema: map[string]any{"type": "object"},
			}})
		case "roots":
			_, err = mcpServer.RequestRoots(ctx, mcp.ListRootsRequest{})
		}
		if err == nil {
			return mcp.NewToolResultText("no error"), nil
		}
		return mcp.NewToolResultText(err.Error()), nil
	})

	client, err := NewInProcessClientWithOptions(mcpServer,
		WithSamplingHandler(nilSamplingHandler{}),
		WithElicitationHandler(nilElicitationHandler{}),
		WithRootsHandler(nilRootsHandler{}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	require.NoError(t, client.Start(t.Context()))
	_, err = client.Initialize(t.Context(), mcp.InitializeRequest{Params: mcp.InitializeParams{
		ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
		ClientInfo:      mcp.Implementation{Name: "test-client", Version: "1.0.0"},
	}})
	require.NoError(t, err)

	for _, what := range []string{"sampling", "elicitation", "roots"} {
		t.Run(what, func(t *testing.T) {
			result, err := client.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{
				Name:      "ask",
				Arguments: map[string]any{"what": what},
			}})
			require.NoError(t, err)
			require.Len(t, result.Content, 1)
			assert.Equal(t, what+" handler returned no result", result.Content[0].(mcp.TextContent).Text)
		})
	}
}
