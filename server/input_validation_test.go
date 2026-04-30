package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

// fakeListTool returns a tool that mirrors the kubernetes_list shape used in
// the bug report: a `continue` pagination token (the standard k8s name) plus
// strict additionalProperties: false so unknown args like `cursor` are
// rejected.
func fakeListTool() mcp.Tool {
	return mcp.NewTool("kubernetes_list",
		mcp.WithDescription("List Kubernetes resources"),
		mcp.WithString("resourceType", mcp.Required(),
			mcp.Description("Type of resource to list")),
		mcp.WithString("continue",
			mcp.Description("Continue token from previous paginated request")),
		mcp.WithSchemaAdditionalProperties(false),
	)
}

func okHandler(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText("ok"), nil
}

func callTool(t *testing.T, srv *MCPServer, toolName string, args any) mcp.JSONRPCMessage {
	t.Helper()
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return srv.HandleMessage(t.Context(), raw)
}

// requireToolErrorContaining asserts that the response is a successful
// JSON-RPC response carrying a tool execution error (CallToolResult.IsError =
// true) whose text contains the supplied substring. This is the SEP-1303
// shape that lets the model receive the validation message in its context.
func requireToolErrorContaining(t *testing.T, resp mcp.JSONRPCMessage, want string) {
	t.Helper()
	jr, ok := resp.(mcp.JSONRPCResponse)
	require.True(t, ok, "expected JSON-RPC response, got %T", resp)
	result, ok := jr.Result.(*mcp.CallToolResult)
	require.True(t, ok, "expected *mcp.CallToolResult, got %T", jr.Result)
	require.True(t, result.IsError, "expected IsError=true, got %#v", result)
	require.NotEmpty(t, result.Content, "expected at least one content entry")
	tc, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok, "expected TextContent, got %T", result.Content[0])
	require.Contains(t, tc.Text, want)
}

func requireToolSuccess(t *testing.T, resp mcp.JSONRPCMessage) *mcp.CallToolResult {
	t.Helper()
	jr, ok := resp.(mcp.JSONRPCResponse)
	require.True(t, ok, "expected JSON-RPC response, got %T", resp)
	result, ok := jr.Result.(*mcp.CallToolResult)
	require.True(t, ok, "expected *mcp.CallToolResult, got %T", jr.Result)
	require.False(t, result.IsError, "expected IsError=false, got %#v", result)
	return result
}

