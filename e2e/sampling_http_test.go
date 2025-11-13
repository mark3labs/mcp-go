package e2e

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// TestSamplingHandler implements client.SamplingHandler for e2e testing
type TestSamplingHandler struct {
	responses map[string]string
	mutex     sync.RWMutex
}

func NewTestSamplingHandler() *TestSamplingHandler {
	return &TestSamplingHandler{
		responses: make(map[string]string),
	}
}

func (h *TestSamplingHandler) SetResponse(question, response string) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.responses[question] = response
}

func (h *TestSamplingHandler) CreateMessage(ctx context.Context, request mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	log.Printf("[TestSamplingHandler] *** CLIENT RECEIVED SAMPLING REQUEST *** with %d messages", len(request.Messages))

	if len(request.Messages) == 0 {
		log.Printf("[TestSamplingHandler] ERROR: no messages provided")
		return nil, fmt.Errorf("no messages provided")
	}

	// Get the last user message
	lastMessage := request.Messages[len(request.Messages)-1]
	userText := ""
	if textContent, ok := lastMessage.Content.(mcp.TextContent); ok {
		userText = textContent.Text
	}

	log.Printf("[TestSamplingHandler] CLIENT processing user text: '%s'", userText)

	h.mutex.RLock()
	response, exists := h.responses[userText]
	h.mutex.RUnlock()

	if !exists {
		response = fmt.Sprintf("Test response to: '%s'", userText)
	}

	log.Printf("[TestSamplingHandler] CLIENT Question: %s -> Response: %s", userText, response)

	result := &mcp.CreateMessageResult{
		SamplingMessage: mcp.SamplingMessage{
			Role: mcp.RoleAssistant,
			Content: mcp.TextContent{
				Type: "text",
				Text: response,
			},
		},
		Model:      "test-model-v1",
		StopReason: "endTurn",
	}

	log.Printf("[TestSamplingHandler] *** CLIENT SENDING SAMPLING RESPONSE *** with model: %s", result.Model)
	return result, nil
}

