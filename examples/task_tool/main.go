//go:build ignore
// +build ignore

// NOTE: This example will not compile until task tool implementation is complete.
// Remove the build constraint above once TAS-4 through TAS-10 are implemented.

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// This example demonstrates task-augmented tools in MCP.
// Task-augmented tools execute asynchronously and return results via polling.
//
// NOTE: This example requires the task tool implementation to be complete.
// Specifically, it requires:
//   - TaskToolHandlerFunc type (TAS-4)
//   - ServerTaskTool type (TAS-5)
//   - AddTaskTool method (TAS-6)
//   - handleTaskAugmentedToolCall (TAS-9)
//   - executeTaskTool (TAS-10)
//
// The example includes three types of tools:
// 1. process_batch - A TaskSupportRequired tool that processes items in batch
// 2. analyze_data - A TaskSupportOptional tool that can run sync or async
// 3. quick_check - A regular synchronous tool for comparison
//
// Usage:
//   go run main.go              # Start with stdio transport
//   go run main.go -t http      # Start with HTTP transport on :8080

func main() {
	var transport string
	flag.StringVar(&transport, "t", "stdio", "Transport type (stdio or http)")
	flag.StringVar(&transport, "transport", "stdio", "Transport type (stdio or http)")
	flag.Parse()

	mcpServer := NewTaskToolServer()

	if transport == "http" {
		httpServer := server.NewStreamableHTTPServer(mcpServer)
		log.Printf("HTTP server listening on :8080/mcp")
		if err := httpServer.Start(":8080"); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	} else {
		if err := server.ServeStdio(mcpServer); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}
}

// NewTaskToolServer creates a new MCP server with task-augmented tool examples.
func NewTaskToolServer() *server.MCPServer {
	// Set up task observability hooks
	taskHooks := &server.TaskHooks{}
	taskHooks.AddOnTaskCreated(func(ctx context.Context, metrics server.TaskMetrics) {
		log.Printf("[METRICS] Task created: %s (tool: %s)", metrics.TaskID, metrics.ToolName)
	})
	taskHooks.AddOnTaskCompleted(func(ctx context.Context, metrics server.TaskMetrics) {
		log.Printf("[METRICS] Task completed: %s (tool: %s, duration: %v)",
			metrics.TaskID, metrics.ToolName, metrics.Duration)
	})
	taskHooks.AddOnTaskFailed(func(ctx context.Context, metrics server.TaskMetrics) {
		log.Printf("[METRICS] Task failed: %s (tool: %s, duration: %v, error: %v)",
			metrics.TaskID, metrics.ToolName, metrics.Duration, metrics.Error)
	})
	taskHooks.AddOnTaskCancelled(func(ctx context.Context, metrics server.TaskMetrics) {
		log.Printf("[METRICS] Task cancelled: %s (tool: %s, duration: %v)",
			metrics.TaskID, metrics.ToolName, metrics.Duration)
	})

	// Create server with task capabilities enabled
	// listTasks: allows clients to list all tasks
	// cancel: allows clients to cancel running tasks
	// toolCallTasks: enables task augmentation for tools
	mcpServer := server.NewMCPServer(
		"example-servers/task-tool",
		"1.0.0",
		server.WithTaskCapabilities(true, true, true),
		server.WithToolCapabilities(true),
		server.WithTaskHooks(taskHooks),
		server.WithMaxConcurrentTasks(10), // Limit to 10 concurrent tasks
		server.WithLogging(),
	)

	// Example 1: Task-Required Tool
	// This tool MUST be called with task augmentation
	processBatchTool := mcp.NewTool("process_batch",
		mcp.WithDescription("Process a batch of items asynchronously. This is a long-running operation that must be executed as a task."),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
		mcp.WithArray("items",
			mcp.Description("Array of items to process"),
			mcp.WithStringItems(),
			mcp.Required(),
		),
		mcp.WithNumber("delay",
			mcp.Description("Delay in seconds per item (simulates processing time)"),
			mcp.DefaultNumber(1.0),
		),
	)
	mcpServer.AddTaskTool(processBatchTool, handleProcessBatch)

	// Example 2: Task-Optional Tool
	// This tool can be called with or without task augmentation
	analyzeDataTool := mcp.NewTool("analyze_data",
		mcp.WithDescription("Analyze data. Can run synchronously (fast) or asynchronously (thorough) depending on how it's called."),
		mcp.WithTaskSupport(mcp.TaskSupportOptional),
		mcp.WithString("data",
			mcp.Description("Data to analyze"),
			mcp.Required(),
		),
		mcp.WithBoolean("thorough",
			mcp.Description("Whether to perform thorough analysis (slower)"),
			mcp.DefaultBool(false),
		),
	)
	mcpServer.AddTaskTool(analyzeDataTool, handleAnalyzeData)

	// Example 3: Regular Synchronous Tool (for comparison)
	quickCheckTool := mcp.NewTool("quick_check",
		mcp.WithDescription("Perform a quick synchronous check. This tool does not support task augmentation."),
		mcp.WithString("input",
			mcp.Description("Input to check"),
			mcp.Required(),
		),
	)
	mcpServer.AddTool(quickCheckTool, handleQuickCheck)

	return mcpServer
}

