package server

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
)

// TestRaceConditions attempts to trigger race conditions by performing
// concurrent operations on different resources of the MCPServer.
func TestRaceConditions(t *testing.T) {
	// Create a server with all capabilities
	srv := NewMCPServer("test-server", "1.0.0",
		WithResourceCapabilities(true, true),
		WithPromptCapabilities(true),
		WithToolCapabilities(true),
		WithLogging(),
		WithRecovery(),
	)

	// Create a context
	ctx := context.Background()

	// Create a sync.WaitGroup to coordinate test goroutines
	var wg sync.WaitGroup

	// Define test duration
	testDuration := 300 * time.Millisecond

	// Start goroutines to perform concurrent operations
	runConcurrentOperation(&wg, testDuration, "add-prompts", func() {
		name := fmt.Sprintf("prompt-%d", time.Now().UnixNano())
		srv.AddPrompt(mcp.Prompt{
			Name:        name,
			Description: "Test prompt",
		}, func(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return &mcp.GetPromptResult{}, nil
		})
	})

	runConcurrentOperation(&wg, testDuration, "add-tools", func() {
		name := fmt.Sprintf("tool-%d", time.Now().UnixNano())
		srv.AddTool(mcp.Tool{
			Name:        name,
			Description: "Test tool",
		}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})
	})

	runConcurrentOperation(&wg, testDuration, "delete-tools", func() {
		name := fmt.Sprintf("delete-tool-%d", time.Now().UnixNano())
		// Add and immediately delete
		srv.AddTool(mcp.Tool{
			Name:        name,
			Description: "Temporary tool",
		}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})
		srv.DeleteTools(name)
	})

	runConcurrentOperation(&wg, testDuration, "add-middleware", func() {
		middleware := func(next ToolHandlerFunc) ToolHandlerFunc {
			return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return next(ctx, req)
			}
		}
		WithToolHandlerMiddleware(middleware)(srv)
	})

	runConcurrentOperation(&wg, testDuration, "list-tools", func() {
		srv.handleListTools(ctx, "123", mcp.ListToolsRequest{})
	})

	runConcurrentOperation(&wg, testDuration, "list-prompts", func() {
		srv.handleListPrompts(ctx, "123", mcp.ListPromptsRequest{})
	})

	// Add a persistent tool for testing tool calls
	srv.AddTool(mcp.Tool{
		Name:        "persistent-tool",
		Description: "Test tool that always exists",
	}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})

	runConcurrentOperation(&wg, testDuration, "call-tools", func() {
		req := mcp.CallToolRequest{}
		req.Params.Name = "persistent-tool"
		req.Params.Arguments = map[string]interface{}{"param": "test"}
		srv.handleToolCall(ctx, "123", req)
	})

	runConcurrentOperation(&wg, testDuration, "add-resources", func() {
		uri := fmt.Sprintf("resource-%d", time.Now().UnixNano())
		srv.AddResource(mcp.Resource{
			URI:         uri,
			Name:        uri,
			Description: "Test resource",
		}, func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:  uri,
					Text: "Test content",
				},
			}, nil
		})
	})

	// Wait for all operations to complete
	wg.Wait()
	t.Log("No race conditions detected")
}

// Helper function to run an operation concurrently for a specified duration
func runConcurrentOperation(wg *sync.WaitGroup, duration time.Duration, name string, operation func()) {
	wg.Add(1)
	go func() {
		defer wg.Done()

		done := time.After(duration)
		for {
			select {
			case <-done:
				return
			default:
				operation()
			}
		}
	}()
}

// TestConcurrentPromptAdd specifically tests for the deadlock scenario where adding a prompt
// from a goroutine can cause a deadlock
func TestConcurrentPromptAdd(t *testing.T) {
	srv := NewMCPServer("test-server", "1.0.0", WithPromptCapabilities(true))
	ctx := context.Background()

	// Add a prompt with a handler that adds another prompt in a goroutine
	srv.AddPrompt(mcp.Prompt{
		Name:        "initial-prompt",
		Description: "Initial prompt",
	}, func(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		go func() {
			srv.AddPrompt(mcp.Prompt{
				Name:        fmt.Sprintf("new-prompt-%d", time.Now().UnixNano()),
				Description: "Added from handler",
			}, func(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
				return &mcp.GetPromptResult{}, nil
			})
		}()
		return &mcp.GetPromptResult{}, nil
	})

	// Create request and channel to track completion
	req := mcp.GetPromptRequest{}
	req.Params.Name = "initial-prompt"
	done := make(chan struct{})

	// Try to get the prompt - this would deadlock with a single mutex
	go func() {
		srv.handleGetPrompt(ctx, "123", req)
		close(done)
	}()

	// Assert the operation completes without deadlock
	assert.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, 1*time.Second, 10*time.Millisecond, "Deadlock detected: operation did not complete in time")
}
