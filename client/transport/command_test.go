package transport

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCommandTransport_Constructors verifies that CommandTransport constructors
// produce a transport with the expected wrapped Stdio configuration without
// actually spawning the subprocess.
func TestCommandTransport_Constructors(t *testing.T) {
	t.Run("NewCommand_NoEnv", func(t *testing.T) {
		ct := NewCommand("my-server", "--flag", "value")
		require.NotNil(t, ct)
		require.NotNil(t, ct.Stdio)
		assert.Equal(t, "my-server", ct.command)
		assert.Equal(t, []string{"--flag", "value"}, ct.args)
		assert.Nil(t, ct.env)
	})

	t.Run("NewCommand_NoArgs", func(t *testing.T) {
		ct := NewCommand("my-server")
		require.NotNil(t, ct)
		assert.Equal(t, "my-server", ct.command)
		assert.Empty(t, ct.args)
	})

	t.Run("NewCommandWithEnv", func(t *testing.T) {
		env := []string{"API_KEY=secret", "DEBUG=1"}
		ct := NewCommandWithEnv("my-server", env, "--port", "8080")
		require.NotNil(t, ct)
		assert.Equal(t, "my-server", ct.command)
		assert.Equal(t, []string{"--port", "8080"}, ct.args)
		assert.Equal(t, env, ct.env)
	})

	t.Run("NewCommandWithOptions_AppliesOptions", func(t *testing.T) {
		var optApplied bool
		opt := func(s *Stdio) {
			optApplied = true
		}
		ct := NewCommandWithOptions("echo", nil, []string{"hi"}, opt)
		require.NotNil(t, ct)
		assert.True(t, optApplied)
	})
}

// TestCommandTransport_ImplementsInterface verifies that *CommandTransport
// satisfies the transport Interface used by the client.
func TestCommandTransport_ImplementsInterface(t *testing.T) {
	var _ Interface = (*CommandTransport)(nil)
}

// TestCommandTransport_StderrAccessor verifies that Stderr() is reachable
// via the embedded Stdio after the subprocess has been spawned.
func TestCommandTransport_StderrAccessor(t *testing.T) {
	ctx := t.Context()

	ct := NewCommand("echo", "hello")
	require.NotNil(t, ct)

	require.NoError(t, ct.spawnCommand(ctx))
	t.Cleanup(func() {
		_ = ct.cmd.Process.Kill()
	})

	require.Equal(t, "echo", filepath.Base(ct.cmd.Path))
	require.Contains(t, ct.cmd.Args, "hello")
	require.NotNil(t, ct.Stderr())
}

// TestCommandTransport_EndToEnd exercises the full request/response loop
// against the mock stdio server, ensuring the wrapper integrates correctly
// with the underlying Stdio implementation.
func TestCommandTransport_EndToEnd(t *testing.T) {
	tempFile, err := os.CreateTemp(t.TempDir(), "mockstdio_server")
	require.NoError(t, err)
	tempFile.Close()
	mockServerPath := tempFile.Name() + ".exe"

	require.NoError(t, compileTestServer(mockServerPath))

	ct := NewCommand(mockServerPath)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	require.NoError(t, ct.Start(ctx))
	defer ct.Close()

	request := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(int64(1)),
		Method:  "debug/echo",
		Params: map[string]any{
			"string": "hello world",
		},
	}

	response, err := ct.SendRequest(ctx, request)
	require.NoError(t, err)

	var result struct {
		JSONRPC string         `json:"jsonrpc"`
		ID      mcp.RequestId  `json:"id"`
		Method  string         `json:"method"`
		Params  map[string]any `json:"params"`
	}
	require.NoError(t, json.Unmarshal(response.Result, &result))

	assert.Equal(t, "2.0", result.JSONRPC)
	assert.Equal(t, "debug/echo", result.Method)
	assert.Equal(t, "hello world", result.Params["string"])
}

// TestCommandTransport_WithEnv verifies that NewCommandWithEnv propagates the
// provided environment variables to the spawned subprocess in addition to the
// inherited parent environment.
func TestCommandTransport_WithEnv(t *testing.T) {
	ctx := t.Context()
	t.Setenv("PARENT_ENV_VAR", "parent")

	ct := NewCommandWithEnv("echo", []string{"CHILD_ENV_VAR=child"}, "hi")
	require.NoError(t, ct.spawnCommand(ctx))
	t.Cleanup(func() {
		_ = ct.cmd.Process.Kill()
	})

	assert.Contains(t, ct.cmd.Env, "PARENT_ENV_VAR=parent")
	assert.Contains(t, ct.cmd.Env, "CHILD_ENV_VAR=child")
}
