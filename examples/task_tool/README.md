# Task Tool Example

This example demonstrates the MCP Tasks capability for asynchronous, long-running operations.

## Overview

Task-augmented tools enable servers to execute operations asynchronously, returning results via polling rather than blocking the client. This is ideal for:

- Long-running operations (batch processing, data analysis, etc.)
- Operations that may take an unpredictable amount of time
- Operations where clients need the ability to cancel mid-execution

## Prerequisites

This example requires the following implementation tasks to be completed:

- ✅ TAS-1: ToolExecution types and TaskSupport constants
- ✅ TAS-2: WithTaskSupport option
- ✅ TAS-3: TaskParams field in CallToolParams
- ⏳ TAS-4: TaskToolHandlerFunc type
- ⏳ TAS-5: ServerTaskTool type
- ⏳ TAS-6: AddTaskTool method
- ⏳ TAS-9: handleTaskAugmentedToolCall
- ⏳ TAS-10: executeTaskTool

Once these are complete, remove the `//go:build ignore` constraint from `main.go`.

## Tools Demonstrated

### 1. process_batch (TaskSupportRequired)

A tool that **must** be called with task augmentation. It processes a batch of items with a configurable delay per item.

**Parameters:**
- `items` (array of strings, required): Items to process
- `delay` (number, default 1.0): Delay in seconds per item

**Example call:**
```json
{
  "name": "process_batch",
  "arguments": {
    "items": ["item1", "item2", "item3"],
    "delay": 0.5
  },
  "task": {
    "ttl": 300
  }
}
```

### 2. analyze_data (TaskSupportOptional)

A tool that **can** be called with or without task augmentation:
- **Without task param**: Quick synchronous analysis (500ms)
- **With task param**: Thorough asynchronous analysis (3s)

**Parameters:**
- `data` (string, required): Data to analyze
- `thorough` (boolean, default false): Whether to perform thorough analysis

**Example call (synchronous):**
```json
{
  "name": "analyze_data",
  "arguments": {
    "data": "Sample text to analyze",
    "thorough": false
  }
}
```

**Example call (asynchronous):**
```json
{
  "name": "analyze_data",
  "arguments": {
    "data": "Sample text to analyze",
    "thorough": true
  },
  "task": {
    "ttl": 60
  }
}
```

### 3. quick_check (No Task Support)

A regular synchronous tool for comparison. It cannot be called with task augmentation.

**Parameters:**
- `input` (string, required): Input to check

**Example call:**
```json
{
  "name": "quick_check",
  "arguments": {
    "input": "test"
  }
}
```

## Task Workflow

When a tool is called with task augmentation:

1. **Client calls tool** with `task` parameter
2. **Server returns immediately** with task creation result containing `taskId`
3. **Client polls** for status using `tasks/get` with the `taskId`
4. **Tool executes asynchronously** in background
5. **Server updates** task status (`pending` → `completed` or `failed`)
6. **Client retrieves result** using `tasks/result` once status is `completed`

## Running the Example

Once implementation is complete:

```bash
# STDIO transport (for use with MCP clients)
go run main.go

# HTTP transport (for testing with curl/Postman)
go run main.go -t http
```

### Testing with HTTP

Start the server with HTTP transport:
```bash
go run main.go -t http
```

Initialize the connection:
```bash
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
      "protocolVersion": "2025-11-25",
      "capabilities": {},
      "clientInfo": {"name": "test-client", "version": "1.0.0"}
    }
  }'
```

Call a task tool:
```bash
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/call",
    "params": {
      "name": "process_batch",
      "arguments": {
        "items": ["apple", "banana", "cherry"],
        "delay": 0.5
      },
      "task": {
        "ttl": 300
      }
    }
  }'
```

Poll for task status:
```bash
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "tasks/get",
    "params": {
      "taskId": "<taskId from previous response>"
    }
  }'
```

Get the result once complete:
```bash
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 4,
    "method": "tasks/result",
    "params": {
      "taskId": "<taskId>"
    }
  }'
```

## Task Cancellation

Tasks can be cancelled while running:

```bash
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 5,
    "method": "tasks/cancel",
    "params": {
      "taskId": "<taskId>",
      "reason": "User requested cancellation"
    }
  }'
```

The tool handler will receive a cancelled context and can clean up accordingly.

## Key Features Demonstrated

- **Task Support Modes**: Required, Optional, and Forbidden (regular tool)
- **Asynchronous Execution**: Long-running operations don't block the client
- **Context Cancellation**: Proper handling of cancelled operations
- **Hybrid Mode**: Optional tools that work both ways
- **Error Handling**: Validation and error reporting
- **Logging**: Server-side progress tracking

## See Also

- [MCP Tasks Specification](https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/tasks)
- [Implementation Spec](/openspec/proposal-001-task-tools.md)
- Task implementation tests in `server/task_tool_test.go`
