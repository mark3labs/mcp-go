package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToolFilterCallTimeEnforcement verifies that tool filters are enforced
// during tools/call, not just tools/list. This prevents clients from
// bypassing visibility restrictions by calling a filtered-out tool directly.
func TestToolFilterCallTimeEnforcement(t *testing.T) {
	// Filter that only allows tools starting with "allow-"
	allowPrefixFilter := func(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
		var filtered []mcp.Tool
		for _, tool := range tools {
			if strings.HasPrefix(tool.Name, "allow-") {
				filtered = append(filtered, tool)
			}
		}
		return filtered
	}

	server := NewMCPServer("test-server", "1.0.0",
		WithToolCapabilities(true),
		WithToolFilter(allowPrefixFilter),
	)

	// Register both allowed and denied tools with real handlers
	server.AddTool(mcp.NewTool("allow-tool"), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("allowed"), nil
	})
	server.AddTool(mcp.NewTool("deny-tool"), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("should not reach"), nil
	})

	tests := []struct {
		name       string
		toolName   string
		wantError  bool
		wantResult string
	}{
		{
			name:       "allowed tool can be called",
			toolName:   "allow-tool",
			wantError:  false,
			wantResult: "allowed",
		},
		{
			name:      "filtered-out tool is rejected at call time",
			toolName:  "deny-tool",
			wantError: true,
		},
		{
			name:      "non-existent tool is rejected",
			toolName:  "no-such-tool",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callRequest := map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "tools/call",
				"params": map[string]any{
					"name": tt.toolName,
				},
			}
			requestBytes, err := json.Marshal(callRequest)
			require.NoError(t, err)

			response := server.HandleMessage(t.Context(), requestBytes)

			if tt.wantError {
				errResp, ok := response.(mcp.JSONRPCError)
				require.True(t, ok, "Expected JSONRPCError response for tool %q, got %T", tt.toolName, response)
				assert.Equal(t, mcp.INVALID_PARAMS, errResp.Error.Code)
				assert.Contains(t, errResp.Error.Message, "not found")
			} else {
				resp, ok := response.(mcp.JSONRPCResponse)
				require.True(t, ok, "Expected JSONRPCResponse for tool %q, got %T", tt.toolName, response)
				callToolResult, ok := resp.Result.(*mcp.CallToolResult)
				require.True(t, ok)
				require.NotEmpty(t, callToolResult.Content)
				textContent, ok := callToolResult.Content[0].(mcp.TextContent)
				require.True(t, ok)
				assert.Equal(t, tt.wantResult, textContent.Text)
			}
		})
	}
}

// TestToolFilterCallTimeWithMultipleFilters verifies that multiple chained
// filters are all enforced at tools/call time, matching tools/list behavior.
func TestToolFilterCallTimeWithMultipleFilters(t *testing.T) {
	// First filter: allow tools starting with "a"
	filterA := func(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
		var filtered []mcp.Tool
		for _, tool := range tools {
			if strings.HasPrefix(tool.Name, "a") {
				filtered = append(filtered, tool)
			}
		}
		return filtered
	}
	// Second filter: allow tools containing "keep"
	filterKeep := func(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
		var filtered []mcp.Tool
		for _, tool := range tools {
			if strings.Contains(tool.Name, "keep") {
				filtered = append(filtered, tool)
			}
		}
		return filtered
	}

	server := NewMCPServer("test-server", "1.0.0",
		WithToolCapabilities(true),
		WithToolFilter(filterA),
		WithToolFilter(filterKeep),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok:" + request.Params.Name), nil
	}

	server.AddTool(mcp.NewTool("a-keep-this"), handler)
	server.AddTool(mcp.NewTool("a-remove-this"), handler)
	server.AddTool(mcp.NewTool("b-keep-this"), handler)

	tests := []struct {
		name      string
		toolName  string
		wantError bool
	}{
		{
			name:      "passes both filters",
			toolName:  "a-keep-this",
			wantError: false,
		},
		{
			name:      "passes first but not second filter",
			toolName:  "a-remove-this",
			wantError: true,
		},
		{
			name:      "passes second but not first filter",
			toolName:  "b-keep-this",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callRequest := map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "tools/call",
				"params": map[string]any{
					"name": tt.toolName,
				},
			}
			requestBytes, err := json.Marshal(callRequest)
			require.NoError(t, err)

			response := server.HandleMessage(t.Context(), requestBytes)

			if tt.wantError {
				errResp, ok := response.(mcp.JSONRPCError)
				require.True(t, ok, "Expected error for %q", tt.toolName)
				assert.Equal(t, mcp.INVALID_PARAMS, errResp.Error.Code)
			} else {
				resp, ok := response.(mcp.JSONRPCResponse)
				require.True(t, ok, "Expected success for %q", tt.toolName)
				callToolResult, ok := resp.Result.(*mcp.CallToolResult)
				require.True(t, ok)
				require.NotEmpty(t, callToolResult.Content)
			}
		})
	}
}

