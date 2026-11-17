package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestResourceTemplateMatching(t *testing.T) {
	tests := []struct {
		name      string
		template  string
		uri       string
		arguments map[string]any
	}{
		{"wrong query variable", "test://items{?id}", "test://items?other=1", nil},
		{"wrong continuation variable", "test://items?fixed=1{&id}", "test://items?fixed=1&other=1", nil},
		{"wrong path parameter", "test://items{;id}", "test://items;other=1", nil},
		{"query variable", "test://items{?id}", "test://items?id=1", map[string]any{"id": []string{"1"}}},
		{"continuation variable", "test://items?fixed=1{&id}", "test://items?fixed=1&id=1", map[string]any{"id": []string{"1"}}},
		{"path parameter", "test://items{;id}", "test://items;id=1", map[string]any{"id": []string{"1"}}},
		{"escaped value", "test://items{?id}", "test://items?id=a%2Fb", map[string]any{"id": []string{"a/b"}}},
		{"omitted variable", "test://items{?id}", "test://items", map[string]any{}},
		{"literal template", "test://items", "test://items", map[string]any{}},
		{"path variable", "test://items/{id}", "test://items/1", map[string]any{"id": []string{"1"}}},
		{"reserved expansion", "test://items/{+path}", "test://items/a/b", map[string]any{"path": []string{"a/b"}}},
	}
	for _, scope := range []string{"global", "session"} {
		for _, tt := range tests {
			t.Run(scope+"/"+tt.name, func(t *testing.T) {
				s := NewMCPServer("test", "1.0", WithResourceCapabilities(false, false))
				called := false
				template := ServerResourceTemplate{
					Template: mcp.NewResourceTemplate(tt.template, "items"),
					Handler: func(_ context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
						called = true
						assert.Equal(t, tt.arguments, request.Params.Arguments)
						return []mcp.ResourceContents{mcp.TextResourceContents{URI: request.Params.URI, Text: "matched"}}, nil
					},
				}
				ctx := t.Context()
				if scope == "global" {
					s.AddResourceTemplates(template)
				} else {
					session := &sessionTestClientWithResourceTemplates{
						sessionID: "template-session",
						sessionResourceTemplates: map[string]ServerResourceTemplate{
							template.Template.URITemplate.Raw(): template,
						},
					}
					session.Initialize()
					ctx = s.WithContext(ctx, session)
				}

				request, err := json.Marshal(map[string]any{
					"jsonrpc": "2.0", "id": 1, "method": "resources/read",
					"params": map[string]string{"uri": tt.uri},
				})
				require.NoError(t, err)
				response := s.HandleMessage(ctx, request)
				if tt.arguments != nil {
					require.True(t, called, "a matching template must invoke its handler")
					result, ok := response.(mcp.JSONRPCResponse)
					require.True(t, ok, "expected a resource result, got %T", response)
					assert.Equal(t, mcp.ReadResourceResult{Contents: []mcp.ResourceContents{
						mcp.TextResourceContents{URI: tt.uri, Text: "matched"},
					}}, result.Result)
					return
				}
				assert.False(t, called, "a nonmatching template must not invoke its handler")
				errorResponse, ok := response.(mcp.JSONRPCError)
				require.True(t, ok, "expected resource-not-found error, got %T", response)
				assert.Equal(t, mcp.RESOURCE_NOT_FOUND, errorResponse.Error.Code)
			})
		}
	}
}

func TestResourceTemplateDispatchPrecedence(t *testing.T) {
	s := NewMCPServer("test", "1.0", WithResourceCapabilities(false, false))
	handler := func(name string) ResourceTemplateHandlerFunc {
		return func(_ context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{mcp.TextResourceContents{URI: request.Params.URI, Text: name}}, nil
		}
	}
	s.AddResourceTemplate(mcp.NewResourceTemplate("test://items{?global}", "global"), handler("global"))
	s.AddResource(mcp.NewResource("test://items?exact=1", "exact"), ResourceHandlerFunc(handler("exact")))
	session := &sessionTestClientWithResourceTemplates{
		sessionID: "template-session",
		sessionResourceTemplates: map[string]ServerResourceTemplate{
			"test://items{?session}": {
				Template: mcp.NewResourceTemplate("test://items{?session}", "session"),
				Handler:  handler("session"),
			},
		},
	}
	session.Initialize()
	ctx := s.WithContext(t.Context(), session)

	for _, name := range []string{"session", "global", "exact"} {
		t.Run(name, func(t *testing.T) {
			uri := "test://items?" + name + "=1"
			request, err := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": 1, "method": "resources/read",
				"params": map[string]string{"uri": uri},
			})
			require.NoError(t, err)
			response := s.HandleMessage(ctx, request)
			result, ok := response.(mcp.JSONRPCResponse)
			require.True(t, ok, "expected a resource result, got %T", response)
			assert.Equal(t, mcp.ReadResourceResult{Contents: []mcp.ResourceContents{
				mcp.TextResourceContents{URI: uri, Text: name},
			}}, result.Result)
		})
	}
}
