package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// mockBroker simulates a message broker for testing multi-instance behavior
type mockBroker struct {
	mu            sync.RWMutex
	events        map[string][]string
	notifications map[string][]mcp.JSONRPCNotification
}

func newMockBroker() *mockBroker {
	return &mockBroker{
		events:        make(map[string][]string),
		notifications: make(map[string][]mcp.JSONRPCNotification),
	}
}

func (m *mockBroker) publishEvent(sessionID, event string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.events[sessionID]; !exists {
		m.events[sessionID] = make([]string, 0)
	}
	m.events[sessionID] = append(m.events[sessionID], event)
}

func (m *mockBroker) publishNotification(sessionID string, notification mcp.JSONRPCNotification) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.notifications[sessionID]; !exists {
		m.notifications[sessionID] = make([]mcp.JSONRPCNotification, 0)
	}
	m.notifications[sessionID] = append(m.notifications[sessionID], notification)
}

func (m *mockBroker) getEvents(sessionID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if events, exists := m.events[sessionID]; exists {
		return events
	}
	return nil
}

func (m *mockBroker) getNotifications(sessionID string) []mcp.JSONRPCNotification {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if notifications, exists := m.notifications[sessionID]; exists {
		return notifications
	}
	return nil
}

// createMockEventQueueBuilder creates a channel that publishes to the mock broker
func createMockEventQueueBuilder(broker *mockBroker) EventQueueBuilder {
	return func(sessionID string) chan string {
		ch := make(chan string, 100)

		// Start a goroutine to forward events to the broker
		go func() {
			for event := range ch {
				fmt.Println("Publishing event to broker:", event)
				broker.publishEvent(sessionID, event)

				// Also check for events from other instances
				//events := broker.getEvents(sessionID)
				//for _, e := range events {
				//	if e != event { // Don't send back the same event
				//		select {
				//		case ch <- e:
				//			// Event sent successfully
				//		default:
				//			// Channel full, would log in real implementation
				//		}
				//	}
				//}
			}
		}()

		return ch
	}
}

// createMockNotificationChannelBuilder creates a channel that publishes to the mock broker
func createMockNotificationChannelBuilder(broker *mockBroker) NotificationChannelBuilder {
	return func(sessionID string) chan mcp.JSONRPCNotification {
		ch := make(chan mcp.JSONRPCNotification, 100)

		// Start a goroutine to forward notifications to the broker
		go func() {
			for notification := range ch {
				broker.publishNotification(sessionID, notification)

				// Also check for notifications from other instances
				notifications := broker.getNotifications(sessionID)
				for _, n := range notifications {
					// Don't compare the full notification, just check if it's the same type/method
					if n.Method != notification.Method {
						select {
						case ch <- n:
							// Notification sent successfully
						default:
							// Channel full, would log in real implementation
						}
					}
				}
			}
		}()

		return ch
	}
}

