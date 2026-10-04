package server

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestStdioServerRejectsInvalidToolCallIDBeforeHandling(t *testing.T) {
	server := NewMCPServer("test", "1.0.0", WithToolCapabilities(true))
	toolCalled := false
	server.AddTool(mcp.NewTool("test_tool"), func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		toolCalled = true
		return mcp.NewToolResultText("called"), nil
	})

	stdioServer := NewStdioServer(server)
	var output bytes.Buffer
	message := `{"jsonrpc":"2.0","id":{"unexpected":true},"method":"tools/call","params":{"name":"test_tool","arguments":{}}}`

	require.NoError(t, stdioServer.processMessage(t.Context(), message, &output))
	require.False(t, toolCalled, "a tools/call with an invalid id must not reach its handler")

	var response struct {
		ID    json.RawMessage `json:"id"`
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(output.Bytes(), &response))
	require.JSONEq(t, `null`, string(response.ID))
	require.Equal(t, mcp.INVALID_REQUEST, response.Error.Code)
}
