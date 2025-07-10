package server

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

// sessionTestClient implements the basic ClientSession interface for testing
type sessionTestClient struct {
	sessionID           string
	notificationChannel chan mcp.JSONRPCNotification
	initialized         bool
}

func (f sessionTestClient) SessionID() string {
	return f.sessionID
}

func (f sessionTestClient) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return f.notificationChannel
}

// Initialize marks the session as initialized
// This implementation properly sets the initialized flag to true
// as required by the interface contract
func (f *sessionTestClient) Initialize() {
	f.initialized = true
}

// Initialized returns whether the session has been initialized
func (f sessionTestClient) Initialized() bool {
	return f.initialized
}

// sessionTestClientWithTools implements the SessionWithTools interface for testing
type sessionTestClientWithTools struct {
	sessionID           string
	notificationChannel chan mcp.JSONRPCNotification
	initialized         bool
	sessionTools        map[string]ServerTool
	mu                  sync.RWMutex // Mutex to protect concurrent access to sessionTools
}

func (f *sessionTestClientWithTools) SessionID() string {
	return f.sessionID
}

func (f *sessionTestClientWithTools) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return f.notificationChannel
}

func (f *sessionTestClientWithTools) Initialize() {
	f.initialized = true
}

func (f *sessionTestClientWithTools) Initialized() bool {
	return f.initialized
}

func (f *sessionTestClientWithTools) GetSessionTools() map[string]ServerTool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Return a copy of the map to prevent concurrent modification
	if f.sessionTools == nil {
		return nil
	}

	toolsCopy := make(map[string]ServerTool, len(f.sessionTools))
	for k, v := range f.sessionTools {
		toolsCopy[k] = v
	}
	return toolsCopy
}

func (f *sessionTestClientWithTools) SetSessionTools(tools map[string]ServerTool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Create a copy of the map to prevent concurrent modification
	if tools == nil {
		f.sessionTools = nil
		return
	}

	toolsCopy := make(map[string]ServerTool, len(tools))
	for k, v := range tools {
		toolsCopy[k] = v
	}
	f.sessionTools = toolsCopy
}

// sessionTestClientWithClientInfo implements the SessionWithClientInfo interface for testing
type sessionTestClientWithClientInfo struct {
	sessionID           string
	notificationChannel chan mcp.JSONRPCNotification
	initialized         bool
	clientInfo          atomic.Value
}

func (f *sessionTestClientWithClientInfo) SessionID() string {
	return f.sessionID
}

func (f *sessionTestClientWithClientInfo) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return f.notificationChannel
}

func (f *sessionTestClientWithClientInfo) Initialize() {
	f.initialized = true
}

func (f *sessionTestClientWithClientInfo) Initialized() bool {
	return f.initialized
}

func (f *sessionTestClientWithClientInfo) GetClientInfo() mcp.Implementation {
	if value := f.clientInfo.Load(); value != nil {
		if clientInfo, ok := value.(mcp.Implementation); ok {
			return clientInfo
		}
	}
	return mcp.Implementation{}
}

func (f *sessionTestClientWithClientInfo) SetClientInfo(clientInfo mcp.Implementation) {
	f.clientInfo.Store(clientInfo)
}

// sessionTestClientWithLogging implements the SessionWithLogging interface for testing
type sessionTestClientWithLogging struct {
	sessionID           string
	notificationChannel chan mcp.JSONRPCNotification
	initialized         bool
	loggingLevel        atomic.Value
}

func (f *sessionTestClientWithLogging) SessionID() string {
	return f.sessionID
}

func (f *sessionTestClientWithLogging) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return f.notificationChannel
}

func (f *sessionTestClientWithLogging) Initialize() {
	// set default logging level
	f.loggingLevel.Store(mcp.LoggingLevelError)
	f.initialized = true
}

func (f *sessionTestClientWithLogging) Initialized() bool {
	return f.initialized
}

func (f *sessionTestClientWithLogging) SetLogLevel(level mcp.LoggingLevel) {
	f.loggingLevel.Store(level)
}

func (f *sessionTestClientWithLogging) GetLogLevel() mcp.LoggingLevel {
	level := f.loggingLevel.Load()
	return level.(mcp.LoggingLevel)
}

// sessionTestClientWithPrompts implements the SessionWithPrompts interface for testing
type sessionTestClientWithPrompts struct {
	sessionID           string
	notificationChannel chan mcp.JSONRPCNotification
	initialized         bool
	sessionPrompts      map[string]ServerPrompt
	mu                  sync.RWMutex // Mutex to protect concurrent access to sessionPrompts
}

func (f *sessionTestClientWithPrompts) SessionID() string {
	return f.sessionID
}

func (f *sessionTestClientWithPrompts) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return f.notificationChannel
}

func (f *sessionTestClientWithPrompts) Initialize() {
	f.initialized = true
}

func (f *sessionTestClientWithPrompts) Initialized() bool {
	return f.initialized
}

func (f *sessionTestClientWithPrompts) GetSessionPrompts() map[string]ServerPrompt {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Return a copy of the map to prevent concurrent modification
	if f.sessionPrompts == nil {
		return nil
	}

	promptsCopy := make(map[string]ServerPrompt, len(f.sessionPrompts))
	for k, v := range f.sessionPrompts {
		promptsCopy[k] = v
	}
	return promptsCopy
}