// TestToolFilterCallTimeWithSessionTools verifies that tool filters are
// enforced at call time for session-specific tools too.
func TestToolFilterCallTimeWithSessionTools(t *testing.T) {
	allowPrefixFilter := func(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
		var filtered []mcp.Tool
		for _, tool := range tools {
			if strings.HasPrefix(tool.Name, "allow-") {
				filtered = append(filtered, tool)
			}
		}
		return filtered
	}

	server := NewMCPServer("test-server", "1.0.0",
		WithToolCapabilities(true),
		WithToolFilter(allowPrefixFilter),
	)

	// Create a session with tools
	session := &sessionTestClientWithTools{
		sessionID:           "session-1",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
		initialized:         true,
		sessionTools: map[string]ServerTool{
			"allow-session-tool": {
				Tool: mcp.NewTool("allow-session-tool"),
				Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return mcp.NewToolResultText("session-allowed"), nil
				},
			},
			"deny-session-tool": {
				Tool: mcp.NewTool("deny-session-tool"),
				Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return mcp.NewToolResultText("should not reach"), nil
				},
			},
		},
	}

	err := server.RegisterSession(t.Context(), session)
	require.NoError(t, err)

	sessionCtx := server.WithContext(t.Context(), session)

	tests := []struct {
		name      string
		toolName  string
		wantError bool
	}{
		{
			name:      "allowed session tool can be called",
			toolName:  "allow-session-tool",
			wantError: false,
		},
		{
			name:      "filtered-out session tool is rejected",
			toolName:  "deny-session-tool",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callRequest := map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "tools/call",
				"params": map[string]any{
					"name": tt.toolName,
				},
			}
			requestBytes, err := json.Marshal(callRequest)
			require.NoError(t, err)

			response := server.HandleMessage(sessionCtx, requestBytes)

			if tt.wantError {
				errResp, ok := response.(mcp.JSONRPCError)
				require.True(t, ok, "Expected error for session tool %q", tt.toolName)
				assert.Equal(t, mcp.INVALID_PARAMS, errResp.Error.Code)
			} else {
				resp, ok := response.(mcp.JSONRPCResponse)
				require.True(t, ok, "Expected success for session tool %q", tt.toolName)
				callToolResult, ok := resp.Result.(*mcp.CallToolResult)
				require.True(t, ok)
				require.NotEmpty(t, callToolResult.Content)
			}
		})
	}
}