// TestInputSchemaValidation covers the matrix of validation outcomes for a
// range of tools: strict, permissive, no schema, raw schema, broken schema.
// Each row exercises exactly one server lifecycle, registers the listed
// tools, makes a single tool call, and asserts on either success or an error
// containing a specific substring.
//
// Scenarios that need multi-step setup (cache invalidation,
// recompilation-on-schema-change) live in their own dedicated tests.
func TestInputSchemaValidation(t *testing.T) {
	rawStrictTool := mcp.Tool{
		Name:        "raw",
		Description: "Tool with raw JSON Schema",
		RawInputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {"name": {"type": "string"}},
			"required": ["name"],
			"additionalProperties": false
		}`),
	}
	brokenSchemaTool := mcp.Tool{
		Name:           "broken",
		Description:    "Tool with malformed schema",
		RawInputSchema: json.RawMessage(`{"type": "object", "properties": {`),
	}
	noSchemaTool := mcp.Tool{Name: "noschema", Description: "no schema"}
	permissiveTool := mcp.NewTool("permissive",
		mcp.WithString("name", mcp.Required()),
	)
	nestedStrictTool := mcp.Tool{
		Name: "nested",
		RawInputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"filter": {
					"type": "object",
					"properties": {"label": {"type": "string"}},
					"required": ["label"],
					"additionalProperties": false
				}
			},
			"required": ["filter"]
		}`),
	}

	tests := []struct {
		name              string
		options           []ServerOption
		tool              mcp.Tool
		args              map[string]any
		wantSuccess       bool
		wantErrContains   string
		wantHandlerCalled bool
	}{
		{
			// Regression guard: with validation off, the kubernetes_list
			// scenario from the bug report (sending `cursor` instead of
			// `continue`) silently succeeds. Pinned so we never flip the
			// default to strict and break existing servers.
			name:              "disabled by default accepts unknown property",
			options:           nil,
			tool:              fakeListTool(),
			args:              map[string]any{"resourceType": "pods", "cursor": "abc"},
			wantSuccess:       true,
			wantHandlerCalled: true,
		},
		{
			// The motivating fix: with validation on and additionalProperties:
			// false, an unknown `cursor` parameter must surface as a tool
			// execution error so the model can retry with `continue`.
			name:            "strict schema rejects unknown property",
			options:         []ServerOption{WithInputSchemaValidation()},
			tool:            fakeListTool(),
			args:            map[string]any{"resourceType": "pods", "cursor": "abc"},
			wantErrContains: "cursor",
		},
		{
			name:            "strict schema rejects missing required",
			options:         []ServerOption{WithInputSchemaValidation()},
			tool:            fakeListTool(),
			args:            map[string]any{"continue": "abc"},
			wantErrContains: "resourceType",
		},
		{
			name:            "strict schema rejects wrong type",
			options:         []ServerOption{WithInputSchemaValidation()},
			tool:            fakeListTool(),
			args:            map[string]any{"resourceType": 123},
			wantErrContains: "resourceType",
		},
		{
			name:              "valid call passes through to handler",
			options:           []ServerOption{WithInputSchemaValidation()},
			tool:              fakeListTool(),
			args:              map[string]any{"resourceType": "pods", "continue": "abc"},
			wantSuccess:       true,
			wantHandlerCalled: true,
		},
		{
			// Tools that omit additionalProperties: false continue to accept
			// extras even with validation enabled. This preserves back-compat
			// with permissive schemas the author chose deliberately.
			name:              "permissive schema accepts extras",
			options:           []ServerOption{WithInputSchemaValidation()},
			tool:              permissiveTool,
			args:              map[string]any{"name": "alice", "extra": "field"},
			wantSuccess:       true,
			wantHandlerCalled: true,
		},
		{
			// A schema that fails to compile must not block calls to the
			// tool. The validator silently degrades and the handler runs.
			name:              "malformed schema is silently skipped",
			options:           []ServerOption{WithInputSchemaValidation()},
			tool:              brokenSchemaTool,
			args:              map[string]any{"anything": true},
			wantSuccess:       true,
			wantHandlerCalled: true,
		},
		{
			name:            "raw input schema participates in validation",
			options:         []ServerOption{WithInputSchemaValidation()},
			tool:            rawStrictTool,
			args:            map[string]any{"name": "alice", "typo": 1},
			wantErrContains: "typo",
		},
		{
			// Tools with no input schema at all (effectively zero-arg tools)
			// are unaffected: there is nothing to validate against.
			name:              "tool without schema is unaffected",
			options:           []ServerOption{WithInputSchemaValidation()},
			tool:              noSchemaTool,
			args:              map[string]any{"anything": "goes"},
			wantSuccess:       true,
			wantHandlerCalled: true,
		},
		{
			name:            "nested error message mentions field path",
			options:         []ServerOption{WithInputSchemaValidation()},
			tool:            nestedStrictTool,
			args:            map[string]any{"filter": map[string]any{"label": "x", "extra": true}},
			wantErrContains: "filter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewMCPServer("test", "1.0.0", tt.options...)
			handlerCalled := false
			srv.AddTool(tt.tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				handlerCalled = true
				return mcp.NewToolResultText("ok"), nil
			})

			resp := callTool(t, srv, tt.tool.Name, tt.args)

			if tt.wantSuccess {
				requireToolSuccess(t, resp)
			} else {
				requireToolErrorContaining(t, resp, tt.wantErrContains)
			}
			assert.Equal(t, tt.wantHandlerCalled, handlerCalled, "handler invocation expectation")
		})
	}
}

