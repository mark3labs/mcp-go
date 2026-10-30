package client

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestClientSamplingPreservesEmptyToolResultContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"single", `{"type":"tool_result","toolUseId":"call_1","content":[],"structuredContent":{"count":0}}`},
		{"array", `[{"type":"tool_result","toolUseId":"call_1","content":[],"structuredContent":{"count":0}}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &mockSamplingHandler{result: &mcp.CreateMessageResult{
				SamplingMessage: mcp.SamplingMessage{Role: mcp.RoleAssistant, Content: mcp.NewTextContent("done")},
				Model:           "test-model",
			}}
			client := &Client{samplingHandler: handler}
			params := json.RawMessage(`{"messages":[{"role":"user","content":` + tt.content + `}],"maxTokens":100}`)
			_, err := client.handleIncomingRequest(t.Context(), transport.JSONRPCRequest{
				JSONRPC: mcp.JSONRPC_VERSION,
				ID:      mcp.NewRequestId(1),
				Method:  string(mcp.MethodSamplingCreateMessage),
				Params:  params,
			})
			require.NoError(t, err)
			require.Len(t, handler.request.Messages, 1)
			encoded, err := json.Marshal(handler.request.Messages[0].Content)
			require.NoError(t, err)
			assert.JSONEq(t, tt.content, string(encoded))
		})
	}
}