// TestToolFilterCallTimeConsistencyWithList verifies that tools/list and
// tools/call agree on which tools are accessible — a tool visible in
// tools/list must be callable, and a tool hidden by filters must not.
func TestToolFilterCallTimeConsistencyWithList(t *testing.T) {
	allowPrefixFilter := func(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
		var filtered []mcp.Tool
		for _, tool := range tools {
			if strings.HasPrefix(tool.Name, "visible-") {
				filtered = append(filtered, tool)
			}
		}
		return filtered
	}

	server := NewMCPServer("test-server", "1.0.0",
		WithToolCapabilities(true),
		WithToolFilter(allowPrefixFilter),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("result:" + request.Params.Name), nil
	}

	server.AddTool(mcp.NewTool("visible-tool-1"), handler)
	server.AddTool(mcp.NewTool("visible-tool-2"), handler)
	server.AddTool(mcp.NewTool("hidden-tool-1"), handler)
	server.AddTool(mcp.NewTool("hidden-tool-2"), handler)

	ctx := t.Context()

	// Step 1: List tools and collect visible names
	listResponse := server.HandleMessage(ctx, []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "tools/list"
	}`))
	listResp, ok := listResponse.(mcp.JSONRPCResponse)
	require.True(t, ok)
	listResult, ok := listResp.Result.(mcp.ListToolsResult)
	require.True(t, ok)

	visibleNames := make(map[string]bool)
	for _, tool := range listResult.Tools {
		visibleNames[tool.Name] = true
	}

	// Step 2: Verify every registered tool name — visible ones should succeed,
	// hidden ones should fail at call time
	allToolNames := []string{"visible-tool-1", "visible-tool-2", "hidden-tool-1", "hidden-tool-2"}

	for _, name := range allToolNames {
		t.Run(name, func(t *testing.T) {
			callRequest := map[string]any{
				"jsonrpc": "2.0",
				"id":      2,
				"method":  "tools/call",
				"params": map[string]any{
					"name": name,
				},
			}
			requestBytes, err := json.Marshal(callRequest)
			require.NoError(t, err)

			response := server.HandleMessage(ctx, requestBytes)

			if visibleNames[name] {
				// Tool was in list — it must be callable
				resp, ok := response.(mcp.JSONRPCResponse)
				require.True(t, ok, "Visible tool %q should be callable", name)
				callToolResult, ok := resp.Result.(*mcp.CallToolResult)
				require.True(t, ok)
				require.NotEmpty(t, callToolResult.Content)
			} else {
				// Tool was NOT in list — it must be rejected
				errResp, ok := response.(mcp.JSONRPCError)
				require.True(t, ok, "Hidden tool %q should be rejected at call time", name)
				assert.Equal(t, mcp.INVALID_PARAMS, errResp.Error.Code)
			}
		})
	}
}

// TestToolFilterCallTimeNoFilters verifies that when no filters are configured,
// all tools remain callable (backward compatibility).
func TestToolFilterCallTimeNoFilters(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0",
		WithToolCapabilities(true),
	)

	server.AddTool(mcp.NewTool("any-tool"), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("works"), nil
	})

	callRequest := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "any-tool",
		},
	}
	requestBytes, err := json.Marshal(callRequest)
	require.NoError(t, err)

	response := server.HandleMessage(t.Context(), requestBytes)
	resp, ok := response.(mcp.JSONRPCResponse)
	require.True(t, ok, "Expected success with no filters")
	callToolResult, ok := resp.Result.(*mcp.CallToolResult)
	require.True(t, ok)
	require.NotEmpty(t, callToolResult.Content)
	textContent, ok := callToolResult.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Equal(t, "works", textContent.Text)
}

func TestToolFilterCallTimeFiltersOnlyRequestedTool(t *testing.T) {
	var filterInputs [][]string
	allowVisibleFilter := func(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
		names := make([]string, 0, len(tools))
		filtered := make([]mcp.Tool, 0, len(tools))
		for _, tool := range tools {
			names = append(names, tool.Name)
			if strings.HasPrefix(tool.Name, "visible-") {
				filtered = append(filtered, tool)
			}
		}
		filterInputs = append(filterInputs, names)
		return filtered
	}

	server := NewMCPServer("test-server", "1.0.0",
		WithToolCapabilities(true),
		WithToolFilter(allowVisibleFilter),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok:" + request.Params.Name), nil
	}

	server.AddTool(mcp.NewTool("visible-tool-1"), handler)
	server.AddTool(mcp.NewTool("visible-tool-2"), handler)
	server.AddTool(mcp.NewTool("visible-tool-3"), handler)
	server.AddTool(mcp.NewTool("hidden-tool-1"), handler)
	server.AddTool(mcp.NewTool("hidden-tool-2"), handler)
	server.AddTool(mcp.NewTool("hidden-tool-3"), handler)

	callRequest := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "visible-tool-3",
		},
	}
	requestBytes, err := json.Marshal(callRequest)
	require.NoError(t, err)

	response := server.HandleMessage(t.Context(), requestBytes)
	_, ok := response.(mcp.JSONRPCResponse)
	require.True(t, ok, "Expected success for visible tool, got %T", response)

	require.Len(t, filterInputs, 1)
	assert.Equal(t, []string{"visible-tool-3"}, filterInputs[0])
}

// TestToolFilterCallTimeErrorType verifies that the error returned for a
// filtered-out tool contains ErrToolNotFound, allowing callers to use
// errors.Is for programmatic inspection via hooks.
func TestToolFilterCallTimeErrorType(t *testing.T) {
	var capturedErr error
	hooks := &Hooks{}
	hooks.AddOnError(func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
		capturedErr = err
	})

	denyAllFilter := func(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
		return nil
	}

	server := NewMCPServer("test-server", "1.0.0",
		WithToolCapabilities(true),
		WithToolFilter(denyAllFilter),
		WithHooks(hooks),
	)

	server.AddTool(mcp.NewTool("blocked-tool"), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("should not reach"), nil
	})

	callRequest := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "blocked-tool",
		},
	}
	requestBytes, err := json.Marshal(callRequest)
	require.NoError(t, err)

	response := server.HandleMessage(t.Context(), requestBytes)

	// Verify error response
	_, ok := response.(mcp.JSONRPCError)
	require.True(t, ok)

	// Verify the error hook received ErrToolNotFound in the chain
	require.NotNil(t, capturedErr)
	assert.True(t, errors.Is(capturedErr, ErrToolNotFound),
		"Error should wrap ErrToolNotFound, got: %v", capturedErr)
}

// TestToolFilterCallTimeContextAware verifies that context-aware filters
// (e.g., based on session identity) work correctly at call time.
func TestToolFilterCallTimeContextAware(t *testing.T) {
	// Filter that uses session ID to decide tool access
	sessionAwareFilter := func(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
		session := ClientSessionFromContext(ctx)
		if session == nil {
			return tools // no session = no filtering
		}

		var filtered []mcp.Tool
		for _, tool := range tools {
			// Session "admin-session" gets all tools,
			// other sessions only get tools starting with "public-"
			if session.SessionID() == "admin-session" || strings.HasPrefix(tool.Name, "public-") {
				filtered = append(filtered, tool)
			}
		}
		return filtered
	}

	server := NewMCPServer("test-server", "1.0.0",
		WithToolCapabilities(true),
		WithToolFilter(sessionAwareFilter),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok:" + request.Params.Name), nil
	}

	server.AddTool(mcp.NewTool("public-tool"), handler)
	server.AddTool(mcp.NewTool("admin-tool"), handler)

	// Create two sessions
	adminSession := &sessionTestClientWithTools{
		sessionID:           "admin-session",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
		initialized:         true,
	}
	userSession := &sessionTestClientWithTools{
		sessionID:           "user-session",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
		initialized:         true,
	}

	err := server.RegisterSession(t.Context(), adminSession)
	require.NoError(t, err)
	err = server.RegisterSession(t.Context(), userSession)
	require.NoError(t, err)

	tests := []struct {
		name      string
		session   ClientSession
		toolName  string
		wantError bool
	}{
		{
			name:      "admin can call public tool",
			session:   adminSession,
			toolName:  "public-tool",
			wantError: false,
		},
		{
			name:      "admin can call admin tool",
			session:   adminSession,
			toolName:  "admin-tool",
			wantError: false,
		},
		{
			name:      "user can call public tool",
			session:   userSession,
			toolName:  "public-tool",
			wantError: false,
		},
		{
			name:      "user cannot call admin tool",
			session:   userSession,
			toolName:  "admin-tool",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessionCtx := server.WithContext(t.Context(), tt.session)
			callRequest := map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "tools/call",
				"params": map[string]any{
					"name": tt.toolName,
				},
			}
			requestBytes, err := json.Marshal(callRequest)
			require.NoError(t, err)

			response := server.HandleMessage(sessionCtx, requestBytes)

			if tt.wantError {
				errResp, ok := response.(mcp.JSONRPCError)
				require.True(t, ok, "Expected error for %q with session %q", tt.toolName, tt.session.SessionID())
				assert.Equal(t, mcp.INVALID_PARAMS, errResp.Error.Code)
			} else {
				resp, ok := response.(mcp.JSONRPCResponse)
				require.True(t, ok, "Expected success for %q with session %q", tt.toolName, tt.session.SessionID())
				callToolResult, ok := resp.Result.(*mcp.CallToolResult)
				require.True(t, ok)
				require.NotEmpty(t, callToolResult.Content)
			}
		})
	}
}

func TestPromptFilterGetTimeFiltersOnlyRequestedPrompt(t *testing.T) {
	var filterInputs [][]string
	allowVisibleFilter := func(ctx context.Context, prompts []mcp.Prompt) []mcp.Prompt {
		names := make([]string, 0, len(prompts))
		filtered := make([]mcp.Prompt, 0, len(prompts))
		for _, prompt := range prompts {
			names = append(names, prompt.Name)
			if strings.HasPrefix(prompt.Name, "visible-") {
				filtered = append(filtered, prompt)
			}
		}
		filterInputs = append(filterInputs, names)
		return filtered
	}

	server := NewMCPServer("test-server", "1.0.0",
		WithPromptCapabilities(true),
		WithPromptFilter(allowVisibleFilter),
	)

	handler := func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Messages: []mcp.PromptMessage{
				{
					Role:    mcp.RoleUser,
					Content: mcp.TextContent{Type: "text", Text: "result:" + request.Params.Name},
				},
			},
		}, nil
	}

	server.AddPrompt(mcp.Prompt{Name: "visible-prompt-1"}, handler)
	server.AddPrompt(mcp.Prompt{Name: "visible-prompt-2"}, handler)
	server.AddPrompt(mcp.Prompt{Name: "hidden-prompt-1"}, handler)
	server.AddPrompt(mcp.Prompt{Name: "hidden-prompt-2"}, handler)

	getRequest := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "prompts/get",
		"params": map[string]any{
			"name": "visible-prompt-2",
		},
	}
	requestBytes, err := json.Marshal(getRequest)
	require.NoError(t, err)

	response := server.HandleMessage(t.Context(), requestBytes)
	_, ok := response.(mcp.JSONRPCResponse)
	require.True(t, ok, "Expected success for visible prompt, got %T", response)

	require.Len(t, filterInputs, 1)
	assert.Equal(t, []string{"visible-prompt-2"}, filterInputs[0])
}

// TestPromptFilterGetTimeEnforcement verifies that prompt filters are enforced
// during prompts/get, not just prompts/list.
func TestPromptFilterGetTimeEnforcement(t *testing.T) {
	allowPrefixFilter := func(ctx context.Context, prompts []mcp.Prompt) []mcp.Prompt {
		var filtered []mcp.Prompt
		for _, prompt := range prompts {
			if strings.HasPrefix(prompt.Name, "allow-") {
				filtered = append(filtered, prompt)
			}
		}
		return filtered
	}

	server := NewMCPServer("test-server", "1.0.0",
		WithPromptCapabilities(true),
		WithPromptFilter(allowPrefixFilter),
	)

	handler := func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Messages: []mcp.PromptMessage{
				{
					Role:    mcp.RoleUser,
					Content: mcp.TextContent{Type: "text", Text: "result:" + request.Params.Name},
				},
			},
		}, nil
	}

	server.AddPrompt(mcp.Prompt{Name: "allow-prompt"}, handler)
	server.AddPrompt(mcp.Prompt{Name: "deny-prompt"}, handler)

	tests := []struct {
		name       string
		promptName string
		wantError  bool
	}{
		{
			name:       "allowed prompt can be retrieved",
			promptName: "allow-prompt",
			wantError:  false,
		},
		{
			name:       "filtered-out prompt is rejected at get time",
			promptName: "deny-prompt",
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getRequest := map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "prompts/get",
				"params": map[string]any{
					"name": tt.promptName,
				},
			}
			requestBytes, err := json.Marshal(getRequest)
			require.NoError(t, err)

			response := server.HandleMessage(t.Context(), requestBytes)

			if tt.wantError {
				errResp, ok := response.(mcp.JSONRPCError)
				require.True(t, ok, "Expected error for prompt %q, got %T", tt.promptName, response)
				assert.Equal(t, mcp.INVALID_PARAMS, errResp.Error.Code)
				assert.Contains(t, errResp.Error.Message, "not found")
			} else {
				resp, ok := response.(mcp.JSONRPCResponse)
				require.True(t, ok, "Expected success for prompt %q", tt.promptName)
				result, ok := resp.Result.(mcp.GetPromptResult)
				require.True(t, ok)
				require.NotEmpty(t, result.Messages)
			}
		})
	}
}