// TestInputSchemaValidation_RecompilesOnSchemaChange documents that
// re-registering a tool with a different schema honours the new schema on
// the next call. The cache keys by name+digest, so a fresh schema produces a
// fresh entry rather than colliding with the previous one.
func TestInputSchemaValidation_RecompilesOnSchemaChange(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithInputSchemaValidation())
	srv.AddTool(fakeListTool(), okHandler)

	resp := callTool(t, srv, "kubernetes_list", map[string]any{
		"resourceType": "pods",
		"cursor":       "abc",
	})
	requireToolErrorContaining(t, resp, "cursor")

	relaxed := mcp.NewTool("kubernetes_list",
		mcp.WithDescription("List Kubernetes resources"),
		mcp.WithString("resourceType", mcp.Required()),
		mcp.WithString("continue"),
	)
	srv.AddTool(relaxed, okHandler)

	resp = callTool(t, srv, "kubernetes_list", map[string]any{
		"resourceType": "pods",
		"cursor":       "abc",
	})
	requireToolSuccess(t, resp)
}

// TestInputSchemaValidation_DeleteToolInvalidatesCache makes a validated
// call first to populate the cache, then deletes the tool and asserts the
// cache entry is gone. Without the warmup, the cache would be empty
// regardless of whether DeleteTools invalidated it (compilation is lazy).
func TestInputSchemaValidation_DeleteToolInvalidatesCache(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithInputSchemaValidation())
	srv.AddTool(fakeListTool(), okHandler)

	resp := callTool(t, srv, "kubernetes_list", map[string]any{
		"resourceType": "pods",
		"continue":     "abc",
	})
	requireToolSuccess(t, resp)
	require.Len(t, srv.inputValidator.cached, 1, "cache should be populated after a validated call")

	srv.DeleteTools("kubernetes_list")
	require.Len(t, srv.inputValidator.cached, 0, "cache should be empty after DeleteTools")
}

// TestInputSchemaValidation_CachesPerSchemaDigest is the regression test for
// the same-named-tool-with-different-schema scenario. Two compilations of the
// same name with different schema bytes must coexist (one per digest), so
// session-scoped tools that shadow global tools don't thrash the cache on
// alternating calls.
func TestInputSchemaValidation_CachesPerSchemaDigest(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithInputSchemaValidation())
	srv.AddTool(fakeListTool(), okHandler)
	_, err := srv.inputValidator.lookupOrCompile("kubernetes_list", []byte(`{"type":"object","additionalProperties":false}`))
	require.NoError(t, err)
	_, err = srv.inputValidator.lookupOrCompile("kubernetes_list", []byte(`{"type":"object","additionalProperties":true}`))
	require.NoError(t, err)

	require.Len(t, srv.inputValidator.cached, 1, "expected exactly one tool entry in the cache")
	require.Len(t, srv.inputValidator.cached["kubernetes_list"], 2,
		"expected two compiled schema entries for the same tool name")
}

// TestInputSchemaValidation_NestedPathInError keeps a focused assertion that
// the validator's error message reaches into nested objects. It deliberately
// duplicates one of the table cases above with extra path-substring checks
// that would clutter the table assertions.
func TestInputSchemaValidation_NestedPathInError(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithInputSchemaValidation())
	tool := mcp.Tool{
		Name: "nested",
		RawInputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"filter": {
					"type": "object",
					"properties": {"label": {"type": "string"}},
					"required": ["label"],
					"additionalProperties": false
				}
			},
			"required": ["filter"]
		}`),
	}
	srv.AddTool(tool, okHandler)

	resp := callTool(t, srv, "nested", map[string]any{
		"filter": map[string]any{"label": "x", "extra": true},
	})
	jr, ok := resp.(mcp.JSONRPCResponse)
	require.True(t, ok)
	result, ok := jr.Result.(*mcp.CallToolResult)
	require.True(t, ok)
	require.True(t, result.IsError)
	tc := result.Content[0].(mcp.TextContent)
	require.True(t, strings.Contains(tc.Text, "/filter") || strings.Contains(tc.Text, "filter"),
		"error message should reference the nested path: %s", tc.Text)
}
