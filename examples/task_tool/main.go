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

// NewTaskServer creates an MCP server with task-augmented tools
func NewTaskServer() *server.MCPServer {
	// Set up task lifecycle hooks for observability
	hooks := &server.Hooks{}

	// Track when tasks are created
	hooks.AddOnTaskCreated(func(ctx context.Context, task mcp.Task) {
		log.Printf("📝 Task created: %s (status: %s)", task.TaskId, task.Status)
	})

	// Track status transitions
	hooks.AddOnTaskStatusChanged(func(ctx context.Context, task mcp.Task, oldStatus mcp.TaskStatus) {
		log.Printf("🔄 Task %s: %s → %s", task.TaskId, oldStatus, task.Status)
	})

	// Track completions (success, failure, or cancellation)
	hooks.AddOnTaskCompleted(func(ctx context.Context, task mcp.Task, err error) {
		createdAt, _ := time.Parse(time.RFC3339, task.CreatedAt)
		duration := time.Since(createdAt)

		if err != nil {
			log.Printf("❌ Task %s failed after %v: %v", task.TaskId, duration.Round(time.Millisecond), err)
		} else if task.Status == mcp.TaskStatusCancelled {
			log.Printf("🚫 Task %s cancelled after %v", task.TaskId, duration.Round(time.Millisecond))
		} else {
			log.Printf("✅ Task %s completed in %v", task.TaskId, duration.Round(time.Millisecond))
		}
	})

	// Create server with task capabilities enabled
	mcpServer := server.NewMCPServer(
		"example-servers/task-tool",
		"1.0.0",
		// Enable task capabilities: list, cancel, and tool call tasks
		server.WithTaskCapabilities(true, true, true),
		// Add hooks for observability
		server.WithHooks(hooks),
	)

	// Example 1: Task-Required Tool
	// This tool MUST be called with task parameters (asynchronously)
	batchTool := mcp.NewTool("process_batch",
		mcp.WithDescription("Process a batch of items asynchronously (requires task augmentation)"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
		mcp.WithArray("items",
			mcp.WithStringItems(),
			mcp.Description("List of items to process"),
			mcp.Required(),
		),
		mcp.WithNumber("delay_ms",
			mcp.Description("Delay between items in milliseconds"),
			mcp.DefaultNumber(100),
		),
	)

	if err := mcpServer.AddTaskTool(batchTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		items := request.GetStringSlice("items", nil)
		delayMs := request.GetFloat("delay_ms", 100)

		results := make([]string, 0, len(items))

		// Process items with cancellation support
		for i, item := range items {
			select {
			case <-ctx.Done():
				// Task was cancelled
				return nil, fmt.Errorf("processing cancelled at item %d: %w", i, ctx.Err())
			default:
				// Simulate processing time
				time.Sleep(time.Duration(delayMs) * time.Millisecond)
				processed := fmt.Sprintf("[%d] Processed: %s", i+1, item)
				results = append(results, processed)
				log.Printf("Item %d/%d: %s", i+1, len(items), item)
			}
		}

		// Return the results
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Successfully processed %d items:\n%s",
						len(results),
						joinResults(results)),
				},
			},
		}, nil
	}); err != nil {
		log.Fatalf("Failed to add process_batch tool: %v", err)
	}

	// Example 2: Optional Task Tool
	// This tool can be called either synchronously (without task params) or asynchronously (with task params)
	computeTool := mcp.NewTool("compute_fibonacci",
		mcp.WithDescription("Compute Fibonacci numbers (supports both sync and async calls)"),
		mcp.WithTaskSupport(mcp.TaskSupportOptional),
		mcp.WithNumber("n",
			mcp.Description("The Fibonacci number to compute"),
			mcp.Required(),
		),
	)

	if err := mcpServer.AddTaskTool(computeTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		n := int(request.GetFloat("n", 0))

		if n < 0 {
			return nil, fmt.Errorf("n must be non-negative")
		}

		// Compute Fibonacci with cancellation support
		result, err := fibonacciWithContext(ctx, n)
		if err != nil {
			return nil, err
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Fibonacci(%d) = %d", n, result),
				},
			},
		}, nil
	}); err != nil {
		log.Fatalf("Failed to add compute_fibonacci tool: %v", err)
	}

	// Example 3: Simulate Long-Running Work
	// Demonstrates a realistic long-running operation
	analysisT := mcp.NewTool("analyze_data",
		mcp.WithDescription("Analyze data asynchronously with progress updates"),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
		mcp.WithString("dataset",
			mcp.Description("Dataset name to analyze"),
			mcp.Required(),
		),
		mcp.WithNumber("samples",
			mcp.Description("Number of samples to analyze"),
			mcp.DefaultNumber(1000),
		),
	)

	if err := mcpServer.AddTaskTool(analysisT, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		dataset := request.GetString("dataset", "")
		samples := int(request.GetFloat("samples", 1000))

		log.Printf("Starting analysis of dataset '%s' with %d samples", dataset, samples)

		// Simulate analysis work in chunks
		chunkSize := 100
		totalChunks := (samples + chunkSize - 1) / chunkSize

		for chunk := 0; chunk < totalChunks; chunk++ {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("analysis cancelled at chunk %d/%d: %w", chunk+1, totalChunks, ctx.Err())
			default:
				// Simulate chunk processing
				time.Sleep(200 * time.Millisecond)
				log.Printf("Analyzed chunk %d/%d", chunk+1, totalChunks)
			}
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Analysis complete for dataset '%s':\n- Samples analyzed: %d\n- Mean: 42.7\n- StdDev: 15.3\n- Status: SUCCESS",
						dataset, samples),
				},
			},
		}, nil
	}); err != nil {
		log.Fatalf("Failed to add analyze_data tool: %v", err)
	}

	return mcpServer
}

// fibonacciWithContext computes Fibonacci number with context cancellation support
func fibonacciWithContext(ctx context.Context, n int) (int64, error) {
	if n <= 1 {
		return int64(n), nil
	}

	a, b := int64(0), int64(1)
	for i := 2; i <= n; i++ {
		// Check for cancellation periodically
		if i%100 == 0 {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			default:
			}
		}
		a, b = b, a+b
	}
	return b, nil
}

// joinResults joins result strings with newlines
func joinResults(results []string) string {
	if len(results) == 0 {
		return ""
	}
	s := results[0]
	for i := 1; i < len(results); i++ {
		s += "\n" + results[i]
	}
	return s
}

func main() {
	var transport string
	flag.StringVar(&transport, "t", "stdio", "Transport type (stdio or http)")
	flag.StringVar(&transport, "transport", "stdio", "Transport type (stdio or http)")
	flag.Parse()

	mcpServer := NewTaskServer()

	log.Println("Task Tool Server Starting...")
	log.Println("Available task-augmented tools:")
	log.Println("  - process_batch: Process items asynchronously (required)")
	log.Println("  - compute_fibonacci: Compute Fibonacci numbers (optional)")
	log.Println("  - analyze_data: Analyze dataset asynchronously (required)")

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
