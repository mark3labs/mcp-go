package server

import (
	"context"
	"maps"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestSessionHookGoroutine_PanicsWithoutRecovery proves that a panicking
// OnError hook in a session notification goroutine crashes the process.
// This test should be run with `go test -run TestSessionHookGoroutine_PanicsWithoutRecovery`
// and will CRASH (exit non-zero) if the bug is present.
//
// After the fix, this test passes because the goroutine recovers the panic.
func TestSessionHookGoroutine_PanicsWithoutRecovery(t *testing.T) {
	var panicRecovered atomic.Bool

	// Create server with a hook that panics
	hooks := &Hooks{}
	hooks.AddOnError(func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
		panic("deliberate panic in OnError hook")
	})

	server := NewMCPServer("test", "1.0.0",
		WithToolCapabilities(true),
		WithHooks(hooks),
	)

	// Create a mock session with a full notification channel (to trigger the error path)
	session := &mockSessionForPanic{
		sessionID:    "test-session-panic",
		initialized:  true,
		notifyChan:   make(chan mcp.JSONRPCNotification), // unbuffered = immediately blocks
		doneOnce:     sync.Once{},
	}

	// Register the session
	server.sessions.Store(session.SessionID(), session)

	// Send notification to all clients. The channel is unbuffered and nobody is
	// reading from it, so the send falls into the default case which spawns
	// a goroutine calling hooks.onError. That goroutine panics.

	// This triggers the goroutine with the panicking hook
	server.SendNotificationToAllClients("notifications/test", nil)

	// Give the goroutine time to execute and (hopefully) recover
	time.Sleep(100 * time.Millisecond)

	// If we reach this point, the panic was recovered (or the test would have crashed)
	panicRecovered.Store(true)
}

// TestSessionHookGoroutine_PanicRecoveryLogged verifies that after the fix,
// a panicking hook in a session notification goroutine:
// 1. Does not crash the process
// 2. Allows subsequent notifications to continue working
func TestSessionHookGoroutine_PanicRecoveryLogged(t *testing.T) {
	var panicCount atomic.Int32

	// First hook panics, second hook increments counter
	hooks := &Hooks{}
	hooks.AddOnError(func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
		panicCount.Add(1)
		panic("deliberate panic in first OnError hook")
	})

	server := NewMCPServer("test", "1.0.0",
		WithToolCapabilities(true),
		WithHooks(hooks),
	)

	// Create mock session with full channel
	session := &mockSessionForPanic{
		sessionID:   "test-session-recovery",
		initialized: true,
		notifyChan:  make(chan mcp.JSONRPCNotification), // unbuffered blocks immediately
		doneOnce:    sync.Once{},
	}
	server.sessions.Store(session.SessionID(), session)

	// Send 3 notifications - each should trigger the hook goroutine
	for range 3 {
		server.SendNotificationToAllClients("notifications/test", nil)
	}

	// Wait for goroutines to execute
	time.Sleep(200 * time.Millisecond)

	// All 3 hook goroutines should have fired (and panicked, then recovered)
	if panicCount.Load() != 3 {
		t.Errorf("expected 3 hook invocations (panics), got %d", panicCount.Load())
	}
}

// TestSendNotificationToSpecificClient_HookPanicRecovery verifies that
// sendNotificationToSpecificClient's hook goroutine also recovers from panics.
func TestSendNotificationToSpecificClient_HookPanicRecovery(t *testing.T) {
	var panicCount atomic.Int32

	hooks := &Hooks{}
	hooks.AddOnError(func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
		panicCount.Add(1)
		panic("panic in specific client hook")
	})

	server := NewMCPServer("test", "1.0.0",
		WithToolCapabilities(true),
		WithHooks(hooks),
	)

	// Create mock session with full channel
	session := &mockSessionForPanic{
		sessionID:   "specific-client-test",
		initialized: true,
		notifyChan:  make(chan mcp.JSONRPCNotification), // unbuffered blocks immediately
		doneOnce:    sync.Once{},
	}

	// Call SendNotificationToSpecificClient with session ID
	server.sessions.Store(session.SessionID(), session)
	_ = server.SendNotificationToSpecificClient(session.SessionID(), "notifications/tools/list_changed", nil)

	// Wait for goroutine
	time.Sleep(100 * time.Millisecond)

	if panicCount.Load() != 1 {
		t.Errorf("expected 1 hook invocation (panic), got %d", panicCount.Load())
	}
}

