package server

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaginationLegacyNameOnlyCursor(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		configure func(*MCPServer)
		collect   func(any) ([]string, mcp.Cursor)
	}{
		{
			name:   "resources",
			method: "resources/list",
			configure: func(s *MCPServer) {
				for _, uri := range []string{"file:///a", "file:///b", "file:///c"} {
					s.AddResource(mcp.NewResource(uri, "shared"), nil)
				}
				s.AddResource(mcp.NewResource("file:///next", "z-next"), nil)
			},
			collect: func(result any) ([]string, mcp.Cursor) {
				page := result.(mcp.ListResourcesResult)
				var names []string
				for _, item := range page.Resources {
					names = append(names, item.Name)
				}
				return names, page.NextCursor
			},
		},
		{
			name:   "resource templates",
			method: "resources/templates/list",
			configure: func(s *MCPServer) {
				for _, uri := range []string{"repo://a/{path}", "repo://b/{path}"} {
					s.AddResourceTemplate(mcp.NewResourceTemplate(uri, "shared"), nil)
				}
				s.AddResourceTemplate(mcp.NewResourceTemplate("repo://next/{path}", "z-next"), nil)
			},
			collect: func(result any) ([]string, mcp.Cursor) {
				page := result.(mcp.ListResourceTemplatesResult)
				var names []string
				for _, item := range page.ResourceTemplates {
					names = append(names, item.Name)
				}
				return names, page.NextCursor
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewMCPServer("test-server", "1.0.0", WithResourceCapabilities(false, false), WithPaginationLimit(1))
			tt.configure(s)
			// Old servers emitted only the last name, so continuation skipped
			// the entire name group; it cannot recover a specific URI within it.
			cursor := mcp.Cursor(base64.StdEncoding.EncodeToString([]byte("shared")))
			response := s.HandleMessage(t.Context(), fmt.Appendf(nil,
				`{"jsonrpc":"2.0","id":1,"method":%q,"params":{"cursor":%q}}`, tt.method, cursor))
			rpc, ok := response.(mcp.JSONRPCResponse)
			require.True(t, ok, "unexpected response: %+v", response)
			names, next := tt.collect(rpc.Result)
			assert.Equal(t, []string{"z-next"}, names)
			require.NotEmpty(t, next)
			response = s.HandleMessage(t.Context(), fmt.Appendf(nil,
				`{"jsonrpc":"2.0","id":2,"method":%q,"params":{"cursor":%q}}`, tt.method, next))
			rpc, ok = response.(mcp.JSONRPCResponse)
			require.True(t, ok, "unexpected response: %+v", response)
			names, next = tt.collect(rpc.Result)
			assert.Empty(t, names)
			assert.Empty(t, next)
		})
	}
}
