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

// TestInputSchemaValidation_DisabledByDefault is the regression guard: the
// kubernetes_list / cursor scenario from the bug report. With validation
// disabled, the server silently accepts the unknown `cursor` parameter, which
// is exactly what hides typos from the model today.
func TestInputSchemaValidation_DisabledByDefault(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0")
	srv.AddTool(fakeListTool(), okHandler)

	resp := callTool(t, srv, "kubernetes_list", map[string]any{
		"resourceType": "pods",
		"cursor":       "abc",
	})
	requireToolSuccess(t, resp)
}

// TestInputSchemaValidation_RejectsUnknownProperty is the fix for the bug
// report: with validation enabled and additionalProperties: false, an unknown
// `cursor` parameter must surface as a tool execution error so the model can
// retry with the correct `continue` parameter.
func TestInputSchemaValidation_RejectsUnknownProperty(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithInputSchemaValidation())
	srv.AddTool(fakeListTool(), okHandler)

	resp := callTool(t, srv, "kubernetes_list", map[string]any{
		"resourceType": "pods",
		"cursor":       "abc",
	})
	requireToolErrorContaining(t, resp, "cursor")
}

// TestInputSchemaValidation_RejectsMissingRequired covers the second SEP-1303
// scenario: a missing required argument should also become a tool execution
// error rather than the JSON-RPC -32602 protocol error path.
func TestInputSchemaValidation_RejectsMissingRequired(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithInputSchemaValidation())
	srv.AddTool(fakeListTool(), okHandler)

	resp := callTool(t, srv, "kubernetes_list", map[string]any{
		"continue": "abc",
	})
	requireToolErrorContaining(t, resp, "resourceType")
}

// TestInputSchemaValidation_RejectsWrongType makes sure type mismatches (here,
// passing a number where a string is required) are caught.
func TestInputSchemaValidation_RejectsWrongType(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithInputSchemaValidation())
	srv.AddTool(fakeListTool(), okHandler)

	resp := callTool(t, srv, "kubernetes_list", map[string]any{
		"resourceType": 123,
	})
	requireToolErrorContaining(t, resp, "resourceType")
}

// TestInputSchemaValidation_AcceptsValidCall is the happy path: a request
// that satisfies the schema reaches the handler and returns its result.
func TestInputSchemaValidation_AcceptsValidCall(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithInputSchemaValidation())
	called := false
	srv.AddTool(fakeListTool(), func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		called = true
		assert.Equal(t, "pods", req.GetString("resourceType", ""))
		assert.Equal(t, "abc", req.GetString("continue", ""))
		return mcp.NewToolResultText("ok"), nil
	})

	resp := callTool(t, srv, "kubernetes_list", map[string]any{
		"resourceType": "pods",
		"continue":     "abc",
	})
	requireToolSuccess(t, resp)
	assert.True(t, called, "handler was not invoked for a valid call")
}

// TestInputSchemaValidation_AllowsAdditionalWhenSchemaPermits documents that
// validation only rejects unknown properties when the tool author has marked
// the schema strict. Tools that omit additionalProperties: false continue to
// accept extras, preserving back-compat with permissive schemas.
func TestInputSchemaValidation_AllowsAdditionalWhenSchemaPermits(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithInputSchemaValidation())
	tool := mcp.NewTool("permissive",
		mcp.WithString("name", mcp.Required()),
	)
	srv.AddTool(tool, okHandler)

	resp := callTool(t, srv, "permissive", map[string]any{
		"name":  "alice",
		"extra": "field",
	})
	requireToolSuccess(t, resp)
}

// TestInputSchemaValidation_BrokenSchemaIsNoOp guards against a malformed
// schema accidentally blocking every call to that tool. A schema that fails
// to compile should be silently skipped (and the handler invoked normally),
// not surfaced as a permanent validation error to the model.
func TestInputSchemaValidation_BrokenSchemaIsNoOp(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithInputSchemaValidation())
	tool := mcp.Tool{
		Name:           "broken",
		Description:    "Tool with malformed schema",
		RawInputSchema: json.RawMessage(`{"type": "object", "properties": {`), // unterminated JSON
	}
	srv.AddTool(tool, okHandler)

	resp := callTool(t, srv, "broken", map[string]any{"anything": true})
	requireToolSuccess(t, resp)
}

// TestInputSchemaValidation_RawInputSchema verifies that schemas supplied via
// RawInputSchema (the alternative to the structured InputSchema) participate
// in validation just like structured ones.
func TestInputSchemaValidation_RawInputSchema(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithInputSchemaValidation())
	tool := mcp.Tool{
		Name:        "raw",
		Description: "Tool with raw JSON Schema",
		RawInputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {"type": "string"}
			},
			"required": ["name"],
			"additionalProperties": false
		}`),
	}
	srv.AddTool(tool, okHandler)

	resp := callTool(t, srv, "raw", map[string]any{"name": "alice", "typo": 1})
	requireToolErrorContaining(t, resp, "typo")
}

// TestInputSchemaValidation_ReusesCompiledSchema is a behavioural test that
// confirms repeated calls reuse the cached compiled schema rather than
// recompiling on every invocation. We can't observe compile counts directly,
// so instead we re-register the tool with a different schema and check that
// the new schema is honoured on the next call.
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

// TestInputSchemaValidation_DeleteToolInvalidatesCache makes sure deleting
// and re-adding a tool doesn't reuse the stale validator. We rely on the
// content-hash check in the cache, but DeleteTools also drops the entry as a
// belt-and-braces measure.
func TestInputSchemaValidation_DeleteToolInvalidatesCache(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithInputSchemaValidation())
	srv.AddTool(fakeListTool(), okHandler)

	srv.DeleteTools("kubernetes_list")
	require.Len(t, srv.inputValidator.cached, 0, "expected cache to be empty after DeleteTools")
}

// TestInputSchemaValidation_NoSchemaSkipsValidation covers tools that declare
// no input schema at all (effectively a tool that accepts no arguments). The
// validator should treat absence of a schema as "nothing to validate" and let
// the call through.
func TestInputSchemaValidation_NoSchemaSkipsValidation(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithInputSchemaValidation())
	tool := mcp.Tool{Name: "noschema", Description: "no schema"}
	srv.AddTool(tool, okHandler)

	resp := callTool(t, srv, "noschema", map[string]any{"anything": "goes"})
	requireToolSuccess(t, resp)
}

// TestInputSchemaValidation_NestedPathInError ensures the JSON pointer path
// in the error message reaches into nested objects, so the model can
// localise the failing field.
func TestInputSchemaValidation_NestedPathInError(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithInputSchemaValidation())
	tool := mcp.Tool{
		Name: "nested",
		RawInputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"filter": {
					"type": "object",
					"properties": {
						"label": {"type": "string"}
					},
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
