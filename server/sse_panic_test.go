package server

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	reader := bufio.NewReader(resp.Body)
	endpoint := readSSEEndpoint(t, reader)

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
	var errResp mcp.JSONRPCError
	readSSEData(t, reader, &errResp)

	// Verify the error response was delivered via SSE
	assert.Equal(t, mcp.INTERNAL_ERROR, errResp.Error.Code, "client should receive INTERNAL_ERROR code for panicking tool call")
	assert.Contains(t, errResp.Error.Message, "internal panic", "error message should indicate a panic occurred")
	// The client matches the error to its pending call by id, so it must be
	// the id of the request that panicked.
	assert.Equal(t, toolCall.ID, errResp.ID, "error response should carry the request's id")

	// Server should still be alive. Send a ping to verify.
	ping := `{"jsonrpc":"2.0","id":2,"method":"ping"}`
	pingResp, err := http.Post(ts.URL+endpoint, "application/json", strings.NewReader(ping))
	require.NoError(t, err)
	defer pingResp.Body.Close()
	assert.Less(t, pingResp.StatusCode, 300, "server should still respond after panic recovery")
}

// A notification gets no reply, not even an error when its handler panics.
func TestSSEServer_NotificationHandlerPanicGetsNoReply(t *testing.T) {
	server := NewMCPServer("test", "1.0.0")
	panicking := make(chan struct{})
	server.AddNotificationHandler("notifications/boom", func(context.Context, mcp.JSONRPCNotification) {
		close(panicking)
		panic("deliberate panic in notification handler")
	})

	sseServer := NewSSEServer(server)
	ts := httptest.NewServer(sseServer)
	defer ts.Close()

	// Bound the reads below, so a missing event fails the test instead of
	// hanging it.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	sseRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/sse", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(sseRequest)
	require.NoError(t, err)
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	endpoint := readSSEEndpoint(t, reader)

	post := func(body string) {
		t.Helper()
		postResp, err := http.Post(ts.URL+endpoint, "application/json", strings.NewReader(body))
		require.NoError(t, err)
		postResp.Body.Close()
	}
	post(`{"jsonrpc":"2.0","method":"notifications/boom"}`)
	select {
	case <-panicking:
	case <-time.After(5 * time.Second):
		t.Fatal("notification handler was not called")
	}
	post(`{"jsonrpc":"2.0","id":2,"method":"ping"}`)

	var response struct {
		ID    any             `json:"id"`
		Error json.RawMessage `json:"error"`
	}
	readSSEData(t, reader, &response)
	assert.Equal(t, float64(2), response.ID, "the first event should answer the ping")
	assert.Empty(t, string(response.Error))
}

// readSSEEndpoint reads the endpoint event an SSE connection starts with.
func readSSEEndpoint(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	for {
		line, err := reader.ReadString('\n')
		require.NoError(t, err)
		if endpoint, ok := strings.CutPrefix(strings.TrimRight(line, "\r\n"), "data: "); ok {
			return endpoint
		}
	}
}