// getAvailablePort finds an available port for testing
func getAvailablePort() (int, error) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func TestSamplingHTTPE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	log.Printf("[E2E Test] Starting Sampling HTTP E2E Test")

	// Get available port for HTTP server
	port, err := getAvailablePort()
	if err != nil {
		t.Fatalf("Failed to get available port: %v", err)
	}

	serverURL := fmt.Sprintf("http://localhost:%d", port)
	serverAddr := fmt.Sprintf(":%d", port)

	// Create test sampling handler with predefined responses
	samplingHandler := NewTestSamplingHandler()
	samplingHandler.SetResponse("What is the capital of France?", "Paris is the capital of France.")
	samplingHandler.SetResponse("What is 2+2?", "2+2 equals 4.")

	// Create MCP server with sampling capability
	mcpServer := server.NewMCPServer("e2e-sampling-server", "1.0.0")
	mcpServer.EnableSampling()

	// Add tool that uses sampling - this is the "question" tool
	mcpServer.AddTool(mcp.Tool{
		Name:        "question",
		Description: "Ask a question and get an answer using sampling",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"question": map[string]any{
					"type":        "string",
					"description": "The question to ask",
				},
			},
			Required: []string{"question"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		question, err := request.RequireString("question")
		if err != nil {
			return nil, err
		}

		log.Printf("[E2E Test] Tool handler processing question: %s", question)

		// Create sampling request to send back to client
		samplingRequest := mcp.CreateMessageRequest{
			CreateMessageParams: mcp.CreateMessageParams{
				Messages: []mcp.SamplingMessage{
					{
						Role: mcp.RoleUser,
						Content: mcp.TextContent{
							Type: "text",
							Text: question,
						},
					},
				},
				MaxTokens:   500,
				Temperature: 0.7,
			},
		}

		log.Printf("[E2E Test] *** SERVER SENDING SAMPLING REQUEST *** for question: %s", question)

		// Request sampling from client with timeout
		samplingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		serverFromCtx := server.ServerFromContext(ctx)
		if serverFromCtx == nil {
			log.Printf("[E2E Test] ERROR: No server in context")
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: "Error: No server in context",
					},
				},
				IsError: true,
			}, nil
		}

		log.Printf("[E2E Test] SERVER calling RequestSampling...")

		// Check what session we have
		session := server.ClientSessionFromContext(ctx)
		if session != nil {
			log.Printf("[E2E Test] SERVER session ID: %s", session.SessionID())
		} else {
			log.Printf("[E2E Test] SERVER ERROR: No session in context")
		}

		// This creates the sampling request to the client
		result, err := serverFromCtx.RequestSampling(samplingCtx, samplingRequest)
		if err != nil {
			log.Printf("[E2E Test] *** SERVER SAMPLING REQUEST FAILED ***: %v", err)
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: fmt.Sprintf("Error requesting sampling: %v", err),
					},
				},
				IsError: true,
			}, nil
		}

		log.Printf("[E2E Test] *** SERVER RECEIVED SAMPLING RESPONSE ***, model: %s", result.Model)

		// Extract response text
		var responseText string
		if textContent, ok := result.Content.(mcp.TextContent); ok {
			responseText = textContent.Text
		} else {
			responseText = fmt.Sprintf("%v", result.Content)
		}

		// Return sampling response as the question tool response
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Answer: %s (Model: %s)", responseText, result.Model),
				},
			},
		}, nil
	})

	// Start HTTP server
	httpServer := server.NewStreamableHTTPServer(mcpServer)

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		log.Printf("[E2E Test] Starting HTTP server on %s", serverAddr)
		if err := httpServer.Start(serverAddr); err != nil && err != http.ErrServerClosed {
			log.Printf("[E2E Test] Server error: %v", err)
		}
	}()

	// Wait for server to start and be ready
	time.Sleep(2 * time.Second)

	// Create HTTP transport for client connection to server - enable continuous listening for sampling
	httpTransport, err := transport.NewStreamableHTTP(serverURL+"/mcp", transport.WithContinuousListening())
	if err != nil {
		t.Fatalf("Failed to create HTTP transport: %v", err)
	}
	defer httpTransport.Close()

	log.Printf("[E2E Test] HTTP transport created, will connect to: %s", serverURL+"/mcp")

	// Create HTTP client with sampling handler - this is the actual client connecting over HTTP
	httpClient := client.NewClient(httpTransport, client.WithSamplingHandler(samplingHandler))
	defer httpClient.Close()

	// Start the HTTP client
	ctx := context.Background()
	if err := httpClient.Start(ctx); err != nil {
		t.Fatalf("Failed to start HTTP client: %v", err)
	}

	// Initialize MCP session over HTTP
	initRequest := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "e2e-http-test-client",
				Version: "1.0.0",
			},
			Capabilities: mcp.ClientCapabilities{
				// Sampling capability will be automatically added by WithSamplingHandler
			},
		},
	}

	initResponse, err := httpClient.Initialize(ctx, initRequest)
	if err != nil {
		t.Fatalf("Failed to initialize HTTP session: %v", err)
	}

	log.Printf("[E2E Test] HTTP session initialized. Server capabilities: %+v", initResponse.Capabilities)
	log.Printf("[E2E Test] Client session ID: %s", httpTransport.GetSessionId())

	// Verify sampling capability is supported
	if initResponse.Capabilities.Sampling == nil {
		t.Fatal("Server should support sampling capability")
	}

	// Wait a bit more for continuous listening to establish
	log.Printf("[E2E Test] Waiting for continuous listening connection to be established...")
	time.Sleep(3 * time.Second)
	log.Printf("[E2E Test] Continuous listening should now be established, proceeding with tests...")

	// Test Case 1: HTTP client calls "question" tool - complete e2e flow
	t.Run("HTTPClientCallsQuestionTool", func(t *testing.T) {
		log.Printf("[E2E Test] HTTP client calling 'question' tool")

		// Client calls "question" tool over HTTP
		result, err := httpClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "question",
				Arguments: map[string]any{
					"question": "What is the capital of France?",
				},
			},
		})
		if err != nil {
			t.Fatalf("Failed to call question tool over HTTP: %v", err)
		}

		if result.IsError {
			t.Fatalf("Question tool returned error: %v", result.Content)
		}

		if len(result.Content) == 0 {
			t.Fatal("Question tool result should have content")
		}

		// Verify response content
		textContent, ok := result.Content[0].(mcp.TextContent)
		if !ok {
			t.Fatalf("Expected TextContent, got %T", result.Content[0])
		}

		responseText := textContent.Text
		log.Printf("[E2E Test] Question tool response over HTTP: %s", responseText)

		// Verify the complete flow worked: client->server->sampling_request->client->sampling_response->server->tool_response->client
		if !strings.Contains(responseText, "Paris is the capital of France") {
			t.Errorf("Expected response to contain 'Paris is the capital of France', got: %s", responseText)
		}

		if !strings.Contains(responseText, "test-model-v1") {
			t.Errorf("Expected response to contain model name, got: %s", responseText)
		}
	})

	// Test Case 2: Multiple HTTP sampling requests
	t.Run("MultipleHTTPSamplingRequests", func(t *testing.T) {
		questions := []string{
			"What is 2+2?",
			"What is the capital of France?",
		}

		expectedAnswers := []string{
			"2+2 equals 4",
			"Paris is the capital of France",
		}

		for i, question := range questions {
			log.Printf("[E2E Test] HTTP client calling question tool with: %s", question)
			result, err := httpClient.CallTool(ctx, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: "question",
					Arguments: map[string]any{
						"question": question,
					},
				},
			})
			if err != nil {
				t.Fatalf("Failed to call question tool for '%s': %v", question, err)
			}

			if result.IsError {
				t.Fatalf("Question tool returned error for '%s': %v", question, result.Content)
			}

			if len(result.Content) == 0 {
				t.Fatal("Question tool result should have content")
			}

			textContent, ok := result.Content[0].(mcp.TextContent)
			if !ok {
				t.Fatalf("Expected TextContent, got %T", result.Content[0])
			}

			responseText := textContent.Text
			log.Printf("[E2E Test] HTTP Response for '%s': %s", question, responseText)

			if !strings.Contains(responseText, expectedAnswers[i]) {
				t.Errorf("Expected response to contain '%s', got: %s", expectedAnswers[i], responseText)
			}
		}
	})

	// Cleanup
	httpClient.Close()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		t.Logf("Server shutdown error: %v", err)
	}

	<-serverDone
	log.Printf("[E2E Test] HTTP E2E test completed successfully")
}

