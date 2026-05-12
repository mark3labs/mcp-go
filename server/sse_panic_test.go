package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSSEServer_MessageHandlerPanicRecovery(t *testing.T) {
	// Create a server with a tool that panics
	server := NewMCPServer("test", "1.0.0", WithToolCapabilities(true))
	server.AddTools(ServerTool{
		Tool: mcp.Tool{
			Name:        "panic-tool",
			Description: "A tool that panics",
		},
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			panic("deliberate panic in SSE handler")
		},
	})

	sseServer := NewSSEServer(server)
	ts := httptest.NewServer(sseServer)
	defer ts.Close()

	// Connect SSE session
	resp, err := http.Get(ts.URL + "/sse")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Read the endpoint event
	buf := make([]byte, 4096)
	n, err := resp.Body.Read(buf)
	require.NoError(t, err)
	body := string(buf[:n])
	require.Contains(t, body, "event: endpoint")

	// Extract message endpoint
	var endpoint string
	for line := range strings.SplitSeq(body, "\n") {
		if after, ok := strings.CutPrefix(line, "data: "); ok {
			endpoint = strings.TrimSpace(after)
			break
		}
	}
	require.NotEmpty(t, endpoint)

	// Send a tool call that will panic
	toolCall := mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(int64(1)),
		Request: mcp.Request{Method: string(mcp.MethodToolsCall)},
	}
	toolCall.Params = json.RawMessage(`{"name":"panic-tool","arguments":{}}`)

	callBody, _ := json.Marshal(toolCall)
	postResp, err := http.Post(ts.URL+endpoint, "application/json", strings.NewReader(string(callBody)))
	require.NoError(t, err)
	postResp.Body.Close()

	// Read from the SSE stream to get the error response for the panicking tool call.
	// The response is delivered as an SSE event on the original connection.
	buf = make([]byte, 4096)
	n, err = resp.Body.Read(buf)
	require.NoError(t, err)
	sseData := string(buf[:n])

	// Verify the error response was delivered via SSE (code -32603 = INTERNAL_ERROR)
	assert.Contains(t, sseData, "-32603", "client should receive INTERNAL_ERROR code for panicking tool call")
	assert.Contains(t, sseData, "internal panic", "error message should indicate a panic occurred")

	// Server should still be alive. Send a ping to verify.
	ping := `{"jsonrpc":"2.0","id":2,"method":"ping"}`
	pingResp, err := http.Post(ts.URL+endpoint, "application/json", strings.NewReader(ping))
	require.NoError(t, err)
	defer pingResp.Body.Close()
	assert.Less(t, pingResp.StatusCode, 300, "server should still respond after panic recovery")
}
