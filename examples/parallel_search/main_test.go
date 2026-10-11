package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name  string
		query string
		url   string
		calls []string
	}{
		{name: "search", query: "Go context cancellation examples", calls: []string{"web_search"}},
		{name: "fetch", url: "https://go.dev/blog/context", calls: []string{"web_fetch"}},
		{name: "search and fetch", query: "Go context cancellation examples", url: "https://go.dev/blog/context", calls: []string{"web_search", "web_fetch"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := server.NewMCPServer("fixture", "1.0.0", server.WithToolCapabilities(false))
			var mu sync.Mutex
			var calls []mcp.CallToolRequest
			for _, name := range []string{"web_search", "web_fetch"} {
				s.AddTool(mcp.NewTool(name), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					mu.Lock()
					defer mu.Unlock()
					calls = append(calls, req)
					return mcp.NewToolResultText(req.Params.Name + ": https://go.dev/blog/context\nUseful context cancellation excerpts"), nil
				})
			}
			handler := server.NewStreamableHTTPServer(s)
			defer handler.Shutdown(t.Context())
			var methods []string
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/mcp", r.URL.Path)
				if r.Method == http.MethodPost {
					assert.Equal(t, userAgent, r.UserAgent())
					assert.Empty(t, r.Header.Get("Authorization"))
					assert.Empty(t, r.Header.Get("X-API-Key"))
					body, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					var message struct{ Method string }
					require.NoError(t, json.Unmarshal(body, &message))
					mu.Lock()
					methods = append(methods, message.Method)
					mu.Unlock()
					r.Body = io.NopCloser(bytes.NewReader(body))
				}
				handler.ServeHTTP(w, r)
			}))
			defer ts.Close()
			var out bytes.Buffer
			require.NoError(t, run(t.Context(), ts.URL+"/mcp", tt.query, tt.url, &out))
			mu.Lock()
			defer mu.Unlock()
			assert.Contains(t, methods, "server/discover")
			assert.Contains(t, methods, "tools/list")
			require.Len(t, calls, len(tt.calls))
			var sessionID string
			for i, call := range calls {
				assert.Equal(t, tt.calls[i], call.Params.Name)
				assert.Contains(t, out.String(), call.Params.Name+": https://go.dev/blog/context")
				args := call.GetArguments()
				id, ok := args["session_id"].(string)
				require.True(t, ok)
				_, err := uuid.Parse(id)
				require.NoError(t, err)
				if sessionID == "" {
					sessionID = id
				}
				assert.Equal(t, sessionID, id)
				assert.NotContains(t, args, "model_name")
				if call.Params.Name == "web_search" {
					assert.Equal(t, tt.query, args["objective"])
				} else {
					assert.Equal(t, []any{tt.url}, args["urls"])
				}
				if tt.query != "" {
					assert.Equal(t, []any{tt.query}, args["search_queries"])
				}
			}
		})
	}
	assert.Equal(t, "https://search.parallel.ai/mcp", endpoint)
}

func TestRunErrors(t *testing.T) {
	tests := []struct {
		name    string
		tool    bool
		isError bool
		delay   time.Duration
		wantErr string
	}{
		{name: "missing tool", wantErr: "server does not offer web_search"},
		{name: "tool error", tool: true, isError: true, wantErr: "web_search returned a tool error"},
		{name: "timeout", tool: true, delay: time.Second, wantErr: "context deadline exceeded"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := server.NewMCPServer("fixture", "1.0.0", server.WithToolCapabilities(false))
			if tt.tool {
				s.AddTool(mcp.NewTool("web_search"), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					if tt.delay > 0 {
						select {
						case <-ctx.Done():
							return nil, ctx.Err()
						case <-time.After(tt.delay):
						}
					}
					if tt.isError {
						return mcp.NewToolResultError("rate limit reached"), nil
					}
					return mcp.NewToolResultText("ok"), nil
				})
			}
			handler := server.NewStreamableHTTPServer(s)
			defer handler.Shutdown(t.Context())
			ts := httptest.NewServer(handler)
			defer ts.Close()
			ctx := t.Context()
			if tt.delay > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 200*time.Millisecond)
				defer cancel()
			}
			var out bytes.Buffer
			err := run(ctx, ts.URL+"/mcp", "Go context cancellation examples", "", &out)
			require.ErrorContains(t, err, tt.wantErr)
			if tt.isError {
				assert.Contains(t, out.String(), "rate limit reached")
			}
		})
	}
}

func TestRunCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := run(ctx, endpoint, "Go context cancellation examples", "", io.Discard)
	require.ErrorIs(t, err, context.Canceled)
}