func (f *sessionTestClientWithPrompts) SetSessionPrompts(prompts map[string]ServerPrompt) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if prompts == nil {
		f.sessionPrompts = nil
		return
	}

	promptsCopy := make(map[string]ServerPrompt, len(prompts))
	for k, v := range prompts {
		promptsCopy[k] = v
	}
	f.sessionPrompts = promptsCopy
}

// Verify that all implementations satisfy their respective interfaces
var (
	_ ClientSession         = (*sessionTestClient)(nil)
	_ SessionWithTools      = (*sessionTestClientWithTools)(nil)
	_ SessionWithLogging    = (*sessionTestClientWithLogging)(nil)
	_ SessionWithClientInfo = (*sessionTestClientWithClientInfo)(nil)
	_ SessionWithPrompts    = (*sessionTestClientWithPrompts)(nil)
)

func TestSessionWithTools_Integration(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithToolCapabilities(true))

	// Create session-specific tools
	sessionTool := ServerTool{
		Tool: mcp.NewTool("session-tool"),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("session-tool result"), nil
		},
	}

	// Create a session with tools
	session := &sessionTestClientWithTools{
		sessionID:           "session-1",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
		initialized:         true,
		sessionTools: map[string]ServerTool{
			"session-tool": sessionTool,
		},
	}

	// Register the session
	err := server.RegisterSession(context.Background(), session)
	require.NoError(t, err)

	// Test that we can access the session-specific tool
	testReq := mcp.CallToolRequest{}
	testReq.Params.Name = "session-tool"
	testReq.Params.Arguments = map[string]any{}

	// Call using session context
	sessionCtx := server.WithContext(context.Background(), session)

	// Check if the session was stored in the context correctly
	s := ClientSessionFromContext(sessionCtx)
	require.NotNil(t, s, "Session should be available from context")
	assert.Equal(t, session.SessionID(), s.SessionID(), "Session ID should match")

	// Check if the session can be cast to SessionWithTools
	swt, ok := s.(SessionWithTools)
	require.True(t, ok, "Session should implement SessionWithTools")

	// Check if the tools are accessible
	tools := swt.GetSessionTools()
	require.NotNil(t, tools, "Session tools should be available")
	require.Contains(t, tools, "session-tool", "Session should have session-tool")

	// Test session tool access with session context
	t.Run("test session tool access", func(t *testing.T) {
		// First test directly getting the tool from session tools
		tool, exists := tools["session-tool"]
		require.True(t, exists, "Session tool should exist in the map")
		require.NotNil(t, tool, "Session tool should not be nil")

		// Now test calling directly with the handler
		result, err := tool.Handler(sessionCtx, testReq)
		require.NoError(t, err, "No error calling session tool handler directly")
		require.NotNil(t, result, "Result should not be nil")
		require.Len(t, result.Content, 1, "Result should have one content item")

		textContent, ok := result.Content[0].(mcp.TextContent)
		require.True(t, ok, "Content should be TextContent")
		assert.Equal(t, "session-tool result", textContent.Text, "Result text should match")
	})
}