// TestSamplingHTTPBasic creates a basic HTTP sampling test
func TestSamplingHTTPBasic(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping HTTP test in short mode")
	}

	log.Printf("[E2E HTTP Test] Starting basic HTTP sampling test")

	// Get available port
	port, err := getAvailablePort()
	if err != nil {
		t.Fatalf("Failed to get available port: %v", err)
	}

	serverURL := fmt.Sprintf("http://localhost:%d", port)
	serverAddr := fmt.Sprintf(":%d", port)

	// Create MCP server with sampling capability
	mcpServer := server.NewMCPServer("e2e-http-server", "1.0.0")
	mcpServer.EnableSampling()

	// Add simple echo tool (no sampling needed)
	mcpServer.AddTool(mcp.Tool{
		Name:        "echo",
		Description: "Echo back the input message",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"message": map[string]any{
					"type":        "string",
					"description": "The message to echo back",
				},
			},
			Required: []string{"message"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		message := request.GetString("message", "")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Echo: %s", message),
				},
			},
		}, nil
	})

	// Start HTTP server
	httpServer := server.NewStreamableHTTPServer(mcpServer)

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		log.Printf("[E2E HTTP Test] Starting server on %s", serverAddr)
		if err := httpServer.Start(serverAddr); err != nil && err != http.ErrServerClosed {
			log.Printf("[E2E HTTP Test] Server error: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(500 * time.Millisecond)

	// Create HTTP transport (no continuous listening for simple test)
	httpTransport, err := transport.NewStreamableHTTP(serverURL + "/mcp")
	if err != nil {
		t.Fatalf("Failed to create HTTP transport: %v", err)
	}
	defer httpTransport.Close()

	// Create simple client (no sampling handler)
	mcpClient := client.NewClient(httpTransport)

	// Start client
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = mcpClient.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}

	// Initialize MCP session
	initRequest := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			Capabilities:    mcp.ClientCapabilities{},
			ClientInfo: mcp.Implementation{
				Name:    "e2e-http-test-client",
				Version: "1.0.0",
			},
		},
	}

	initResponse, err := mcpClient.Initialize(ctx, initRequest)
	if err != nil {
		t.Fatalf("Failed to initialize MCP session: %v", err)
	}

	log.Printf("[E2E HTTP Test] Session initialized. Server capabilities: %+v", initResponse.Capabilities)

	// Test basic tool call over HTTP
	result, err := mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "echo",
			Arguments: map[string]any{
				"message": "Hello HTTP MCP!",
			},
		},
	})
	if err != nil {
		t.Fatalf("Failed to call echo tool: %v", err)
	}

	if result.IsError {
		t.Fatalf("Tool returned error: %v", result.Content)
	}

	if len(result.Content) == 0 {
		t.Fatal("Tool result should have content")
	}

	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("Expected TextContent, got %T", result.Content[0])
	}

	responseText := textContent.Text
	log.Printf("[E2E HTTP Test] Tool response: %s", responseText)

	if !strings.Contains(responseText, "Hello HTTP MCP!") {
		t.Errorf("Expected response to contain 'Hello HTTP MCP!', got: %s", responseText)
	}

	// Cleanup
	mcpClient.Close()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		t.Logf("Server shutdown error: %v", err)
	}

	<-serverDone
	log.Printf("[E2E HTTP Test] HTTP test completed successfully")
}

// TestMain sets up test environment
func TestMain(m *testing.M) {
	// Enable debug logging for better visibility during tests
	log.SetOutput(os.Stdout)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	code := m.Run()
	os.Exit(code)
}
