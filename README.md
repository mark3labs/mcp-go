<!-- omit in toc -->
<div align="center">
<img src="./logo.png" alt="MCP Go Logo">

[![Build](https://github.com/mark3labs/mcp-go/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/mark3labs/mcp-go/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/mark3labs/mcp-go?cache)](https://goreportcard.com/report/github.com/mark3labs/mcp-go)
[![GoDoc](https://pkg.go.dev/badge/github.com/mark3labs/mcp-go.svg)](https://pkg.go.dev/github.com/mark3labs/mcp-go)

<strong>A Go implementation of the Model Context Protocol (MCP), enabling seamless integration between LLM applications and external data sources and tools.</strong>

<br>

[![Tutorial](http://img.youtube.com/vi/qoaeYMrXJH0/0.jpg)](http://www.youtube.com/watch?v=qoaeYMrXJH0 "Tutorial")

<br>

Discuss the SDK on [Discord](https://discord.gg/RqSS2NQVsY)

</div>


```go
package main

import (
    "context"
    "errors"
    "fmt"

    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"
)

func main() {
    // Create a new MCP server
    s := server.NewMCPServer(
        "Demo 🚀",
        "1.0.0",
        server.WithToolCapabilities(false),
    )

    // Add tool
    tool := mcp.NewTool("hello_world",
        mcp.WithDescription("Say hello to someone"),
        mcp.WithString("name",
            mcp.Required(),
            mcp.Description("Name of the person to greet"),
        ),
    )

    // Add tool handler
    s.AddTool(tool, helloHandler)

    // Start the stdio server
    if err := server.ServeStdio(s); err != nil {
        fmt.Printf("Server error: %v\n", err)
    }
}

func helloHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    name, err := request.RequireString("name")
    if err != nil {
        return mcp.NewToolResultError(err.Error()), nil
    }

    return mcp.NewToolResultText(fmt.Sprintf("Hello, %s!", name)), nil
}
```

That's it!

MCP Go handles all the complex protocol details and server management, so you can focus on building great tools. It aims to be high-level and easy to use.

### Key features:
* **Fast**: High-level interface means less code and faster development
* **Simple**: Build MCP servers with minimal boilerplate
* **Complete***: MCP Go aims to provide a full implementation of the core MCP specification

(\*emphasis on *aims*)

🚨 🚧 🏗️ *MCP Go is under active development, as is the MCP specification itself. Core features are working but some advanced capabilities are still in progress.* 


<!-- omit in toc -->
## Table of Contents

- [Installation](#installation)
- [Quickstart](#quickstart)
- [What is MCP?](#what-is-mcp)
- [Core Concepts](#core-concepts)
  - [Server](#server)
  - [Resources](#resources)
  - [Tools](#tools)
  - [Prompts](#prompts)
- [Examples](#examples)
- [Extras](#extras)
  - [Transports](#transports)
  - [Session Management](#session-management)
    - [Basic Session Handling](#basic-session-handling)
    - [Per-Session Tools](#per-session-tools)
    - [Tool Filtering](#tool-filtering)
    - [Working with Context](#working-with-context)
  - [Request Hooks](#request-hooks)
  - [Tool Handler Middleware](#tool-handler-middleware)
  - [Regenerating Server Code](#regenerating-server-code)

## Installation

```bash
go get github.com/mark3labs/mcp-go
```

## Quickstart

Let's create a simple MCP server that exposes a calculator tool and some data:

```go
package main

import (
    "context"
    "fmt"

    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"
)

func main() {
    // Create a new MCP server
    s := server.NewMCPServer(
        "Calculator Demo",
        "1.0.0",
        server.WithToolCapabilities(false),
        server.WithRecovery(),
    )

    // Add a calculator tool
    calculatorTool := mcp.NewTool("calculate",
        mcp.WithDescription("Perform basic arithmetic operations"),
        mcp.WithString("operation",
            mcp.Required(),
            mcp.Description("The operation to perform (add, subtract, multiply, divide)"),
            mcp.Enum("add", "subtract", "multiply", "divide"),
        ),
        mcp.WithNumber("x",
            mcp.Required(),
            mcp.Description("First number"),
        ),
        mcp.WithNumber("y",
            mcp.Required(),
            mcp.Description("Second number"),
        ),
    )

    // Add the calculator handler
    s.AddTool(calculatorTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        // Using helper functions for type-safe argument access
        op, err := request.RequireString("operation")
        if err != nil {
            return mcp.NewToolResultError(err.Error()), nil
        }
        
        x, err := request.RequireFloat("x")
        if err != nil {
            return mcp.NewToolResultError(err.Error()), nil
        }
        
        y, err := request.RequireFloat("y")
        if err != nil {
            return mcp.NewToolResultError(err.Error()), nil
        }

        var result float64
        switch op {
        case "add":
            result = x + y
        case "subtract":
            result = x - y
        case "multiply":
            result = x * y
        case "divide":
            if y == 0 {
                return mcp.NewToolResultError("cannot divide by zero"), nil
            }
            result = x / y
        }

        return mcp.NewToolResultText(fmt.Sprintf("%.2f", result)), nil
    })

    // Start the server
    if err := server.ServeStdio(s); err != nil {
        fmt.Printf("Server error: %v\n", err)
    }
}
```

## What is MCP?

The [Model Context Protocol (MCP)](https://modelcontextprotocol.io) lets you build servers that expose data and functionality to LLM applications in a secure, standardized way. Think of it like a web API, but specifically designed for LLM interactions. MCP servers can:

- Expose data through **Resources** (think of these sort of like GET endpoints; they are used to load information into the LLM's context)
- Provide functionality through **Tools** (sort of like POST endpoints; they are used to execute code or otherwise produce a side effect)
- Define interaction patterns through **Prompts** (reusable templates for LLM interactions)
- And more!


## Core Concepts


### Server

<details>
<summary>Show Server Examples</summary>

The server is your core interface to the MCP protocol. It handles connection management, protocol compliance, and message routing:

```go
// Create a basic server
s := server.NewMCPServer(
    "My Server",  // Server name
    "1.0.0",     // Version
)

// Start the server using stdio
if err := server.ServeStdio(s); err != nil {
    log.Fatalf("Server error: %v", err)
}
```

</details>

### Resources

<details>
<summary>Show Resource Examples</summary>
Resources are how you expose data to LLMs. They can be anything - files, API responses, database queries, system information, etc. Resources can be:

- Static (fixed URI)
- Dynamic (using URI templates)

Here's a simple example of a static resource:

```go
// Static resource example - exposing a README file
resource := mcp.NewResource(
    "docs://readme",
    "Project README",
    mcp.WithResourceDescription("The project's README file"), 
    mcp.WithMIMEType("text/markdown"),
)

// Add resource with its handler
s.AddResource(resource, func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
    content, err := os.ReadFile("README.md")
    if err != nil {
        return nil, err
    }
    
    return []mcp.ResourceContents{
        mcp.TextResourceContents{
            URI:      "docs://readme",
            MIMEType: "text/markdown",
            Text:     string(content),
        },
    }, nil
})
```

And here's an example of a dynamic resource using a template:

```go
// Dynamic resource example - user profiles by ID
template := mcp.NewResourceTemplate(
    "users://{id}/profile",
    "User Profile",
    mcp.WithTemplateDescription("Returns user profile information"),
    mcp.WithTemplateMIMEType("application/json"),
)

// Add template with its handler
s.AddResourceTemplate(template, func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
    // Extract ID from the URI using regex matching
    // The server automatically matches URIs to templates
    userID := extractIDFromURI(request.Params.URI)
    
    profile, err := getUserProfile(userID)  // Your DB/API call here
    if err != nil {
        return nil, err
    }
    
    return []mcp.ResourceContents{
        mcp.TextResourceContents{
            URI:      request.Params.URI,
            MIMEType: "application/json",
            Text:     profile,
        },
    }, nil
})
```

The examples are simple but demonstrate the core concepts. Resources can be much more sophisticated - serving multiple contents, integrating with databases or external APIs, etc.
</details>

### Tools

<details>
<summary>Show Tool Examples</summary>

Tools let LLMs take actions through your server. Unlike resources, tools are expected to perform computation and have side effects. They're similar to POST endpoints in a REST API.

Simple calculation example:
```go
calculatorTool := mcp.NewTool("calculate",
    mcp.WithDescription("Perform basic arithmetic calculations"),
    mcp.WithString("operation",
        mcp.Required(),
        mcp.Description("The arithmetic operation to perform"),
        mcp.Enum("add", "subtract", "multiply", "divide"),
    ),
    mcp.WithNumber("x",
        mcp.Required(),
        mcp.Description("First number"),
    ),
    mcp.WithNumber("y",
        mcp.Required(),
        mcp.Description("Second number"),
    ),
)

s.AddTool(calculatorTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    args := request.GetArguments()
    op := args["operation"].(string)
    x := args["x"].(float64)
    y := args["y"].(float64)

    var result float64
    switch op {
    case "add":
        result = x + y
    case "subtract":
        result = x - y
    case "multiply":
        result = x * y
    case "divide":
        if y == 0 {
            return mcp.NewToolResultError("cannot divide by zero"), nil
        }
        result = x / y
    }
    
    return mcp.FormatNumberResult(result), nil
})
```

HTTP request example:
```go
httpTool := mcp.NewTool("http_request",
    mcp.WithDescription("Make HTTP requests to external APIs"),
    mcp.WithString("method",
        mcp.Required(),
        mcp.Description("HTTP method to use"),
        mcp.Enum("GET", "POST", "PUT", "DELETE"),
    ),
    mcp.WithString("url",
        mcp.Required(),
        mcp.Description("URL to send the request to"),
        mcp.Pattern("^https?://.*"),
    ),
    mcp.WithString("body",
        mcp.Description("Request body (for POST/PUT)"),
    ),
)

s.AddTool(httpTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    args := request.GetArguments()
    method := args["method"].(string)
    url := args["url"].(string)
    body := ""
    if b, ok := args["body"].(string); ok {
        body = b
    }

    // Create and send request
    var req *http.Request
    var err error
    if body != "" {
        req, err = http.NewRequest(method, url, strings.NewReader(body))
    } else {
        req, err = http.NewRequest(method, url, nil)
    }
    if err != nil {
        return mcp.NewToolResultErrorFromErr("unable to create request", err), nil
    }

    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        return mcp.NewToolResultErrorFromErr("unable to execute request", err), nil
    }
    defer resp.Body.Close()

    // Return response
    respBody, err := io.ReadAll(resp.Body)
    if err != nil {
        return mcp.NewToolResultErrorFromErr("unable to read request response", err), nil
    }

    return mcp.NewToolResultText(fmt.Sprintf("Status: %d\nBody: %s", resp.StatusCode, string(respBody))), nil
})
```

Tools can be used for any kind of computation or side effect:
- Database queries
- File operations  
- External API calls
- Calculations
- System operations

Each tool should:
- Have a clear description
- Validate inputs
- Handle errors gracefully 
- Return structured responses
- Use appropriate result types

</details>

### Prompts

<details>
<summary>Show Prompt Examples</summary>

Prompts are reusable templates that help LLMs interact with your server effectively. They're like "best practices" encoded into your server. Here are some examples:

```go
// Simple greeting prompt
s.AddPrompt(mcp.NewPrompt("greeting",
    mcp.WithPromptDescription("A friendly greeting prompt"),
    mcp.WithArgument("name",
        mcp.ArgumentDescription("Name of the person to greet"),
    ),
), func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
    name := request.Params.Arguments["name"]
    if name == "" {
        name = "friend"
    }
    
    return mcp.NewGetPromptResult(
        "A friendly greeting",
        []mcp.PromptMessage{
            mcp.NewPromptMessage(
                mcp.RoleAssistant,
                mcp.NewTextContent(fmt.Sprintf("Hello, %s! How can I help you today?", name)),
            ),
        },
    ), nil
})

// Code review prompt with embedded resource
s.AddPrompt(mcp.NewPrompt("code_review",
    mcp.WithPromptDescription("Code review assistance"),
    mcp.WithArgument("pr_number",
        mcp.ArgumentDescription("Pull request number to review"),
        mcp.RequiredArgument(),
    ),
), func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
    prNumber := request.Params.Arguments["pr_number"]
    if prNumber == "" {
        return nil, fmt.Errorf("pr_number is required")
    }
    
    return mcp.NewGetPromptResult(
        "Code review assistance",
        []mcp.PromptMessage{
            mcp.NewPromptMessage(
                mcp.RoleUser,
                mcp.NewTextContent("Review the changes and provide constructive feedback."),
            ),
            mcp.NewPromptMessage(
                mcp.RoleAssistant,
                mcp.NewEmbeddedResource(mcp.ResourceContents{
                    URI: fmt.Sprintf("git://pulls/%s/diff", prNumber),
                    MIMEType: "text/x-diff",
                }),
            ),
        },
    ), nil
})

// Database query builder prompt
s.AddPrompt(mcp.NewPrompt("query_builder",
    mcp.WithPromptDescription("SQL query builder assistance"),
    mcp.WithArgument("table",
        mcp.ArgumentDescription("Name of the table to query"),
        mcp.RequiredArgument(),
    ),
), func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
    tableName := request.Params.Arguments["table"]
    if tableName == "" {
        return nil, fmt.Errorf("table name is required")
    }
    
    return mcp.NewGetPromptResult(
        "SQL query builder assistance",
        []mcp.PromptMessage{
            mcp.NewPromptMessage(
                mcp.RoleUser,
                mcp.NewTextContent("Help construct efficient and safe queries for the provided schema."),
            ),
            mcp.NewPromptMessage(
                mcp.RoleUser,
                mcp.NewEmbeddedResource(mcp.ResourceContents{
                    URI: fmt.Sprintf("db://schema/%s", tableName),
                    MIMEType: "application/json",
                }),
            ),
        },
    ), nil
})
```

Prompts can include:
- System instructions
- Required arguments
- Embedded resources
- Multiple messages
- Different content types (text, images, etc.)
- Custom URI schemes

</details>

## Examples

For examples, see the [`examples/`](examples/) directory.

## Extras

### Transports

MCP-Go supports stdio, SSE and streamable-HTTP transport layers.

### Session Management

MCP-Go provides a robust session management system that allows you to:
- Maintain separate state for each connected client
- Register and track client sessions
- Send notifications to specific clients
- Provide per-session tool customization
- Provide per-session prompt customization

<details>
<summary>Show Session Management Examples</summary>

#### Basic Session Handling

```go
// Create a server with session capabilities
s := server.NewMCPServer(
    "Session Demo",
    "1.0.0",
    server.WithToolCapabilities(true),
)

// Implement your own ClientSession
type MySession struct {
    id           string
    notifChannel chan mcp.JSONRPCNotification
    isInitialized bool
    // Add custom fields for your application
}

// Implement the ClientSession interface
func (s *MySession) SessionID() string {
    return s.id
}

func (s *MySession) NotificationChannel() chan<- mcp.JSONRPCNotification {
    return s.notifChannel
}

func (s *MySession) Initialize() {
    s.isInitialized = true
}

func (s *MySession) Initialized() bool {
    return s.isInitialized
}

// Register a session
session := &MySession{
    id:           "user-123",
    notifChannel: make(chan mcp.JSONRPCNotification, 10),
}
if err := s.RegisterSession(context.Background(), session); err != nil {
    log.Printf("Failed to register session: %v", err)
}

// Send notification to a specific client
err := s.SendNotificationToSpecificClient(
    session.SessionID(),
    "notification/update",
    map[string]any{"message": "New data available!"},
)
if err != nil {
    log.Printf("Failed to send notification: %v", err)
}

// Unregister session when done
s.UnregisterSession(context.Background(), session.SessionID())
```

#### Per-Session Tools

For more advanced use cases, you can implement the `SessionWithTools` interface to support per-session tool customization:

```go
// Implement SessionWithTools interface for per-session tools
type MyAdvancedSession struct {
    MySession  // Embed the basic session
    sessionTools map[string]server.ServerTool
}

// Implement additional methods for SessionWithTools
func (s *MyAdvancedSession) GetSessionTools() map[string]server.ServerTool {
    return s.sessionTools
}

func (s *MyAdvancedSession) SetSessionTools(tools map[string]server.ServerTool) {
    s.sessionTools = tools
}

// Create and register a session with tools support
advSession := &MyAdvancedSession{
    MySession: MySession{
        id:           "user-456",
        notifChannel: make(chan mcp.JSONRPCNotification, 10),
    },
    sessionTools: make(map[string]server.ServerTool),
}
if err := s.RegisterSession(context.Background(), advSession); err != nil {
    log.Printf("Failed to register session: %v", err)
}

// Add session-specific tools
userSpecificTool := mcp.NewTool(
    "user_data",
    mcp.WithDescription("Access user-specific data"),
)
// You can use AddSessionTool (similar to AddTool)
err := s.AddSessionTool(
    advSession.SessionID(),
    userSpecificTool,
    func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        // This handler is only available to this specific session
        return mcp.NewToolResultText("User-specific data for " + advSession.SessionID()), nil
    },
)
if err != nil {
    log.Printf("Failed to add session tool: %v", err)
}

// Or use AddSessionTools directly with ServerTool
/*
err := s.AddSessionTools(
    advSession.SessionID(),
    server.ServerTool{
        Tool: userSpecificTool,
        Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
            // This handler is only available to this specific session
            return mcp.NewToolResultText("User-specific data for " + advSession.SessionID()), nil
        },
    },
)
if err != nil {
    log.Printf("Failed to add session tool: %v", err)
}
*/

// Delete session-specific tools when no longer needed
err = s.DeleteSessionTools(advSession.SessionID(), "user_data")
if err != nil {
    log.Printf("Failed to delete session tool: %v", err)
}
```

#### Tool Filtering

You can also apply filters to control which tools are available to certain sessions:

```go
// Add a tool filter that only shows tools with certain prefixes
s := server.NewMCPServer(
    "Tool Filtering Demo",
    "1.0.0",
    server.WithToolCapabilities(true),
    server.WithToolFilter(func(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
        // Get session from context
        session := server.ClientSessionFromContext(ctx)
        if session == nil {
            return tools // Return all tools if no session
        }
        
        // Example: filter tools based on session ID prefix
        if strings.HasPrefix(session.SessionID(), "admin-") {
            // Admin users get all tools
            return tools
        } else {
            // Regular users only get tools with "public-" prefix
            var filteredTools []mcp.Tool
            for _, tool := range tools {
                if strings.HasPrefix(tool.Name, "public-") {
                    filteredTools = append(filteredTools, tool)
                }
            }
            return filteredTools
        }
    }),
)
```

#### Working with Context

The session context is automatically passed to tool and resource handlers:

```go
s.AddTool(mcp.NewTool("session_aware"), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    // Get the current session from context
    session := server.ClientSessionFromContext(ctx)
    if session == nil {
        return mcp.NewToolResultError("No active session"), nil
    }
    
    return mcp.NewToolResultText("Hello, session " + session.SessionID()), nil
})

// When using handlers in HTTP/SSE servers, you need to pass the context with the session
httpHandler := func(w http.ResponseWriter, r *http.Request) {
    // Get session from somewhere (like a cookie or header)
    session := getSessionFromRequest(r)
    
    // Add session to context
    ctx := s.WithContext(r.Context(), session)
    
    // Use this context when handling requests
    // ...
}
```

</details>

### Request Hooks

Hook into the request lifecycle by creating a `Hooks` object with your
selection among the possible callbacks.  This enables telemetry across all
functionality, and observability of various facts, for example the ability
to count improperly-formatted requests, or to log the agent identity during
initialization.

Add the `Hooks` to the server at the time of creation using the
`server.WithHooks` option.

### Tool Handler Middleware

Add middleware to tool call handlers using the `server.WithToolHandlerMiddleware` option. Middlewares can be registered on server creation and are applied on every tool call.

A recovery middleware option is available to recover from panics in a tool call and can be added to the server with the `server.WithRecovery` option.

### Regenerating Server Code

Server hooks and request handlers are generated. Regenerate them by running:

```bash
go generate ./...
```

You need `go` installed and the `goimports` tool available. The generator runs
`goimports` automatically to format and fix imports.

