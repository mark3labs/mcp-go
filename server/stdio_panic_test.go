package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStdioServer_ToolCallWorkerPanicRecovery(t *testing.T) {
	// Create a server with a tool that panics and one that works
	server := NewMCPServer("test", "1.0.0", WithToolCapabilities(true))
	server.AddTools(
		ServerTool{
			Tool: mcp.Tool{
				Name:        "panic-tool",
				Description: "A tool that panics",
			},
			Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				panic("deliberate panic in stdio handler")
			},
		},
		ServerTool{
			Tool: mcp.Tool{
				Name:        "safe-tool",
				Description: "A tool that works",
			},
			Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return mcp.NewToolResultText("ok"), nil
			},
		},
	)

	stdioServer := NewStdioServer(server)

	// Build input: initialize, then panic tool call, then safe tool call
	var input bytes.Buffer
	initMsg := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`
	panicMsg := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"panic-tool","arguments":{}}}`
	safeMsg := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"safe-tool","arguments":{}}}`
	input.WriteString(initMsg + "\n")
	input.WriteString(panicMsg + "\n")
	input.WriteString(safeMsg + "\n")

	var output bytes.Buffer
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	err := stdioServer.Listen(ctx, &input, &output)
	// Listen returns nil on EOF (input exhausted)
	require.NoError(t, err)

	// Parse responses
	scanner := bufio.NewScanner(strings.NewReader(output.String()))
	var responses []json.RawMessage
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		responses = append(responses, json.RawMessage(line))
	}

	// Should have at least 3 responses (initialize + panic error + safe result)
	require.GreaterOrEqual(t, len(responses), 3, "expected at least 3 responses, got %d", len(responses))

	// Verify: panic produces an error response AND safe tool gets a result
	var foundPanicError, foundSafeResponse bool
	for _, resp := range responses {
		var msg struct {
			ID     any             `json:"id"`
			Result json.RawMessage `json:"result,omitempty"`
			Error  json.RawMessage `json:"error,omitempty"`
		}
		if err := json.Unmarshal(resp, &msg); err != nil {
			continue
		}

		// The panic recovery sends id:null (request ID not available in recover scope)
		if msg.Error != nil && strings.Contains(string(msg.Error), "internal panic") {
			foundPanicError = true
		}

		// Check for id=3 (safe tool response)
		if id, ok := msg.ID.(float64); ok && int(id) == 3 {
			foundSafeResponse = true
			assert.NotNil(t, msg.Result, "safe tool should have a result")
			assert.Nil(t, msg.Error, "safe tool should not have an error")
		}
	}
	assert.True(t, foundPanicError, "client should receive INTERNAL_ERROR for panicking tool call")
	assert.True(t, foundSafeResponse, "worker should survive panic and process subsequent tool calls")
}
