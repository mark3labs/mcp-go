package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newLoggerCapturingTo(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// decodeLines splits the captured JSON-lines buffer into one map per line so
// individual assertions don't depend on field order.
func decodeLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for line := range strings.SplitSeq(strings.TrimRight(buf.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m), "invalid JSON log line: %s", line)
		out = append(out, m)
	}
	return out
}

func findLine(lines []map[string]any, msg string) map[string]any {
	for _, l := range lines {
		if l["msg"] == msg {
			return l
		}
	}
	return nil
}

func TestWithLogger_NilIsNoOp(t *testing.T) {
	s := NewMCPServer("logger-srv", "1.0", WithLogger(nil))
	require.Nil(t, s.requestLogger, "WithLogger(nil) must leave requestLogger unset")
}

// TestWithLogger_RequestLine covers startMessageLog's two outcomes — a
// successful dispatch (resp == nil) and a JSON-RPC error response.
func TestWithLogger_RequestLine(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		resp        mcp.JSONRPCMessage
		wantOutcome string
		wantError   string
	}{
		{
			name:        "ok outcome on nil response",
			method:      "tools/list",
			resp:        nil,
			wantOutcome: logOutcomeOK,
		},
		{
			name:        "error outcome on JSONRPCError",
			method:      "tools/call",
			resp:        mcp.NewJSONRPCError(mcp.NewRequestId(1), mcp.INVALID_PARAMS, "bad params", nil),
			wantOutcome: logOutcomeError,
			wantError:   "bad params",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			s := NewMCPServer("logger-srv", "1.0", WithLogger(newLoggerCapturingTo(&buf)))

			end := s.startMessageLog(t.Context(), nil, tc.method)
			end(tc.resp)

			line := findLine(decodeLines(t, &buf), logMessageRequest)
			require.NotNil(t, line, "expected one %q line; buf=%s", logMessageRequest, buf.String())
			assert.Equal(t, tc.method, line[logKeyMethod])
			assert.Equal(t, tc.wantOutcome, line[logKeyOutcome])
			assert.Contains(t, line, logKeyDurationSeconds, "request line missing %s", logKeyDurationSeconds)
			if tc.wantError != "" {
				assert.Equal(t, tc.wantError, line[logKeyError])
			}
		})
	}
}

// TestWithLogger_ToolMiddleware drives the tool middleware directly across
// the three handler outcomes (ok handler, handler error, IsError result)
// and asserts the emitted mcp.tool line.
func TestWithLogger_ToolMiddleware(t *testing.T) {
	tests := []struct {
		name        string
		handler     ToolHandlerFunc
		wantOutcome string
		wantError   string
	}{
		{
			name: "ok outcome",
			handler: func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{}, nil
			},
			wantOutcome: logOutcomeOK,
		},
		{
			name: "handler error",
			handler: func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return nil, errors.New("boom")
			},
			wantOutcome: logOutcomeError,
			wantError:   "boom",
		},
		{
			name: "IsError result",
			handler: func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{IsError: true}, nil
			},
			wantOutcome: logOutcomeErrorResult,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			wrapped := toolLoggingMiddleware(newLoggerCapturingTo(&buf))(tc.handler)
			_, _ = wrapped(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "list_pods"}})

			line := findLine(decodeLines(t, &buf), logMessageTool)
			require.NotNil(t, line, "expected %q line; buf=%s", logMessageTool, buf.String())
			assert.Equal(t, "list_pods", line[logKeyToolName])
			assert.Equal(t, tc.wantOutcome, line[logKeyOutcome])
			if tc.wantError != "" {
				assert.Equal(t, tc.wantError, line[logKeyError])
			}
		})
	}
}

// TestWithLogger_RegistersToolMiddleware asserts that WithLogger appends a
// tool middleware so the chain emits mcp.tool lines when run against a
// happy-path handler. Separate from TestWithLogger_ToolMiddleware because
// it exercises the middleware-registration plumbing rather than the
// middleware function directly.
func TestWithLogger_RegistersToolMiddleware(t *testing.T) {
	var buf bytes.Buffer
	s := NewMCPServer("logger-srv", "1.0", WithLogger(newLoggerCapturingTo(&buf)))

	called := false
	handler := func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		called = true
		return &mcp.CallToolResult{}, nil
	}

	s.toolMiddlewareMu.RLock()
	mws := append([]ToolHandlerMiddleware(nil), s.toolHandlerMiddlewares...)
	s.toolMiddlewareMu.RUnlock()
	require.NotEmpty(t, mws, "WithLogger must register at least one tool middleware")

	wrapped := ToolHandlerFunc(handler)
	for i := len(mws) - 1; i >= 0; i-- {
		wrapped = mws[i](wrapped)
	}
	_, _ = wrapped(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "list_pods"}})

	require.True(t, called, "middleware did not invoke handler")
	line := findLine(decodeLines(t, &buf), logMessageTool)
	require.NotNil(t, line)
	assert.Equal(t, "list_pods", line[logKeyToolName])
	assert.Equal(t, logOutcomeOK, line[logKeyOutcome])
}