// TestAddSessionTools_HookPanicRecovery verifies that the hook goroutine
// spawned during AddSessionTools notification failure also recovers.
func TestAddSessionTools_HookPanicRecovery(t *testing.T) {
	var panicCount atomic.Int32

	hooks := &Hooks{}
	hooks.AddOnError(func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
		panicCount.Add(1)
		panic("panic in AddSessionTools hook")
	})

	server := NewMCPServer("test", "1.0.0",
		WithToolCapabilities(true),
		WithHooks(hooks),
	)

	// Create a session that supports tools but has a full notification channel
	session := &mockSessionForPanicWithTools{
		mockSessionForPanic: mockSessionForPanic{
			sessionID:   "add-tools-test",
			initialized: true,
			notifyChan:  make(chan mcp.JSONRPCNotification), // unbuffered blocks
			doneOnce:    sync.Once{},
		},
		tools: make(map[string]ServerTool),
	}
	server.sessions.Store(session.SessionID(), session)

	// Add a tool to the session - notification will fail (channel full),
	// triggering the error hook goroutine which panics
	err := server.AddSessionTools(session.SessionID(), ServerTool{
		Tool: mcp.Tool{Name: "test-tool", Description: "test"},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("ok"), nil
		},
	})

	// The tool addition itself should succeed even if the notification hook panics
	if err != nil {
		t.Fatalf("AddSessionTools should not fail: %v", err)
	}

	// Wait for goroutines
	time.Sleep(100 * time.Millisecond)

	// The hook fires at least once (may fire twice: once from
	// SendNotificationToSpecificClient's blocked channel, once from the
	// AddSessionTools notification failure path)
	if panicCount.Load() < 1 {
		t.Errorf("expected at least 1 hook invocation (panic), got %d", panicCount.Load())
	}
}

// --- Mock session types ---

type mockSessionForPanic struct {
	sessionID   string
	initialized bool
	notifyChan  chan mcp.JSONRPCNotification
	doneOnce    sync.Once
}

func (m *mockSessionForPanic) SessionID() string                              { return m.sessionID }
func (m *mockSessionForPanic) Initialized() bool                              { return m.initialized }
func (m *mockSessionForPanic) NotificationChannel() chan<- mcp.JSONRPCNotification { return m.notifyChan }
func (m *mockSessionForPanic) Initialize()                                    {}
func (m *mockSessionForPanic) GetLoggingLevel() mcp.LoggingLevel              { return mcp.LoggingLevelError }
func (m *mockSessionForPanic) SetLoggingLevel(level mcp.LoggingLevel)         {}
func (m *mockSessionForPanic) GetClientInfo() *mcp.Implementation             { return nil }
func (m *mockSessionForPanic) SetClientInfo(info mcp.Implementation)          {}
func (m *mockSessionForPanic) GetClientCapabilities() *mcp.ClientCapabilities { return nil }
func (m *mockSessionForPanic) SetClientCapabilities(caps mcp.ClientCapabilities) {}

type mockSessionForPanicWithTools struct {
	mockSessionForPanic
	tools map[string]ServerTool
	mu    sync.RWMutex
}

func (m *mockSessionForPanicWithTools) GetSessionTools() map[string]ServerTool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := make(map[string]ServerTool, len(m.tools))
	maps.Copy(cp, m.tools)
	return cp
}

func (m *mockSessionForPanicWithTools) SetSessionTools(tools map[string]ServerTool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tools = tools
}
