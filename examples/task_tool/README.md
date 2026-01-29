# Task Tool Example

This example demonstrates how to implement task-augmented tools in mcp-go. Task-augmented tools enable long-running, asynchronous operations with status polling and deferred result retrieval.

## Overview

The example includes three tools that showcase different task support modes:

1. **process_batch** - Task-Required Tool
   - MUST be called with task parameters (asynchronously)
   - Processes a batch of items with configurable delay
   - Demonstrates cancellation support

2. **compute_fibonacci** - Optional Task Tool
   - Can be called either synchronously OR asynchronously
   - Computes Fibonacci numbers
   - Shows hybrid mode where the same tool supports both calling patterns

3. **analyze_data** - Task-Required Tool
   - MUST be called asynchronously
   - Simulates a long-running data analysis operation
   - Demonstrates chunked processing with cancellation

## Building

```bash
cd examples/task_tool
go build -o task_tool main.go
```

## Running

### stdio transport (default)
```bash
./task_tool
```

### HTTP transport
```bash
./task_tool -transport http
```

The HTTP server will listen on `:8080/mcp`.

## Task Workflow

When a client calls a task-augmented tool:

1. **Call tool with task params** - The client calls `tools/call` with a `task` parameter
2. **Receive task ID immediately** - The server returns a `CreateTaskResult` with task ID immediately
3. **Task executes asynchronously** - The tool handler runs in the background
4. **Poll task status** - Client can call `tasks/get` to check status
5. **Retrieve result** - Once complete, client calls `tasks/result` to get the actual result

## Example Client Interaction

### Calling a Task-Required Tool

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "process_batch",
    "arguments": {
      "items": ["item1", "item2", "item3"],
      "delay_ms": 100
    },
    "task": {
      "ttl": 300
    }
  }
}
```

Response (immediate):
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "_meta": {
      "task": {
        "taskId": "550e8400-e29b-41d4-a716-446655440000",
        "status": "running",
        "createdAt": "2025-01-29T14:00:00Z"
      }
    }
  }
}
```

### Checking Task Status

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tasks/get",
  "params": {
    "taskId": "550e8400-e29b-41d4-a716-446655440000"
  }
}
```

Response:
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "task": {
      "taskId": "550e8400-e29b-41d4-a716-446655440000",
      "status": "completed",
      "createdAt": "2025-01-29T14:00:00Z",
      "lastUpdatedAt": "2025-01-29T14:00:03Z"
    }
  }
}
```

### Retrieving Task Result

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tasks/result",
  "params": {
    "taskId": "550e8400-e29b-41d4-a716-446655440000"
  }
}
```

Response:
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "Successfully processed 3 items:\n[1] Processed: item1\n[2] Processed: item2\n[3] Processed: item3"
      }
    ],
    "_meta": {
      "io.modelcontextprotocol/related-task": {
        "taskId": "550e8400-e29b-41d4-a716-446655440000"
      }
    }
  }
}
```

### Calling an Optional Task Tool Synchronously

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "tools/call",
  "params": {
    "name": "compute_fibonacci",
    "arguments": {
      "n": 10
    }
  }
}
```

Response (immediate with result):
```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "Fibonacci(10) = 55"
      }
    ]
  }
}
```

## Task Support Modes

### TaskSupportRequired
Tools with this mode MUST be called with task parameters. They will return an error if called without task augmentation.

```go
tool := mcp.NewTool("async_only",
    mcp.WithTaskSupport(mcp.TaskSupportRequired),
)
server.AddTaskTool(tool, handler)
```

### TaskSupportOptional
Tools with this mode can be called either way:
- **With task params**: Returns immediately, executes async
- **Without task params**: Returns result directly, executes sync

```go
tool := mcp.NewTool("flexible",
    mcp.WithTaskSupport(mcp.TaskSupportOptional),
)
server.AddTaskTool(tool, handler)
```

### TaskSupportForbidden (default)
Regular tools that cannot be called with task augmentation.

## Cancellation Support

All task handlers receive a context that will be cancelled if:
- The client calls `tasks/cancel`
- The task TTL expires
- The client disconnects
- The server shuts down

Example:
```go
func handler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    for i := 0; i < 100; i++ {
        select {
        case <-ctx.Done():
            return nil, ctx.Err() // Handle cancellation
        default:
            // Do work
        }
    }
    return result, nil
}
```

## See Also

- [MCP Tasks Specification](https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/tasks)
- [mcp-go Documentation](../../README.md)
