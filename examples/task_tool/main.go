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
// Task-augmented tools execute asynchronously: the server returns a task handle
// immediately, and the client polls tasks/get until the task reaches a terminal
// state (completed, failed, or cancelled).
// The context passed to a task handler is rooted at the server's lifetime, not
// the per-request context. It is only cancelled when tasks/cancel is called or
// the server shuts down. Blocking on ctx.Done() is safe and is the recommended
// way to honour cancellation.
//
// Usage:
//	go run main.go              # stdio transport
//	go run main.go -t http      # Streamable HTTP on :8080

func main() {
	transport := flag.String("t", "stdio", "Transport: stdio or http")
	flag.Parse()

	srv := newServer()

	switch *transport {
	case "http":
		log.Println("Streamable HTTP listening on :8080")
		if err := server.NewStreamableHTTPServer(srv).Start(":8080"); err != nil {
			log.Fatal(err)
		}
	default:
		if err := server.ServeStdio(srv); err != nil {
			log.Fatal(err)
		}
	}
}

func newServer() *server.MCPServer {
	hooks := &server.TaskHooks{}
	hooks.AddOnTaskCreated(func(_ context.Context, m server.TaskMetrics) {
		log.Printf("[task] created  %s  tool=%s", m.TaskID, m.ToolName)
	})
	hooks.AddOnTaskCompleted(func(_ context.Context, m server.TaskMetrics) {
		log.Printf("[task] done     %s  tool=%s  duration=%v", m.TaskID, m.ToolName, m.Duration)
	})
	hooks.AddOnTaskFailed(func(_ context.Context, m server.TaskMetrics) {
		log.Printf("[task] failed   %s  tool=%s  err=%v", m.TaskID, m.ToolName, m.Error)
	})
	hooks.AddOnTaskCancelled(func(_ context.Context, m server.TaskMetrics) {
		log.Printf("[task] cancelled %s  tool=%s", m.TaskID, m.ToolName)
	})

	srv := server.NewMCPServer(
		"example-task-tool",
		"1.0.0",
		server.WithTaskCapabilities(true, true, true),
		// advertise io.modelcontextprotocol/tasks for SEP-2663 clients
		server.WithTasksExtension(),
		server.WithToolCapabilities(true),
		server.WithTaskHooks(hooks),
		server.WithMaxConcurrentTasks(10),
	)

	// Tool 1: must always run as a task.
	processBatch := mcp.NewTool("process_batch",
		mcp.WithDescription("Process items asynchronously (task required)."),
		mcp.WithTaskSupport(mcp.TaskSupportRequired),
		mcp.WithArray("items",
			mcp.Description("Items to process"),
			mcp.WithStringItems(),
			mcp.Required(),
		),
		mcp.WithNumber("delay_ms",
			mcp.Description("Simulated delay per item in ms"),
			mcp.DefaultNumber(200),
		),
	)
	srv.AddTaskTool(processBatch, handleProcessBatch)

	// Tool 2: can run synchronously or as a task.
	analyzeData := mcp.NewTool("analyze_data",
		mcp.WithDescription("Analyse text — quick sync or thorough async."),
		mcp.WithTaskSupport(mcp.TaskSupportOptional),
		mcp.WithString("text", mcp.Required()),
		mcp.WithBoolean("thorough", mcp.DefaultBool(false)),
	)
	// For optional-task tools the handler only runs when the tool is invoked
	// as a task. Synchronous calls go to a separate handler if registered via
	// AddTool.
	srv.AddTaskTool(analyzeData, handleAnalyzeData)

	// Tool 3: plain synchronous tool, no task support.
	quickCheck := mcp.NewTool("quick_check",
		mcp.WithDescription("Synchronous sanity check."),
		mcp.WithString("input", mcp.Required()),
	)
	srv.AddTool(quickCheck, handleQuickCheck)

	return srv
}

// handleProcessBatch is a TaskToolHandlerFunc, it runs in a background
// goroutine and returns a *CreateTaskResult when the work is done.
func handleProcessBatch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
	items := req.GetStringSlice("items", nil)
	delayMs := req.GetFloat("delay_ms", 200)
	delay := time.Duration(delayMs) * time.Millisecond

	log.Printf("process_batch: starting %d items, delay=%v", len(items), delay)

	for i, item := range items {
		select {
		case <-ctx.Done():
			log.Printf("process_batch: cancelled after %d/%d items", i, len(items))
			return nil, ctx.Err()
		case <-time.After(delay):
		}

		log.Printf("process_batch: [%d/%d] %s", i+1, len(items), item)
	}

	log.Printf("process_batch: finished %d items", len(items))
	r := mcp.NewCreateTaskResult(mcp.Task{})
	return &r, nil
}

// handleAnalyzeData is a TaskToolHandlerFunc used for the optional-task tool.
func handleAnalyzeData(ctx context.Context, req mcp.CallToolRequest) (*mcp.CreateTaskResult, error) {
	text := req.GetString("text", "")
	thorough := req.GetBool("thorough", false)

	duration := 100 * time.Millisecond
	if thorough {
		duration = 2 * time.Second
	}

	// Here we register only the task handler via AddTaskTool so the
	// synchronous path returns an error when called without a task param.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(duration):
	}

	summary := fmt.Sprintf("analysed %d chars", len([]rune(text)))
	if thorough {
		summary += fmt.Sprintf(", upper=%d lower=%d digits=%d",
			countRunes(text, 'A', 'Z'),
			countRunes(text, 'a', 'z'),
			countRunes(text, '0', '9'),
		)
	}
	log.Printf("analyze_data: %s", summary)

	r := mcp.NewCreateTaskResult(mcp.Task{})
	return &r, nil
}

// handleQuickCheck is a plain ToolHandlerFunc, synchronous, no task involved.
func handleQuickCheck(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	input := req.GetString("input", "")
	return mcp.NewToolResultText(fmt.Sprintf("ok: %q (%d chars)", input, len(input))), nil
}

func countRunes(s string, lo, hi rune) int {
	n := 0
	for _, r := range s {
		if r >= lo && r <= hi {
			n++
		}
	}
	return n
}