func TestMCPServer_ToolsWithSessionTools(t *testing.T) {
	// Basic test to verify that session-specific tools are returned correctly in a tools list
	server := NewMCPServer("test-server", "1.0.0", WithToolCapabilities(true))

	// Add global tools
	server.AddTools(
		ServerTool{Tool: mcp.NewTool("global-tool-1")},
		ServerTool{Tool: mcp.NewTool("global-tool-2")},
	)

	// Create a session with tools
	session := &sessionTestClientWithTools{
		sessionID:           "session-1",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
		initialized:         true,
		sessionTools: map[string]ServerTool{
			"session-tool-1": {Tool: mcp.NewTool("session-tool-1")},
			"global-tool-1":  {Tool: mcp.NewTool("global-tool-1", mcp.WithDescription("Overridden"))},
		},
	}

	// Register the session
	err := server.RegisterSession(context.Background(), session)
	require.NoError(t, err)

	// List tools with session context
	sessionCtx := server.WithContext(context.Background(), session)
	resp := server.HandleMessage(sessionCtx, []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "tools/list"
	}`))

	jsonResp, ok := resp.(mcp.JSONRPCResponse)
	require.True(t, ok, "Response should be a JSONRPCResponse")

	result, ok := jsonResp.Result.(mcp.ListToolsResult)
	require.True(t, ok, "Result should be a ListToolsResult")

	// Should have 3 tools - 2 global tools (one overridden) and 1 session-specific tool
	assert.Len(t, result.Tools, 3, "Should have 3 tools")

	// Find the overridden tool and verify its description
	var found bool
	for _, tool := range result.Tools {
		if tool.Name == "global-tool-1" {
			assert.Equal(t, "Overridden", tool.Description, "Global tool should be overridden")
			found = true
			break
		}
	}
	assert.True(t, found, "Should find the overridden global tool")
}

func TestMCPServer_AddSessionTools(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithToolCapabilities(true))
	ctx := context.Background()

	// Create a session
	sessionChan := make(chan mcp.JSONRPCNotification, 10)
	session := &sessionTestClientWithTools{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
		initialized:         true,
	}

	// Register the session
	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Add session-specific tools
	err = server.AddSessionTools(session.SessionID(),
		ServerTool{Tool: mcp.NewTool("session-tool")},
	)
	require.NoError(t, err)

	// Check that notification was sent
	select {
	case notification := <-sessionChan:
		assert.Equal(t, "notifications/tools/list_changed", notification.Method)
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected notification not received")
	}

	// Verify tool was added to session
	assert.Len(t, session.GetSessionTools(), 1)
	assert.Contains(t, session.GetSessionTools(), "session-tool")
}

func TestMCPServer_AddSessionPrompts(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithPromptCapabilities(true))
	ctx := context.Background()

	// Create a session
	sessionChan := make(chan mcp.JSONRPCNotification, 10)
	session := &sessionTestClientWithPrompts{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
		initialized:         true,
	}

	// Register the session
	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Add session-specific prompts
	err = server.AddSessionPrompts(session.SessionID(),
		ServerPrompt{Prompt: mcp.NewPrompt("session-prompt")},
	)
	require.NoError(t, err)

	// Check that notification was sent
	select {
	case notification := <-sessionChan:
		assert.Equal(t, "notifications/prompts/list_changed", notification.Method)
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected notification not received")
	}

	// Verify prompt was added to session
	assert.Len(t, session.GetSessionPrompts(), 1)
	assert.Contains(t, session.GetSessionPrompts(), "session-prompt")
}

func TestMCPServer_AddSessionTool(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithToolCapabilities(true))
	ctx := context.Background()

	// Create a session
	sessionChan := make(chan mcp.JSONRPCNotification, 10)
	session := &sessionTestClientWithTools{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
		initialized:         true,
	}

	// Register the session
	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Add session-specific tool using the new helper method
	err = server.AddSessionTool(
		session.SessionID(),
		mcp.NewTool("session-tool-helper"),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("helper result"), nil
		},
	)
	require.NoError(t, err)

	// Check that notification was sent
	select {
	case notification := <-sessionChan:
		assert.Equal(t, "notifications/tools/list_changed", notification.Method)
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected notification not received")
	}

	// Verify tool was added to session
	assert.Len(t, session.GetSessionTools(), 1)
	assert.Contains(t, session.GetSessionTools(), "session-tool-helper")
}

func TestMCPServer_AddSessionPrompt(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithPromptCapabilities(true))
	ctx := context.Background()

	// Create a session
	sessionChan := make(chan mcp.JSONRPCNotification, 10)
	session := &sessionTestClientWithPrompts{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
		initialized:         true,
	}

	// Register the session
	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Add session-specific tool using the new helper method
	err = server.AddSessionPrompt(
		session.SessionID(),
		mcp.NewPrompt("session-prompt-helper"),
		func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return mcp.NewGetPromptResult("helper result", []mcp.PromptMessage{
				{
					Role:    mcp.RoleUser,
					Content: mcp.TextContent{Text: "helper result"},
				},
			}), nil
		},
	)
	require.NoError(t, err)

	// Check that notification was sent
	select {
	case notification := <-sessionChan:
		assert.Equal(t, "notifications/prompts/list_changed", notification.Method)
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected notification not received")
	}

	// Verify tool was added to session
	assert.Len(t, session.GetSessionPrompts(), 1)
	assert.Contains(t, session.GetSessionPrompts(), "session-prompt-helper")
}

func TestMCPServer_AddSessionToolsUninitialized(t *testing.T) {
	// This test verifies that adding tools to an uninitialized session works correctly.
	//
	// This scenario can occur when tools are added during the session registration hook,
	// before the session is fully initialized. In this case, we should:
	// 1. Successfully add the tools to the session
	// 2. Not attempt to send a notification (since the session isn't ready)
	// 3. Have the tools available once the session is initialized
	// 4. Not trigger any error hooks when adding tools to uninitialized sessions

	// Set up error hook to track if it's called
	errorChan := make(chan error)
	hooks := &Hooks{}
	hooks.AddOnError(
		func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
			errorChan <- err
		},
	)

	server := NewMCPServer("test-server", "1.0.0",
		WithToolCapabilities(true),
		WithHooks(hooks),
	)
	ctx := context.Background()

	// Create an uninitialized session
	sessionChan := make(chan mcp.JSONRPCNotification, 1)
	session := &sessionTestClientWithTools{
		sessionID:           "uninitialized-session",
		notificationChannel: sessionChan,
		initialized:         false,
	}

	// Register the session
	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Add session-specific tools to the uninitialized session
	err = server.AddSessionTools(session.SessionID(),
		ServerTool{Tool: mcp.NewTool("uninitialized-tool")},
	)
	require.NoError(t, err)

	// Verify no errors
	select {
	case err := <-errorChan:
		t.Error("Expected no errors, but OnError called with: ", err)
	case <-time.After(25 * time.Millisecond): // no errors
	}

	// Verify no notification was sent (channel should be empty)
	select {
	case <-sessionChan:
		t.Error("Expected no notification to be sent for uninitialized session")
	default: // no notifications
	}

	// Verify tool was added to session
	assert.Len(t, session.GetSessionTools(), 1)
	assert.Contains(t, session.GetSessionTools(), "uninitialized-tool")

	// Initialize the session
	session.Initialize()

	// Now verify that subsequent tool additions will send notifications
	err = server.AddSessionTools(session.SessionID(),
		ServerTool{Tool: mcp.NewTool("initialized-tool")},
	)
	require.NoError(t, err)

	// Verify no errors
	select {
	case err := <-errorChan:
		t.Error("Expected no errors, but OnError called with:", err)
	case <-time.After(200 * time.Millisecond): // No errors
	}

	// Verify notification was sent for the initialized session
	select {
	case notification := <-sessionChan:
		assert.Equal(t, "notifications/tools/list_changed", notification.Method)
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for expected notifications/tools/list_changed notification")
	}

	// Verify both tools are available
	assert.Len(t, session.GetSessionTools(), 2)
	assert.Contains(t, session.GetSessionTools(), "uninitialized-tool")
	assert.Contains(t, session.GetSessionTools(), "initialized-tool")
}

func TestMCPServer_AddSessionPromptsUninitialized(t *testing.T) {
	// This test verifies that adding prompts to an uninitialized session works correctly.
	//
	// This scenario can occur when prompts are added during the session registration hook,
	// before the session is fully initialized. In this case, we should:
	// 1. Successfully add the prompts to the session
	// 2. Not attempt to send a notification (since the session isn't ready)
	// 3. Have the prompts available once the session is initialized
	// 4. Not trigger any error hooks when adding prompts to uninitialized sessions

	// Set up error hook to track if it's called
	errorChan := make(chan error)
	hooks := &Hooks{}
	hooks.AddOnError(
		func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
			errorChan <- err
		},
	)

	server := NewMCPServer("test-server", "1.0.0",
		WithPromptCapabilities(true),
		WithHooks(hooks),
	)
	ctx := context.Background()

	// Create an uninitialized session
	sessionChan := make(chan mcp.JSONRPCNotification, 1)
	session := &sessionTestClientWithPrompts{
		sessionID:           "uninitialized-session",
		notificationChannel: sessionChan,
		initialized:         false,
	}

	// Register the session
	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Add session-specific tools to the uninitialized session
	err = server.AddSessionPrompts(session.SessionID(),
		ServerPrompt{Prompt: mcp.NewPrompt("uninitialized-prompt")},
	)
	require.NoError(t, err)

	// Verify no errors
	select {
	case err := <-errorChan:
		t.Error("Expected no errors, but OnError called with: ", err)
	case <-time.After(25 * time.Millisecond): // no errors
	}

	// Verify no notification was sent (channel should be empty)
	select {
	case <-sessionChan:
		t.Error("Expected no notification to be sent for uninitialized session")
	default: // no notifications
	}

	// Verify prompt was added to session
	assert.Len(t, session.GetSessionPrompts(), 1)
	assert.Contains(t, session.GetSessionPrompts(), "uninitialized-prompt")

	// Initialize the session
	session.Initialize()

	// Now verify that subsequent tool additions will send notifications
	err = server.AddSessionPrompts(session.SessionID(),
		ServerPrompt{Prompt: mcp.NewPrompt("initialized-prompt")},
	)
	require.NoError(t, err)

	// Verify no errors
	select {
	case err := <-errorChan:
		t.Error("Expected no errors, but OnError called with:", err)
	case <-time.After(200 * time.Millisecond): // No errors
	}

	// Verify notification was sent for the initialized session
	select {
	case notification := <-sessionChan:
		assert.Equal(t, "notifications/prompts/list_changed", notification.Method)
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for expected notifications/prompts/list_changed notification")
	}

	// Verify both tools are available
	assert.Len(t, session.GetSessionPrompts(), 2)
	assert.Contains(t, session.GetSessionPrompts(), "uninitialized-prompt")
	assert.Contains(t, session.GetSessionPrompts(), "initialized-prompt")
}

func TestMCPServer_DeleteSessionToolsUninitialized(t *testing.T) {
	// This test verifies that deleting tools from an uninitialized session works correctly.
	//
	// This is a bit of a weird edge case but can happen if tools are added and
	// deleted during the RegisterSession hook.
	//
	// In this case, we should:
	// 1. Successfully delete the tools from the session
	// 2. Not attempt to send a notification (since the session isn't ready)
	// 3. Have the tools properly deleted once the session is initialized
	// 4. Not trigger any error hooks when deleting tools from uninitialized sessions

	// Set up error hook to track if it's called
	errorChan := make(chan error)
	hooks := &Hooks{}
	hooks.AddOnError(
		func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
			errorChan <- err
		},
	)

	server := NewMCPServer("test-server", "1.0.0",
		WithToolCapabilities(true),
		WithHooks(hooks),
	)
	ctx := context.Background()

	// Create an uninitialized session with some tools
	sessionChan := make(chan mcp.JSONRPCNotification, 1)
	session := &sessionTestClientWithTools{
		sessionID:           "uninitialized-session",
		notificationChannel: sessionChan,
		initialized:         false,
		sessionTools: map[string]ServerTool{
			"tool-to-delete": {Tool: mcp.NewTool("tool-to-delete")},
			"tool-to-keep":   {Tool: mcp.NewTool("tool-to-keep")},
		},
	}

	// Register the session
	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Delete a tool from the uninitialized session
	err = server.DeleteSessionTools(session.SessionID(), "tool-to-delete")
	require.NoError(t, err)

	select {
	case err := <-errorChan:
		t.Errorf("Expected error hooks not to be called, got error: %v", err)
	case <-time.After(25 * time.Millisecond): // No errors
	}

	// Verify no notification was sent (channel should be empty)
	select {
	case <-sessionChan:
		t.Error("Expected no notification to be sent for uninitialized session")
	default:
		// This is the expected case - no notification should be sent
	}

	// Verify tool was deleted from session
	assert.Len(t, session.GetSessionTools(), 1)
	assert.NotContains(t, session.GetSessionTools(), "tool-to-delete")
	assert.Contains(t, session.GetSessionTools(), "tool-to-keep")

	// Initialize the session
	session.Initialize()

	// Now verify that subsequent tool deletions will send notifications
	err = server.DeleteSessionTools(session.SessionID(), "tool-to-keep")
	require.NoError(t, err)

	select {
	case err := <-errorChan:
		t.Errorf("Expected error hooks not to be called, got error: %v", err)
	case <-time.After(200 * time.Millisecond): // No errors
	}

	// Verify notification was sent for the initialized session
	select {
	case notification := <-sessionChan:
		assert.Equal(t, "notifications/tools/list_changed", notification.Method)
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected notification not received for initialized session")
	}

	// Verify all tools are deleted
	assert.Len(t, session.GetSessionTools(), 0)
}

func TestMCPServer_CallSessionTool(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithToolCapabilities(true))

	// Add global tool
	server.AddTool(mcp.NewTool("test_tool"), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("global result"), nil
	})

	// Create a session
	sessionChan := make(chan mcp.JSONRPCNotification, 10)
	session := &sessionTestClientWithTools{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
		initialized:         true,
	}

	// Register the session
	err := server.RegisterSession(context.Background(), session)
	require.NoError(t, err)

	// Add session-specific tool with the same name to override the global tool
	err = server.AddSessionTool(
		session.SessionID(),
		mcp.NewTool("test_tool"),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("session result"), nil
		},
	)
	require.NoError(t, err)

	// Call the tool using session context
	sessionCtx := server.WithContext(context.Background(), session)
	toolRequest := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "test_tool",
		},
	}
	requestBytes, err := json.Marshal(toolRequest)
	if err != nil {
		t.Fatalf("Failed to marshal tool request: %v", err)
	}

	response := server.HandleMessage(sessionCtx, requestBytes)
	resp, ok := response.(mcp.JSONRPCResponse)
	assert.True(t, ok)

	callToolResult, ok := resp.Result.(mcp.CallToolResult)
	assert.True(t, ok)

	// Since we specify a tool with the same name for current session, the expected text should be "session result"
	if text := callToolResult.Content[0].(mcp.TextContent).Text; text != "session result" {
		t.Errorf("Expected result 'session result', got %q", text)
	}
}

func TestMCPServer_GetSessionPrompt(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithPromptCapabilities(true))

	// Add global prompt
	server.AddPrompt(mcp.NewPrompt("test_prompt"), func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return mcp.NewGetPromptResult("global result", []mcp.PromptMessage{
			{
				Role:    mcp.RoleUser,
				Content: mcp.TextContent{Text: "global result"},
			},
		}), nil
	})

	// Create a session
	sessionChan := make(chan mcp.JSONRPCNotification, 10)
	session := &sessionTestClientWithPrompts{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
		initialized:         true,
	}

	// Register the session
	err := server.RegisterSession(context.Background(), session)
	require.NoError(t, err)

	// Add session-specific prompt with the same name to override the global prompt
	err = server.AddSessionPrompt(
		session.SessionID(),
		mcp.NewPrompt("test_prompt"),
		func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return mcp.NewGetPromptResult("session result", []mcp.PromptMessage{
				{
					Role:    mcp.RoleUser,
					Content: mcp.TextContent{Text: "session result"},
				},
			}), nil
		},
	)
	require.NoError(t, err)

	// Get the prompt using session context
	sessionCtx := server.WithContext(context.Background(), session)
	toolRequest := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "prompts/get",
		"params": map[string]any{
			"name": "test_prompt",
		},
	}
	requestBytes, err := json.Marshal(toolRequest)
	if err != nil {
		t.Fatalf("Failed to marshal prompt request: %v", err)
	}

	response := server.HandleMessage(sessionCtx, requestBytes)
	resp, ok := response.(mcp.JSONRPCResponse)
	assert.True(t, ok)

	getPromptResult, ok := resp.Result.(mcp.GetPromptResult)
	assert.True(t, ok)

	// Since we specify a prompt with the same name for current session, the expected text should be "session result"
	if textContent, ok := getPromptResult.Messages[0].Content.(mcp.TextContent); ok {
		if textContent.Text != "session result" {
			t.Errorf("Expected result 'session result', got %q", textContent.Text)
		}
	} else {
		t.Error("Expected TextContent")
	}
}

func TestMCPServer_DeleteSessionTools(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithToolCapabilities(true))
	ctx := context.Background()

	// Create a session with tools
	sessionChan := make(chan mcp.JSONRPCNotification, 10)
	session := &sessionTestClientWithTools{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
		initialized:         true,
		sessionTools: map[string]ServerTool{
			"session-tool-1": {
				Tool: mcp.NewTool("session-tool-1"),
			},
			"session-tool-2": {
				Tool: mcp.NewTool("session-tool-2"),
			},
		},
	}

	// Register the session
	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Delete one of the session tools
	err = server.DeleteSessionTools(session.SessionID(), "session-tool-1")
	require.NoError(t, err)

	// Check that notification was sent
	select {
	case notification := <-sessionChan:
		assert.Equal(t, "notifications/tools/list_changed", notification.Method)
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected notification not received")
	}

	// Verify tool was removed from session
	assert.Len(t, session.GetSessionTools(), 1)
	assert.NotContains(t, session.GetSessionTools(), "session-tool-1")
	assert.Contains(t, session.GetSessionTools(), "session-tool-2")
}

func TestMCPServer_ToolFiltering(t *testing.T) {
	// Create a filter that filters tools by prefix
	filterByPrefix := func(prefix string) ToolFilterFunc {
		return func(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
			var filtered []mcp.Tool
			for _, tool := range tools {
				if len(tool.Name) >= len(prefix) && tool.Name[:len(prefix)] == prefix {
					filtered = append(filtered, tool)
				}
			}
			return filtered
		}
	}

	// Create a server with a tool filter
	server := NewMCPServer("test-server", "1.0.0",
		WithToolCapabilities(true),
		WithToolFilter(filterByPrefix("allow-")),
	)

	// Add tools with different prefixes
	server.AddTools(
		ServerTool{Tool: mcp.NewTool("allow-tool-1")},
		ServerTool{Tool: mcp.NewTool("allow-tool-2")},
		ServerTool{Tool: mcp.NewTool("deny-tool-1")},
		ServerTool{Tool: mcp.NewTool("deny-tool-2")},
	)

	// Create a session with tools
	session := &sessionTestClientWithTools{
		sessionID:           "session-1",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
		initialized:         true,
		sessionTools: map[string]ServerTool{
			"allow-session-tool": {
				Tool: mcp.NewTool("allow-session-tool"),
			},
			"deny-session-tool": {
				Tool: mcp.NewTool("deny-session-tool"),
			},
		},
	}

	// Register the session
	err := server.RegisterSession(context.Background(), session)
	require.NoError(t, err)

	// List tools with session context
	sessionCtx := server.WithContext(context.Background(), session)
	response := server.HandleMessage(sessionCtx, []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "tools/list"
	}`))
	resp, ok := response.(mcp.JSONRPCResponse)
	require.True(t, ok)

	result, ok := resp.Result.(mcp.ListToolsResult)
	require.True(t, ok)

	// Should only include tools with the "allow-" prefix
	assert.Len(t, result.Tools, 3)

	// Verify all tools start with "allow-"
	for _, tool := range result.Tools {
		assert.True(t, len(tool.Name) >= 6 && tool.Name[:6] == "allow-",
			"Tool should start with 'allow-', got: %s", tool.Name)
	}
}

