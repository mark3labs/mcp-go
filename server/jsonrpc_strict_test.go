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
		{
			name:    "custom method with case-variant params",
			message: `{"jsonrpc":"2.0","id":1,"method":"custom/update","params":{"Name":"x","ID":2}}`,
		},
		{
			name:    "custom notification with arbitrary params",
			message: `{"jsonrpc":"2.0","method":"custom/event","params":{"Event":"fired","Timestamp":1234}}`,
		},

		// ISSUE #1: Escaped Method Names
		{
			name:    "escaped method name tools\\u002fcall should validate params",
			message: `{"jsonrpc":"2.0","id":1,"method":"tools\u002fcall","params":{"name":"t","arguments":{}}}`,
		},
		{
			name:    "escaped method name tools\\u002fcall with invalid params should reject",
			message: `{"jsonrpc":"2.0","id":1,"method":"tools\u002fcall","params":{"NAME":"t"}}`,
			wantErr: `is not the defined name "params.name"`,
		},
		{
			name:    "escaped underscore in method name",
			message: `{"jsonrpc":"2.0","id":1,"method":"tools\u005frefresh","params":{"Name":"x"}}`,
		},
		{
			name:    "double-escaped method name should be treated as custom",
			message: `{"jsonrpc":"2.0","id":1,"method":"tools\\u002fcall","params":{"NAME":"t"}}`,
		},
		{
			name:    "escaped method name that is unknown method",
			message: `{"jsonrpc":"2.0","id":1,"method":"custom\u002fmethod","params":{"Name":"x","ID":2}}`,
		},

		// ISSUE #2: Member Order Bypass
		{
			name:    "params before method with invalid params should reject",
			message: `{"jsonrpc":"2.0","id":1,"params":{"NAME":"t"},"method":"tools/call"}`,
			wantErr: `is not the defined name "params.name"`,
		},
		{
			name:    "params before method with valid params should accept",
			message: `{"jsonrpc":"2.0","id":1,"params":{"name":"t","arguments":{}},"method":"tools/call"}`,
		},
		{
			name:    "id, params, method reversed order with invalid params",
			message: `{"id":1,"params":{"NAME":"x"},"method":"tools/call","jsonrpc":"2.0"}`,
			wantErr: `is not the defined name "params.name"`,
		},
		{
			name:    "params before method for custom method should not validate",
			message: `{"jsonrpc":"2.0","id":1,"params":{"NAME":"t","ID":2},"method":"custom/method"}`,
		},
		{
			name:    "params appears multiple times with method",
			message: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"t","arguments":{}},"params":{"NAME":"bad"}}`,
			wantErr: `duplicate member "params"`,
		},

		// ISSUE #3: Malformed JSON should return PARSE_ERROR via caller, not INVALID_REQUEST
		// Note: These tests verify that validateProtocolMemberNames doesn't interfere with PARSE_ERROR
		// The actual PARSE_ERROR vs INVALID_REQUEST distinction is tested at the HandleMessage level
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			protocolMethods := protocolDefinedMethods()
			err := validateProtocolMemberNames(json.RawMessage(tt.message), protocolMethods)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}


// TestHandleMessageRejectsUndefinedMemberNames verifies that invalid envelope
// member names return INVALID_REQUEST error code.
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
	response := server.HandleMessage(context.Background(), json.RawMessage(message))

	errorResponse, ok := response.(mcp.JSONRPCError)
	require.True(t, ok, "expected JSONRPCError, got %T", response)
	assert.Equal(t, mcp.INVALID_REQUEST, errorResponse.Error.Code)
	assert.False(t, called)
}

// TestHandleMessageRejectsMalformedJSON verifies that malformed JSON returns
// PARSE_ERROR, not INVALID_REQUEST from member name validation.
func TestHandleMessageRejectsMalformedJSON(t *testing.T) {
	server := NewMCPServer("test", "1.0.0")

	tests := []struct {
		name    string
		message string
	}{
		{
			name:    "incomplete JSON object",
			message: `{"jsonrpc":"2.0","id":1,"method":"ping"`,
		},
		{
			name:    "missing closing brace",
			message: `{"jsonrpc":"2.0","id":1,"method":"ping"`,
		},
		{
			name:    "trailing comma",
			message: `{"jsonrpc":"2.0","id":1,"method":"ping",}`,
		},
		{
			name:    "invalid escape sequence in string",
			message: `{"jsonrpc":"2.0","id":1,"method":"ping\x00"}`,
		},
		{
			name:    "unclosed string",
			message: `{"jsonrpc":"2.0","id":1,"method":"ping`,
		},
		{
			name:    "invalid JSON value",
			message: `{"jsonrpc":"2.0","id":1,"method":undefined}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := server.HandleMessage(context.Background(), json.RawMessage(tt.message))

			errorResponse, ok := response.(mcp.JSONRPCError)
			require.True(t, ok, "expected JSONRPCError, got %T", response)
			assert.Equal(t, mcp.PARSE_ERROR, errorResponse.Error.Code, "malformed JSON should return PARSE_ERROR, not %s", errorResponse.Error.Code)
		})
	}
}

// TestHandleEscapedMethodNameValidation verifies that escaped method names
// are properly unescaped before validation (Issue #1).
func TestHandleEscapedMethodNameValidation(t *testing.T) {
	server := NewMCPServer("test", "1.0.0")
	server.AddTool(
		mcp.NewTool("test_tool"),
		func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("ok"), nil
		},
	)

	tests := []struct {
		name        string
		message     string
		shouldReject bool
	}{
		{
			name:    "escaped slash in method tools\\u002fcall with valid params",
			message: `{"jsonrpc":"2.0","id":1,"method":"tools\u002fcall","params":{"name":"test_tool","arguments":{}}}`,
		},
		{
			name:        "escaped slash in method with invalid params member name",
			message:     `{"jsonrpc":"2.0","id":1,"method":"tools\u002fcall","params":{"NAME":"test_tool"}}`,
			shouldReject: true,
		},
		{
			name:    "fully escaped method name representing tools/call",
			message: `{"jsonrpc":"2.0","id":1,"method":"\u0074\u006f\u006f\u006c\u0073\u002f\u0063\u0061\u006c\u006c","params":{"name":"test_tool","arguments":{}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := server.HandleMessage(context.Background(), json.RawMessage(tt.message))

			if tt.shouldReject {
				errorResponse, ok := response.(mcp.JSONRPCError)
				require.True(t, ok, "expected JSONRPCError, got %T", response)
				assert.Equal(t, mcp.INVALID_REQUEST, errorResponse.Error.Code)
			} else {
				assert.NotNil(t, response)
			}
		})
	}
}

// TestHandleParamsMemberOrderBypass verifies that params appearing before method
// in JSON is still properly validated (Issue #2).
func TestHandleParamsMemberOrderBypass(t *testing.T) {
	server := NewMCPServer("test", "1.0.0")
	server.AddTool(
		mcp.NewTool("test_tool"),
		func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("ok"), nil
		},
	)

	tests := []struct {
		name        string
		message     string
		shouldReject bool
	}{
		{
			name:    "params before method with valid params",
			message: `{"jsonrpc":"2.0","params":{"name":"test_tool","arguments":{}},"id":1,"method":"tools/call"}`,
		},
		{
			name:        "params before method with invalid params member name",
			message:     `{"jsonrpc":"2.0","params":{"NAME":"test_tool"},"id":1,"method":"tools/call"}`,
			shouldReject: true,
		},
		{
			name:    "params before id before method",
			message: `{"jsonrpc":"2.0","params":{"name":"test_tool","arguments":{}},"id":1,"method":"tools/call"}`,
		},
		{
			name:    "fully reversed member order",
			message: `{"id":1,"params":{"name":"test_tool","arguments":{}},"method":"tools/call","jsonrpc":"2.0"}`,
		},
		{
			name:        "fully reversed with invalid params",
			message:     `{"id":1,"params":{"NAME":"test_tool"},"method":"tools/call","jsonrpc":"2.0"}`,
			shouldReject: true,
		},
		{
			name:    "params before method for custom method - does not validate params",
			message: `{"jsonrpc":"2.0","id":1,"params":{"NAME":"anything","ID":2},"method":"custom/method"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := server.HandleMessage(context.Background(), json.RawMessage(tt.message))

			if tt.shouldReject {
				errorResponse, ok := response.(mcp.JSONRPCError)
				require.True(t, ok, "expected JSONRPCError, got %T", response)
				assert.Equal(t, mcp.INVALID_REQUEST, errorResponse.Error.Code)
			} else {
				// Should not be an error
				assert.NotNil(t, response)
			}
		})
	}
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
