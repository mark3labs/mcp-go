package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// encoding/json rejects NaN, so this tool's result cannot be written.
func newUnencodableResultServer() *MCPServer {
	server := NewMCPServer("test", "1.0.0", WithToolCapabilities(true))
	server.AddTool(mcp.NewTool("nan"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultStructured(map[string]any{"ratio": math.NaN()}, "ratio"), nil
	})
	return server
}

const unencodableResultCall = `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"nan","arguments":{}}}`

func TestUnencodableResultIsAnsweredWithInternalError(t *testing.T) {
	tests := []struct {
		name string
		call func(t *testing.T) []byte
	}{
		{
			name: "stdio",
			call: func(t *testing.T) []byte {
				var output bytes.Buffer
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				err := NewStdioServer(newUnencodableResultServer()).Listen(ctx, strings.NewReader(unencodableResultCall+"\n"), &output)
				require.NoError(t, err)
				return output.Bytes()
			},
		},
		{
			name: "sse",
			call: func(t *testing.T) []byte {
				ts := httptest.NewServer(NewSSEServer(newUnencodableResultServer()))
				defer ts.Close()

				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				sseRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/sse", nil)
				require.NoError(t, err)
				resp, err := http.DefaultClient.Do(sseRequest)
				require.NoError(t, err)
				defer resp.Body.Close()
				reader := bufio.NewReader(resp.Body)
				endpoint := readSSEEndpoint(t, reader)

				postResp, err := http.Post(ts.URL+endpoint, "application/json", strings.NewReader(unencodableResultCall))
				require.NoError(t, err)
				postResp.Body.Close()

				var message json.RawMessage
				readSSEData(t, reader, &message)
				return message
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var response mcp.JSONRPCError
			require.NoError(t, json.Unmarshal(tt.call(t), &response), "the client must get a JSON-RPC response")

			assert.Equal(t, mcp.NewRequestId(int64(7)), response.ID, "the response must carry the request's id")
			assert.Equal(t, mcp.INTERNAL_ERROR, response.Error.Code)
			assert.Contains(t, response.Error.Message, "failed to marshal response")
		})
	}
}
