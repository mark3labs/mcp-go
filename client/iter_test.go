package client

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newIterTestClient creates an in-process client connected to an MCP server
// configured with paginationLimit and N items of each kind.
func newIterTestClient(t *testing.T, paginationLimit, count int) *Client {
	t.Helper()

	mcpServer := server.NewMCPServer(
		"test-server",
		"1.0.0",
		server.WithResourceCapabilities(true, true),
		server.WithPromptCapabilities(true),
		server.WithToolCapabilities(true),
		server.WithPaginationLimit(paginationLimit),
	)

	for i := range count {
		mcpServer.AddTool(
			mcp.NewTool(fmt.Sprintf("tool-%02d", i)),
			func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return mcp.NewToolResultText("ok"), nil
			},
		)
		mcpServer.AddResource(
			mcp.Resource{URI: fmt.Sprintf("test://resource-%02d", i), Name: fmt.Sprintf("Resource %02d", i)},
			func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
				return nil, nil
			},
		)
		mcpServer.AddResourceTemplate(
			mcp.NewResourceTemplate(fmt.Sprintf("test://template-%02d/{id}", i), fmt.Sprintf("template-%02d", i)),
			func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
				return nil, nil
			},
		)
		mcpServer.AddPrompt(
			mcp.Prompt{Name: fmt.Sprintf("prompt-%02d", i)},
			func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
				return &mcp.GetPromptResult{}, nil
			},
		)
	}

	client, err := NewInProcessClient(mcpServer)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	require.NoError(t, client.Start(t.Context()))

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "test-client", Version: "1.0.0"}
	_, err = client.Initialize(t.Context(), initReq)
	require.NoError(t, err)

	return client
}

func TestClient_IterTools(t *testing.T) {
	t.Run("iterates across all pages", func(t *testing.T) {
		client := newIterTestClient(t, 3, 7)

		var names []string
		for tool, err := range client.IterTools(t.Context(), mcp.ListToolsRequest{}) {
			require.NoError(t, err)
			names = append(names, tool.Name)
		}
		assert.Len(t, names, 7)
		// names should be unique
		seen := make(map[string]struct{})
		for _, n := range names {
			seen[n] = struct{}{}
		}
		assert.Len(t, seen, 7)
	})

	t.Run("early break stops iteration", func(t *testing.T) {
		client := newIterTestClient(t, 2, 10)

		count := 0
		for _, err := range client.IterTools(t.Context(), mcp.ListToolsRequest{}) {
			require.NoError(t, err)
			count++
			if count == 1 {
				break
			}
		}
		assert.Equal(t, 1, count)
	})

	t.Run("single page when results fit", func(t *testing.T) {
		client := newIterTestClient(t, 100, 3)
		count := 0
		for _, err := range client.IterTools(t.Context(), mcp.ListToolsRequest{}) {
			require.NoError(t, err)
			count++
		}
		assert.Equal(t, 3, count)
	})

	t.Run("empty list yields no values", func(t *testing.T) {
		client := newIterTestClient(t, 5, 0)
		count := 0
		for _, err := range client.IterTools(t.Context(), mcp.ListToolsRequest{}) {
			require.NoError(t, err)
			count++
		}
		assert.Equal(t, 0, count)
	})

	t.Run("cancelled context yields error", func(t *testing.T) {
		client := newIterTestClient(t, 5, 3)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		var gotErr error
		for _, err := range client.IterTools(ctx, mcp.ListToolsRequest{}) {
			if err != nil {
				gotErr = err
				break
			}
		}
		require.Error(t, gotErr)
		assert.True(t, errors.Is(gotErr, context.Canceled))
	})
}

func TestClient_IterResources(t *testing.T) {
	client := newIterTestClient(t, 2, 5)

	var uris []string
	for res, err := range client.IterResources(t.Context(), mcp.ListResourcesRequest{}) {
		require.NoError(t, err)
		uris = append(uris, res.URI)
	}
	assert.Len(t, uris, 5)
}

func TestClient_IterResourceTemplates(t *testing.T) {
	client := newIterTestClient(t, 2, 5)

	count := 0
	for _, err := range client.IterResourceTemplates(t.Context(), mcp.ListResourceTemplatesRequest{}) {
		require.NoError(t, err)
		count++
	}
	assert.Equal(t, 5, count)
}

func TestClient_IterPrompts(t *testing.T) {
	client := newIterTestClient(t, 2, 5)

	var names []string
	for p, err := range client.IterPrompts(t.Context(), mcp.ListPromptsRequest{}) {
		require.NoError(t, err)
		names = append(names, p.Name)
	}
	assert.Len(t, names, 5)
}