func TestMCPServer_SendNotificationToSpecificClient(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0")

	session1Chan := make(chan mcp.JSONRPCNotification, 10)
	session1 := &sessionTestClient{
		sessionID:           "session-1",
		notificationChannel: session1Chan,
	}
	session1.Initialize()

	session2Chan := make(chan mcp.JSONRPCNotification, 10)
	session2 := &sessionTestClient{
		sessionID:           "session-2",
		notificationChannel: session2Chan,
	}
	session2.Initialize()

	session3 := &sessionTestClient{
		sessionID:           "session-3",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
		initialized:         false, // Not initialized - deliberately not calling Initialize()
	}

	// Register sessions
	err := server.RegisterSession(context.Background(), session1)
	require.NoError(t, err)
	err = server.RegisterSession(context.Background(), session2)
	require.NoError(t, err)
	err = server.RegisterSession(context.Background(), session3)
	require.NoError(t, err)

	// Send notification to session 1
	err = server.SendNotificationToSpecificClient(session1.SessionID(), "test-method", map[string]any{
		"data": "test-data",
	})
	require.NoError(t, err)

	// Check that only session 1 received the notification
	select {
	case notification := <-session1Chan:
		assert.Equal(t, "test-method", notification.Method)
		assert.Equal(t, "test-data", notification.Params.AdditionalFields["data"])
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected notification not received by session 1")
	}

	// Verify session 2 did not receive notification
	select {
	case notification := <-session2Chan:
		t.Errorf("Unexpected notification received by session 2: %v", notification)
	case <-time.After(100 * time.Millisecond):
		// Expected, no notification for session 2
	}

	// Test sending to non-existent session
	err = server.SendNotificationToSpecificClient("non-existent", "test-method", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// Test sending to uninitialized session
	err = server.SendNotificationToSpecificClient(session3.SessionID(), "test-method", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not properly initialized")
}

func TestMCPServer_NotificationChannelBlocked(t *testing.T) {
	// Set up a hooks object to capture error notifications
	var mu sync.Mutex
	errorCaptured := false
	errorSessionID := ""
	errorMethod := ""

	hooks := &Hooks{}
	hooks.AddOnError(func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
		mu.Lock()
		defer mu.Unlock()

		errorCaptured = true
		// Extract session ID and method from the error message metadata
		if msgMap, ok := message.(map[string]any); ok {
			if sid, ok := msgMap["sessionID"].(string); ok {
				errorSessionID = sid
			}
			if m, ok := msgMap["method"].(string); ok {
				errorMethod = m
			}
		}
		// Verify the error is a notification channel blocked error
		assert.True(t, errors.Is(err, ErrNotificationChannelBlocked))
	})

	// Create a server with hooks
	server := NewMCPServer("test-server", "1.0.0", WithHooks(hooks))

	// Create a session with a very small buffer that will get blocked
	smallBufferChan := make(chan mcp.JSONRPCNotification, 1)
	session := &sessionTestClient{
		sessionID:           "blocked-session",
		notificationChannel: smallBufferChan,
	}
	session.Initialize()

	// Register the session
	err := server.RegisterSession(context.Background(), session)
	require.NoError(t, err)

	// Fill the buffer first to ensure it gets blocked
	err = server.SendNotificationToSpecificClient(session.SessionID(), "first-message", nil)
	require.NoError(t, err)

	// This will cause the buffer to block
	err = server.SendNotificationToSpecificClient(session.SessionID(), "blocked-message", nil)
	assert.Error(t, err)
	assert.Equal(t, ErrNotificationChannelBlocked, err)

	// Wait a bit for the goroutine to execute
	time.Sleep(10 * time.Millisecond)

	// Verify the error was logged via hooks
	mu.Lock()
	localErrorCaptured := errorCaptured
	localErrorSessionID := errorSessionID
	localErrorMethod := errorMethod
	mu.Unlock()

	assert.True(t, localErrorCaptured, "Error hook should have been called")
	assert.Equal(t, "blocked-session", localErrorSessionID, "Session ID should be captured in the error hook")
	assert.Equal(t, "blocked-message", localErrorMethod, "Method should be captured in the error hook")

	// Also test SendNotificationToAllClients with a blocked channel
	// Reset the captured data
	mu.Lock()
	errorCaptured = false
	errorSessionID = ""
	errorMethod = ""
	mu.Unlock()

	// Send to all clients (which includes our blocked one)
	server.SendNotificationToAllClients("broadcast-message", nil)

	// Wait a bit for the goroutine to execute
	time.Sleep(10 * time.Millisecond)

	// Verify the error was logged via hooks
	mu.Lock()
	localErrorCaptured = errorCaptured
	localErrorSessionID = errorSessionID
	localErrorMethod = errorMethod
	mu.Unlock()

	assert.True(t, localErrorCaptured, "Error hook should have been called for broadcast")
	assert.Equal(t, "blocked-session", localErrorSessionID, "Session ID should be captured in the error hook")
	assert.Equal(t, "broadcast-message", localErrorMethod, "Method should be captured in the error hook")
}

func TestMCPServer_SessionToolCapabilitiesBehavior(t *testing.T) {
	tests := []struct {
		name           string
		serverOptions  []ServerOption
		validateServer func(t *testing.T, s *MCPServer, session *sessionTestClientWithTools)
	}{
		{
			name:          "no tool capabilities provided",
			serverOptions: []ServerOption{
				// No WithToolCapabilities
			},
			validateServer: func(t *testing.T, s *MCPServer, session *sessionTestClientWithTools) {
				s.capabilitiesMu.RLock()
				defer s.capabilitiesMu.RUnlock()

				require.NotNil(t, s.capabilities.tools, "tools capability should be initialized")
				assert.True(t, s.capabilities.tools.listChanged, "listChanged should be true when no capabilities were provided")
			},
		},
		{
			name: "tools.listChanged set to false",
			serverOptions: []ServerOption{
				WithToolCapabilities(false),
			},
			validateServer: func(t *testing.T, s *MCPServer, session *sessionTestClientWithTools) {
				s.capabilitiesMu.RLock()
				defer s.capabilitiesMu.RUnlock()

				require.NotNil(t, s.capabilities.tools, "tools capability should be initialized")
				assert.False(t, s.capabilities.tools.listChanged, "listChanged should remain false when explicitly set to false")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewMCPServer("test-server", "1.0.0", tt.serverOptions...)

			// Create and register a session
			session := &sessionTestClientWithTools{
				sessionID:           "test-session",
				notificationChannel: make(chan mcp.JSONRPCNotification, 10),
				initialized:         true,
			}
			err := server.RegisterSession(context.Background(), session)
			require.NoError(t, err)

			// Add a session tool and verify listChanged remains false
			err = server.AddSessionTool(session.SessionID(), mcp.NewTool("test-tool"), nil)
			require.NoError(t, err)

			tt.validateServer(t, server, session)
		})
	}
}

func TestMCPServer_ToolNotificationsDisabled(t *testing.T) {
	// This test verifies that when tool capabilities are disabled, we still
	// add/delete tools correctly but don't send notifications about it.
	//
	// This is important because:
	// 1. Tools should still work even if notifications are disabled
	// 2. We shouldn't waste resources sending notifications that won't be used
	// 3. The client might not be ready to handle tool notifications yet

	// Create a server WITHOUT tool capabilities
	server := NewMCPServer("test-server", "1.0.0", WithToolCapabilities(false))
	ctx := context.Background()

	// Create an initialized session
	sessionChan := make(chan mcp.JSONRPCNotification, 1)
	session := &sessionTestClientWithTools{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
		initialized:         true,
	}

	// Register the session
	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Add a tool
	err = server.AddSessionTools(session.SessionID(),
		ServerTool{Tool: mcp.NewTool("test-tool")},
	)
	require.NoError(t, err)

	// Verify no notification was sent
	select {
	case <-sessionChan:
		t.Error("Expected no notification to be sent when capabilities.tools.listChanged is false")
	default:
		// This is the expected case - no notification should be sent
	}

	// Verify tool was added to session
	assert.Len(t, session.GetSessionTools(), 1)
	assert.Contains(t, session.GetSessionTools(), "test-tool")

	// Delete the tool
	err = server.DeleteSessionTools(session.SessionID(), "test-tool")
	require.NoError(t, err)

	// Verify no notification was sent
	select {
	case <-sessionChan:
		t.Error("Expected no notification to be sent when capabilities.tools.listChanged is false")
	default:
		// This is the expected case - no notification should be sent
	}

	// Verify tool was deleted from session
	assert.Len(t, session.GetSessionTools(), 0)
}

func TestMCPServer_SetLevelNotEnabled(t *testing.T) {
	// Create server without logging capability
	server := NewMCPServer("test-server", "1.0.0")

	// Create and initialize a session
	sessionChan := make(chan mcp.JSONRPCNotification, 10)
	session := &sessionTestClientWithLogging{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
	}
	session.Initialize()

	// Register the session
	err := server.RegisterSession(context.Background(), session)
	require.NoError(t, err)

	// Try to set logging level when capability is disabled
	sessionCtx := server.WithContext(context.Background(), session)
	setRequest := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "logging/setLevel",
		"params": map[string]any{
			"level": mcp.LoggingLevelCritical,
		},
	}
	requestBytes, err := json.Marshal(setRequest)
	require.NoError(t, err)

	response := server.HandleMessage(sessionCtx, requestBytes)
	errorResponse, ok := response.(mcp.JSONRPCError)
	assert.True(t, ok)

	// Verify we get a METHOD_NOT_FOUND error
	assert.NotNil(t, errorResponse.Error)
	assert.Equal(t, mcp.METHOD_NOT_FOUND, errorResponse.Error.Code)
}

