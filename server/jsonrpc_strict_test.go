package server

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestValidateProtocolMemberNames(t *testing.T) {
	tests := []struct {
		name    string
		message string
		wantErr string
	}{
		{
			name:    "defined member names",
			message: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"t","arguments":{}}}`,
		},
		{
			name:    "tool arguments keep their own casing",
			message: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"t","arguments":{"Name":"x","ID":2}}}`,
		},
		{
			name:    "unrelated member is not a protocol member",
			message: `{"jsonrpc":"2.0","id":1,"method":"ping","extra":true}`,
		},
		{
			name:    "params is not an object",
			message: `{"jsonrpc":"2.0","id":1,"method":"ping","params":null}`,
		},
		{
			name:    "capitalized method",
			message: `{"jsonrpc":"2.0","id":1,"Method":"tools/call"}`,
			wantErr: `member "Method" is not the defined name "method"`,
		},
		{
			name:    "upper-case method",
			message: `{"jsonrpc":"2.0","id":1,"METHOD":"tools/call"}`,
			wantErr: `is not the defined name "method"`,
		},
		{
			name:    "U+017F folds onto s",
			message: "{\"jſonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}",
			wantErr: `is not the defined name "jsonrpc"`,
		},
		{
			name:    "U+212A folds onto k",
			message: "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"tas\u212aId\":\"x\"}}",
			wantErr: `is not the defined name "params.taskId"`,
		},
		{
			name:    "folded params member",
			message: `{"jsonrpc":"2.0","id":1,"method":"tools/call","paramſ":{"name":"t"}}`,
			wantErr: `is not the defined name "params"`,
		},
		{
			name:    "folded params member name",
			message: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"NAME":"t"}}`,
			wantErr: `member "params.NAME" is not the defined name "params.name"`,
		},
		{
			name:    "escaped member name",
			message: `{"jsonrpc":"2.0","id":1,"\u004dethod":"tools/call"}`,
			wantErr: `member "Method" is not the defined name "method"`,
		},
		{
			name:    "nested values are skipped",
			message: `{"jsonrpc":"2.0","id":1,"result":{"Method":[1,{"x":"}"}]},"method":"x"}`,
		},
		{
			name:    "repeated method",
			message: `{"jsonrpc":"2.0","id":1,"method":"tools/list","method":"tools/call"}`,
			wantErr: `duplicate member "method"`,
		},
		{
			name:    "repeated unknown member",
			message: `{"jsonrpc":"2.0","id":1,"method":"ping","x":1,"x":2}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateProtocolMemberNames(json.RawMessage(tt.message))
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestHandleMessageRejectsUndefinedMemberNames(t *testing.T) {
	var called bool
	server := NewMCPServer("test", "1.0.0")
	server.AddTool(
		mcp.NewTool("test_tool"),
		func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			called = true
			return mcp.NewToolResultText("ok"), nil
		},
	)

	message := `{"jsonrpc":"2.0","id":1,"Method":"tools/call","params":{"name":"test_tool","arguments":{}}}`
	response := server.HandleMessage(t.Context(), json.RawMessage(message))

	errorResponse, ok := response.(mcp.JSONRPCError)
	require.True(t, ok, "expected JSONRPCError, got %T", response)
	assert.Equal(t, mcp.INVALID_REQUEST, errorResponse.Error.Code)
	assert.False(t, called)
}

// TestParamsMembersCoverDecodedRequests checks that jsonrpcParamsMembers lists
// every params member name of the requests HandleMessage decodes.
func TestParamsMembersCoverDecodedRequests(t *testing.T) {
	requests := []any{
		mcp.InitializeRequest{}, mcp.PingRequest{}, mcp.DiscoverRequest{},
		mcp.SubscriptionsListenRequest{}, mcp.SetLevelRequest{},
		mcp.ListResourcesRequest{}, mcp.ListResourceTemplatesRequest{},
		mcp.ReadResourceRequest{}, mcp.SubscribeRequest{}, mcp.UnsubscribeRequest{},
		mcp.ListPromptsRequest{}, mcp.GetPromptRequest{}, mcp.ListToolsRequest{},
		mcp.CallToolRequest{}, mcp.GetTaskRequest{}, mcp.ListTasksRequest{},
		mcp.TaskResultRequest{}, mcp.CancelTaskRequest{}, mcp.CompleteRequest{},
	}
	for _, req := range requests {
		params, ok := reflect.TypeOf(req).FieldByName("Params")
		require.True(t, ok, "%T has no Params", req)
		for _, name := range jsonNames(params.Type) {
			assert.True(t, slices.Contains(jsonrpcParamsMembers, name), "%T: params.%s is not in jsonrpcParamsMembers", req, name)
		}
	}
}

func jsonNames(typ reflect.Type) []string {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil
	}
	var names []string
	for i := range typ.NumField() {
		f := typ.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		switch {
		case name == "-":
		case f.Anonymous && name == "":
			names = append(names, jsonNames(f.Type)...)
		case f.IsExported():
			if name == "" {
				name = f.Name
			}
			names = append(names, name)
		}
	}
	return names
}
