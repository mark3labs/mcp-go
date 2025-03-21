<!-- omit in toc -->
# MCP Go 🚀
[![Build](https://github.com/mark3labs/mcp-go/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/mark3labs/mcp-go/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/mark3labs/mcp-go?cache)](https://goreportcard.com/report/github.com/mark3labs/mcp-go)
[![GoDoc](https://pkg.go.dev/badge/github.com/mark3labs/mcp-go.svg)](https://pkg.go.dev/github.com/mark3labs/mcp-go)

<div align="center">

<strong>A Go implementation of the Model Context Protocol (MCP), enabling seamless integration between LLM applications and external data sources and tools.</strong>

<br>

[![Tutorial](http://img.youtube.com/vi/qoaeYMrXJH0/0.jpg)](http://www.youtube.com/watch?v=qoaeYMrXJH0 "Tutorial")

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
    // Create MCP server
    s := server.NewMCPServer(
        "Demo 🚀",
        "1.0.0",
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
    name, ok := request.Params.Arguments["name"].(string)
    if !ok {
        return nil, errors.New("name must be a string")
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
- [Contributing](#contributing)
  - [Prerequisites](#prerequisites)
  - [Installation](#installation-1)
  - [Testing](#testing)
  - [Opening a Pull Request](#opening-a-pull-request)

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
    "errors"
    "fmt"

    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"
)

func main() {
    // Create a new MCP server
    s := server.NewMCPServer(
        "Calculator Demo",
        "1.0.0",
        server.WithResourceCapabilities(true, true),
        server.WithLogging(),
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
        op := request.Params.Arguments["operation"].(string)
        x := request.Params.Arguments["x"].(float64)
        y := request.Params.Arguments["y"].(float64)

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
                return nil, errors.New("Cannot divide by zero")
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
    op := request.Params.Arguments["operation"].(string)
    x := request.Params.Arguments["x"].(float64)
    y := request.Params.Arguments["y"].(float64)

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
            return nil, errors.New("Division by zero is not allowed")
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
    method := request.Params.Arguments["method"].(string)
    url := request.Params.Arguments["url"].(string)
    body := ""
    if b, ok := request.Params.Arguments["body"].(string); ok {
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
        return nil, fmt.Errorf("Failed to create request: %v", err)
    }

    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("Request failed: %v", err)
    }
    defer resp.Body.Close()

    // Return response
    respBody, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("Failed to read response: %v", err)
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
                mcp.RoleSystem,
                mcp.NewTextContent("You are a helpful code reviewer. Review the changes and provide constructive feedback."),
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
                mcp.RoleSystem,
                mcp.NewTextContent("You are a SQL expert. Help construct efficient and safe queries."),
            ),
            mcp.NewPromptMessage(
                mcp.RoleAssistant,
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

For examples, see the `examples/` directory.

## Extras

### Request Hooks

Hook into the request lifecycle by creating a `Hooks` object with your
selection among the possible callbacks.  This enables telemetry across all
functionality, and observability of various facts, for example the ability
to count improperly-formatted requests, or to log the agent identity during
initialization.

Add the `Hooks` to the server at the time of creation using the
`server.WithHooks` option.

## Contributing

<details>

<summary><h3>Open Developer Guide</h3></summary>

### Prerequisites

Go version >= 1.23

### Installation

Create a fork of this repository, then clone it:

```bash
git clone https://github.com/mark3labs/mcp-go.git
cd mcp-go
```

### Testing

Please make sure to test any new functionality. Your tests should be simple and atomic and anticipate change rather than cement complex patterns.

Run tests from the root directory:

```bash
go test -v './...'
```

### Opening a Pull Request

Fork the repository and create a new branch:

```bash
git checkout -b my-branch
```

Make your changes and commit them:


```bash
git add . && git commit -m "My changes"
```

Push your changes to your fork:


```bash
git push origin my-branch
```

Feel free to reach out in a GitHub issue or discussion if you have any questions!

</details>