func TestMCPServer_SetLevel(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithLogging())

	// Create and initicalize a session
	sessionChan := make(chan mcp.JSONRPCNotification, 10)
	session := &sessionTestClientWithLogging{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
	}
	session.Initialize()

	// Check default logging level
	if session.GetLogLevel() != mcp.LoggingLevelError {
		t.Errorf("Expected error level, got %v", session.GetLogLevel())
	}

	// Register the session
	err := server.RegisterSession(context.Background(), session)
	require.NoError(t, err)

	// Set Logging level to critical
	sessionCtx := server.WithContext(context.Background(), session)
	setRequest := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "logging/setLevel",
		"params": map[string]any{
			"level": mcp.LoggingLevelCritical,
		},
	}
	requestBytes, err := json.Marshal(setRequest)
	if err != nil {
		t.Fatalf("Failed to marshal tool request: %v", err)
	}

	response := server.HandleMessage(sessionCtx, requestBytes)
	resp, ok := response.(mcp.JSONRPCResponse)
	assert.True(t, ok)

	_, ok = resp.Result.(mcp.EmptyResult)
	assert.True(t, ok)

	// Check logging level
	if session.GetLogLevel() != mcp.LoggingLevelCritical {
		t.Errorf("Expected critical level, got %v", session.GetLogLevel())
	}
}

