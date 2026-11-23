package server

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The client and the server number their requests independently, so a client
// response may carry the id of a client request that is still running.
func TestStdioServer_ResponseSharingInflightRequestIDIsNotAnswered(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	mcpServer := NewMCPServer("test", "1.0.0", WithToolCapabilities(true))
	mcpServer.AddTool(mcp.NewTool("block"), func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		close(started)
		<-release
		return mcp.NewToolResultText("done"), nil
	})

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	listenDone := make(chan error, 1)
	go func() {
		listenDone <- NewStdioServer(mcpServer).Listen(ctx, stdinReader, stdoutWriter)
		_ = stdoutWriter.Close()
	}()

	messages := make(chan map[string]any, 8)
	go func() {
		defer close(messages)
		decoder := json.NewDecoder(stdoutReader)
		for {
			var msg map[string]any
			if decoder.Decode(&msg) != nil {
				return
			}
			messages <- msg
		}
	}()

	send := func(line string) {
		_, err := stdinWriter.Write([]byte(line + "\n"))
		require.NoError(t, err)
	}

	send(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"block","arguments":{}}}`)
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("tool did not start")
	}

	// Responses to server requests that are no longer pending, such as a
	// sampling request the handler gave up on.
	send(`{"jsonrpc":"2.0","id":1,"result":{}}`)
	send(`{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"late"}}`)
	send(`{"jsonrpc":"2.0","id":2,"method":"ping"}`)

	select {
	case msg := <-messages:
		assert.Equal(t, float64(2), msg["id"], "a response must not be answered: %v", msg)
		assert.NotContains(t, msg, "error")
	case <-ctx.Done():
		t.Fatal("timed out waiting for the ping response")
	}

	close(release)
	select {
	case msg := <-messages:
		assert.Equal(t, float64(1), msg["id"])
		assert.Contains(t, msg, "result")
	case <-ctx.Done():
		t.Fatal("timed out waiting for the tool result")
	}

	require.NoError(t, stdinWriter.Close())
	require.NoError(t, <-listenDone)
}
