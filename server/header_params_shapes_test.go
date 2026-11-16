package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestValidateStandardHeaders_ParamsShapes(t *testing.T) {
	echo, _ := mcp.EncodeHeaderValue("echo")
	other, _ := mcp.EncodeHeaderValue("other")
	uri, _ := mcp.EncodeHeaderValue("file:///a")

	tests := []struct {
		name         string
		method       string
		headerMethod string
		headerName   string
		body         string
		wantErr      string
	}{
		{name: "call ok", method: "tools/call", headerName: echo,
			body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"text":"x"}}}`},
		{name: "call mismatch", method: "tools/call", headerName: other,
			body:    `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo"}}`,
			wantErr: `header mismatch: Mcp-Name: header value "other" does not match body value "echo"`},
		{name: "call no params", method: "tools/call", headerName: echo,
			body:    `{"jsonrpc":"2.0","id":1,"method":"tools/call"}`,
			wantErr: `header mismatch: Mcp-Name: failed to extract name from parameters for method "tools/call"`},
		{name: "call null params", method: "tools/call", headerName: echo,
			body:    `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":null}`,
			wantErr: `header mismatch: Mcp-Name: header value "echo" does not match body value ""`},
		{name: "call array params", method: "tools/call", headerName: echo,
			body:    `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":[1]}`,
			wantErr: `header mismatch: Mcp-Name: failed to extract name from parameters for method "tools/call"`},
		{name: "call name number", method: "tools/call", headerName: echo,
			body:    `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":5}}`,
			wantErr: `header mismatch: Mcp-Name: failed to extract name from parameters for method "tools/call"`},
		{name: "call upper key", method: "tools/call", headerName: echo,
			body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"NAME":"echo"}}`},
		{name: "call duplicate key", method: "tools/call", headerName: echo,
			body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"other","name":"echo"}}`},
		{name: "call empty name", method: "tools/call",
			body:    `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":""}}`,
			wantErr: `header mismatch: Mcp-Name: missing required header for method "tools/call"`},
		{name: "call unknown tool", method: "tools/call", headerName: other,
			body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"other"}}`},
		{name: "read ok", method: "resources/read", headerName: uri,
			body: `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"file:///a"}}`},
		{name: "read mismatch", method: "resources/read", headerName: echo,
			body:    `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"file:///a"}}`,
			wantErr: `header mismatch: Mcp-Name: header value "echo" does not match body value "file:///a"`},
		{name: "prompt ok", method: "prompts/get", headerName: echo,
			body: `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"echo"}}`},
		{name: "list", method: "tools/list",
			body: `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`},
		{name: "list with wrong method header", method: "tools/list", headerMethod: "tools/call",
			body:    `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
			wantErr: `header mismatch: Mcp-Method: header value "tools/call" does not match body value "tools/list"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headerMethod := tt.headerMethod
			if headerMethod == "" {
				headerMethod = tt.method
			}
			h := http.Header{}
			h.Set(mcp.HeaderProtocolVersion, mcp.ProtocolVersion20260728)
			h.Set(mcp.HeaderMethod, headerMethod)
			if tt.headerName != "" {
				h.Set(mcp.HeaderName, tt.headerName)
			}

			err := benchServer().validateStandardHeadersForMessage(
				t.Context(), h, mcp.ProtocolVersion20260728, mcp.MCPMethod(tt.method), json.RawMessage(tt.body))

			assert.Equal(t, tt.wantErr, fmt.Sprint(errString(err)))
		})
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