func TestSessionWithClientInfo_Integration(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0")

	session := &sessionTestClientWithClientInfo{
		sessionID:           "session-1",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
		initialized:         false,
	}

	err := server.RegisterSession(context.Background(), session)
	require.NoError(t, err)

	clientInfo := mcp.Implementation{
		Name:    "test-client",
		Version: "1.0.0",
	}

	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ClientInfo = clientInfo
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.Capabilities = mcp.ClientCapabilities{}

	sessionCtx := server.WithContext(context.Background(), session)

	// Retrieve the session from context
	retrievedSession := ClientSessionFromContext(sessionCtx)
	require.NotNil(t, retrievedSession, "Session should be available from context")
	assert.Equal(t, session.SessionID(), retrievedSession.SessionID(), "Session ID should match")

	result, reqErr := server.handleInitialize(sessionCtx, 1, initRequest)
	require.Nil(t, reqErr)
	require.NotNil(t, result)

	// Check if the session can be cast to SessionWithClientInfo
	sessionWithClientInfo, ok := retrievedSession.(SessionWithClientInfo)
	require.True(t, ok, "Session should implement SessionWithClientInfo")

	assert.True(t, sessionWithClientInfo.Initialized(), "Session should be initialized")

	storedClientInfo := sessionWithClientInfo.GetClientInfo()

	assert.Equal(t, clientInfo.Name, storedClientInfo.Name, "Client name should match")
	assert.Equal(t, clientInfo.Version, storedClientInfo.Version, "Client version should match")
}
