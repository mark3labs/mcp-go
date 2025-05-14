package server

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// Verify that both implementations satisfy their respective interfaces
var _ ClientSession = &sessionTestClient{}
var _ SessionWithTools = &sessionTestClientWithTools{}

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