func TestChannelBuilders(t *testing.T) {
	t.Run("Can instantiate with channel builders", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		broker := newMockBroker()

		sseServer := NewSSEServer(mcpServer,
			WithEventQueueBuilder(createMockEventQueueBuilder(broker)),
			WithNotificationChannelBuilder(createMockNotificationChannelBuilder(broker)),
		)

		if sseServer.eventQueueBuilder == nil {
			t.Error("eventQueueBuilder should not be nil")
		}
		if sseServer.notificationChannelBuilder == nil {
			t.Error("notificationChannelBuilder should not be nil")
		}
	})

	t.Run("Custom event channels work correctly", func(t *testing.T) {
		mcpServer := NewMCPServer("test", "1.0.0")
		broker := newMockBroker()

		// Create server with custom channel builders
		testServer := httptest.NewServer(NewSSEServer(mcpServer,
			WithEventQueueBuilder(createMockEventQueueBuilder(broker)),
		))
		defer testServer.Close()

		// Connect to SSE endpoint
		resp, err := http.Get(fmt.Sprintf("%s/sse", testServer.URL))
		if err != nil {
			t.Fatalf("Failed to connect to SSE endpoint: %v", err)
		}
		defer resp.Body.Close()

		// Read enough data to get the session ID
		buffer := make([]byte, 1024)
		n, err := resp.Body.Read(buffer)
		if err != nil {
			t.Fatalf("Failed to read from SSE stream: %v", err)
		}

		// Extract session ID from response
		data := string(buffer[:n])
		lines := strings.Split(data, "\n")
		var messageURL string

		for i, line := range lines {
			line = strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(line, "data: ") && i > 0 && strings.HasPrefix(lines[i-1], "event: endpoint") {
				messageURL = strings.TrimPrefix(line, "data: ")
				t.Log("Message URL: ", "'"+messageURL+"'")
				break
			}
		}

		if messageURL == "" {
			t.Fatalf("Failed to find message URL in response: %s", data)
		}

		// Extract session ID
		sessionIDPart := strings.Split(messageURL, "sessionId=")
		if len(sessionIDPart) != 2 {
			t.Fatalf("Invalid message URL format: %s", messageURL)
		}
		sessionID := strings.TrimSpace(sessionIDPart[1])

		// Verify the event appears in the SSE stream
		buffer = make([]byte, 1024)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		done := make(chan struct{})
		var eventReceived bool

		go func() {
			defer close(done)
			n, err := resp.Body.Read(buffer)
			if err != nil {
				return
			}

			data := string(buffer[:n])
			t.Log("Received data: ", "'"+data+"'")
			if strings.Contains(data, "\"method\":\"test\"") {
				eventReceived = true
			}
		}()

		// Create a custom event and send it via the broker
		customEvent := fmt.Sprintf("event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"test\",\"params\":{\"test\":true}}\n\n")
		broker.publishEvent(sessionID, customEvent)

		select {
		case <-done:
			if !eventReceived {
				t.Error("Expected custom event not received")
			}
		case <-ctx.Done():
			t.Error("Timeout waiting for custom event")
		}
	})

	// t.Run("Notifications propagate through custom channels", func(t *testing.T) {
	// 	mcpServer := NewMCPServer("test", "1.0.0",
	// 		WithToolCapabilities(true),
	// 	)
	// 	broker := newMockBroker()
	//
	// 	// Create server with custom channel builders
	// 	testServer := NewTestServer(mcpServer,
	// 		WithEventQueueBuilder(createMockEventQueueBuilder(broker)),
	// 		WithNotificationChannelBuilder(createMockNotificationChannelBuilder(broker)),
	// 	)
	// 	defer testServer.Close()
	//
	// 	// Connect to SSE endpoint
	// 	resp, err := http.Get(fmt.Sprintf("%s/sse", testServer.URL))
	// 	if err != nil {
	// 		t.Fatalf("Failed to connect to SSE endpoint: %v", err)
	// 	}
	// 	defer resp.Body.Close()
	//
	// 	// Read enough data to get the session ID and message URL
	// 	buffer := make([]byte, 1024)
	// 	n, err := resp.Body.Read(buffer)
	// 	if err != nil {
	// 		t.Fatalf("Failed to read from SSE stream: %v", err)
	// 	}
	//
	// 	// Extract message URL from response
	// 	data := string(buffer[:n])
	// 	lines := strings.Split(data, "\n")
	// 	var messageURL string
	//
	// 	for i, line := range lines {
	// 		if strings.HasPrefix(line, "data: ") && i > 0 && strings.HasPrefix(lines[i-1], "event: endpoint") {
	// 			messageURL = strings.TrimPrefix(line, "data: ")
	// 			messageURL = strings.ReplaceAll(messageURL, "\r", "")
	// 			t.Log("Message URL: ", "'"+messageURL+"'")
	// 			break
	// 		}
	// 	}
	//
	// 	// Send initialize request to activate notifications
	// 	initReq := map[string]interface{}{
	// 		"jsonrpc": "2.0",
	// 		"id":      1,
	// 		"method":  "initialize",
	// 		"params": map[string]interface{}{
	// 			"protocolVersion": "2024-11-05",
	// 			"clientInfo": map[string]interface{}{
	// 				"name":    "test-client",
	// 				"version": "1.0.0",
	// 			},
	// 		},
	// 	}
	//
	// 	reqBody, _ := json.Marshal(initReq)
	// 	initResp, err := http.Post(
	// 		messageURL,
	// 		"application/json",
	// 		bytes.NewBuffer(reqBody),
	// 	)
	// 	if err != nil {
	// 		t.Fatalf("Failed to send initialize request: %v", err)
	// 	}
	// 	initResp.Body.Close()
	//
	// 	// Read the initialize response from SSE stream
	// 	buffer = make([]byte, 1024)
	// 	_, err = resp.Body.Read(buffer)
	// 	if err != nil {
	// 		t.Fatalf("Failed to read initialize response: %v", err)
	// 	}
	//
	// 	// Send a notification through MCP server
	// 	mcpServer.SendNotificationToAllClients("test/notification", map[string]any{
	// 		"message": "test message",
	// 	})
	//
	// 	// Wait for notification in SSE stream
	// 	buffer = make([]byte, 1024)
	// 	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	// 	defer cancel()
	//
	// 	done := make(chan struct{})
	// 	var notificationReceived bool
	//
	// 	go func() {
	// 		defer close(done)
	// 		n, err := resp.Body.Read(buffer)
	// 		if err != nil {
	// 			return
	// 		}
	//
	// 		data := string(buffer[:n])
	// 		t.Log("Received data: ", "'"+data+"'")
	// 		if strings.Contains(data, "\"method\":\"test/notification\"") {
	// 			notificationReceived = true
	// 		}
	// 	}()
	//
	// 	select {
	// 	case <-done:
	// 		if !notificationReceived {
	// 			t.Error("Expected notification not received")
	// 		}
	// 	case <-ctx.Done():
	// 		t.Error("Timeout waiting for notification")
	// 	}
	// })
}