// handleProcessBatch processes items in a batch asynchronously.
// This demonstrates a task-required tool that performs long-running work.
func handleProcessBatch(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CreateTaskResult, error) {
	items := request.GetStringSlice("items", []string{})
	delay := request.GetFloat("delay", 1.0)

	if len(items) == 0 {
		return nil, fmt.Errorf("items array cannot be empty")
	}

	log.Printf("Starting batch processing of %d items with %v second delay per item", len(items), delay)

	// Process each item with delay
	results := make([]string, 0, len(items))
	for i, item := range items {
		select {
		case <-ctx.Done():
			// Task was cancelled
			log.Printf("Batch processing cancelled after %d/%d items", i, len(items))
			return nil, ctx.Err()
		default:
			// Simulate processing time
			time.Sleep(time.Duration(delay * float64(time.Second)))
			result := fmt.Sprintf("Processed '%s' [%d/%d]", item, i+1, len(items))
			results = append(results, result)
			log.Printf("Item %d/%d: %s", i+1, len(items), item)
		}
	}

	log.Printf("Batch processing completed: %d items", len(items))

	// Return the result
	return &mcp.CreateTaskResult{
		Task: mcp.Task{
			// Task fields are managed by the server
		},
	}, nil
}

// handleAnalyzeData analyzes data either quickly or thoroughly.
// This demonstrates a task-optional tool that adapts based on how it's called.
func handleAnalyzeData(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CreateTaskResult, error) {
	data := request.GetString("data", "")
	thorough := request.GetBool("thorough", false)

	if data == "" {
		return nil, fmt.Errorf("data cannot be empty")
	}

	analysisType := "quick"
	duration := 500 * time.Millisecond
	if thorough {
		analysisType = "thorough"
		duration = 3 * time.Second
	}

	log.Printf("Starting %s analysis of data: %s", analysisType, data)

	// Simulate analysis time
	select {
	case <-ctx.Done():
		log.Printf("Analysis cancelled")
		return nil, ctx.Err()
	case <-time.After(duration):
		// Analysis complete
	}

	// Generate analysis results
	wordCount := len(data)
	charCount := len([]rune(data))

	result := fmt.Sprintf("Analysis complete (%s mode):\n", analysisType)
	result += fmt.Sprintf("- Characters: %d\n", charCount)
	result += fmt.Sprintf("- Words: %d\n", wordCount)

	if thorough {
		result += fmt.Sprintf("- Uppercase: %d\n", countUppercase(data))
		result += fmt.Sprintf("- Lowercase: %d\n", countLowercase(data))
		result += fmt.Sprintf("- Digits: %d\n", countDigits(data))
	}

	log.Printf("Analysis completed: %s mode, %d characters", analysisType, charCount)

	return &mcp.CreateTaskResult{
		Task: mcp.Task{
			// Task fields are managed by the server
		},
	}, nil
}

// handleQuickCheck performs a quick synchronous check.
// This demonstrates a regular tool without task support for comparison.
func handleQuickCheck(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	input := request.GetString("input", "")

	if input == "" {
		return nil, fmt.Errorf("input cannot be empty")
	}

	log.Printf("Quick check: %s", input)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Quick check passed for: %s (length: %d)", input, len(input)),
			},
		},
	}, nil
}

// Helper functions for thorough analysis

func countUppercase(s string) int {
	count := 0
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			count++
		}
	}
	return count
}

func countLowercase(s string) int {
	count := 0
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			count++
		}
	}
	return count
}

func countDigits(s string) int {
	count := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			count++
		}
	}
	return count
}
