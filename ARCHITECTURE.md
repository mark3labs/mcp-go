# MCP-Go Architecture Documentation

## Overview

MCP-Go is a comprehensive Go implementation of the Model Context Protocol (MCP), designed to enable seamless integration between LLM applications and external data sources and tools. This document provides a detailed architectural overview of the codebase, including component relationships, data flow, and design patterns.

## Table of Contents

- [Architecture Overview](#architecture-overview)
- [Core Components](#core-components)
- [Transport Layer](#transport-layer)
- [Session Management](#session-management)
- [Protocol Implementation](#protocol-implementation)
- [Data Flow](#data-flow)
- [Design Patterns](#design-patterns)
- [Extension Points](#extension-points)

## Architecture Overview

The MCP-Go implementation follows a layered architecture with clear separation of concerns:

```mermaid
graph TB
    subgraph "Application Layer"
        A[Client Applications]
        B[Server Applications]
    end
    
    subgraph "MCP-Go SDK"
        subgraph "Client Package"
            C[MCPClient Interface]
            D[Client Implementation]
            E[Transport Layer]
        end
        
        subgraph "Server Package"
            F[MCPServer]
            G[Request Handlers]
            H[Session Management]
        end
        
        subgraph "Core Package"
            I[MCP Types]
            J[Protocol Definitions]
            K[Utilities]
        end
    end
    
    subgraph "Transport Implementations"
        L[Stdio Transport]
        M[HTTP Transport]
        N[SSE Transport]
        O[In-Process Transport]
    end
    
    A --> C
    B --> F
    C --> D
    D --> E
    E --> L
    E --> M
    E --> N
    E --> O
    F --> G
    F --> H
    G --> I
    H --> I
    I --> J
    I --> K
```

## Core Components

### 1. Client Package (`client/`)

The client package provides a unified interface for connecting to MCP servers across different transport mechanisms.

#### Key Components:

- **MCPClient Interface**: Defines the contract for all client implementations
- **Client Implementation**: Concrete implementation handling protocol communication
- **Transport Layer**: Abstracted transport mechanisms (stdio, HTTP, SSE, in-process)

```mermaid
classDiagram
    class MCPClient {
        <<interface>>
        +Initialize(ctx, request) InitializeResult
        +Ping(ctx) error
        +ListResources(ctx, request) ListResourcesResult
        +ReadResource(ctx, request) ReadResourceResult
        +ListTools(ctx, request) ListToolsResult
        +CallTool(ctx, request) CallToolResult
        +ListPrompts(ctx, request) ListPromptsResult
        +GetPrompt(ctx, request) GetPromptResult
        +Close() error
        +OnNotification(handler) void
    }
    
    class Client {
        -transport Transport
        -notificationHandlers []NotificationHandler
        +Initialize(ctx, request) InitializeResult
        +Start(ctx) error
        +Close() error
    }
    
    class Transport {
        <<interface>>
        +Send(request) Response
        +Receive() Message
        +Close() error
    }
    
    class StdioTransport {
        -command string
        -args []string
        +Send(request) Response
        +Receive() Message
    }
    
    class HTTPTransport {
        -url string
        -client *http.Client
        +Send(request) Response
        +Receive() Message
    }
    
    MCPClient <|.. Client
    Transport <|.. StdioTransport
    Transport <|.. HTTPTransport
    Client --> Transport
```

### 2. Server Package (`server/`)

The server package implements the MCP server with comprehensive capabilities for handling resources, tools, and prompts.

#### Key Components:

- **MCPServer**: Main server implementation
- **Request Handlers**: Protocol-specific request processing
- **Session Management**: Client session tracking and management
- **Capabilities Management**: Feature capability negotiation

```mermaid
classDiagram
    class MCPServer {
        -name string
        -version string
        -resources map[string]resourceEntry
        -resourceTemplates map[string]resourceTemplateEntry
        -tools map[string]ServerTool
        -prompts map[string]mcp.Prompt
        -sessions sync.Map
        -capabilities serverCapabilities
        +AddResource(resource, handler) void
        +AddTool(tool, handler) void
        +AddPrompt(prompt, handler) void
        +RegisterSession(ctx, session) error
        +SendNotificationToClient(ctx, method, params) error
    }
    
    class ServerTool {
        +Tool mcp.Tool
        +Handler ToolHandlerFunc
    }
    
    class ServerPrompt {
        +Prompt mcp.Prompt
        +Handler PromptHandlerFunc
    }
    
    class ServerResource {
        +Resource mcp.Resource
        +Handler ResourceHandlerFunc
    }
    
    class ClientSession {
        <<interface>>
        +SessionID() string
        +NotificationChannel() chan<- JSONRPCNotification
        +Initialize() void
        +Initialized() bool
    }
    
    class SessionWithTools {
        <<interface>>
        +GetSessionTools() map[string]ServerTool
        +SetSessionTools(tools) void
    }
    
    MCPServer --> ServerTool
    MCPServer --> ServerPrompt
    MCPServer --> ServerResource
    MCPServer --> ClientSession
    ClientSession <|.. SessionWithTools
```

### 3. MCP Core Package (`mcp/`)

The core package contains the fundamental types, constants, and utilities for the MCP protocol implementation.

#### Key Components:

- **Types**: Core data structures for requests, responses, and protocol elements
- **Constants**: Protocol version, error codes, and method definitions
- **Utilities**: Helper functions for common operations

```mermaid
classDiagram
    class InitializeRequest {
        +Params InitializeParams
    }
    
    class InitializeParams {
        +ProtocolVersion string
        +ClientInfo Implementation
        +Capabilities ClientCapabilities
    }
    
    class InitializeResult {
        +ProtocolVersion string
        +ServerInfo Implementation
        +Capabilities ServerCapabilities
        +Instructions string
    }
    
    class Tool {
        +Name string
        +Description string
        +InputSchema ToolInputSchema
    }
    
    class Resource {
        +URI string
        +Name string
        +Description string
        +MIMEType string
    }
    
    class Prompt {
        +Name string
        +Description string
        +Arguments map[string]PromptArgument
    }
    
    class CallToolRequest {
        +Params CallToolParams
    }
    
    class CallToolParams {
        +Name string
        +Arguments map[string]any
        +Meta RequestMeta
    }
    
    class CallToolResult {
        +Content []Content
        +IsError bool
    }
    
    InitializeRequest --> InitializeParams
    InitializeResult --> Implementation
    InitializeResult --> ServerCapabilities
    CallToolRequest --> CallToolParams
    CallToolResult --> Content
```

## Transport Layer

MCP-Go supports multiple transport mechanisms, each optimized for different use cases:

### Transport Architecture

```mermaid
graph LR
    subgraph "Transport Layer"
        A[Transport Interface]
        B[Stdio Transport]
        C[HTTP Transport]
        D[SSE Transport]
        E[In-Process Transport]
    end
    
    subgraph "Use Cases"
        F[Command Line Tools]
        G[Web Applications]
        H[Real-time Apps]
        I[Testing/Embedding]
    end
    
    A --> B
    A --> C
    A --> D
    A --> E
    
    B --> F
    C --> G
    D --> H
    E --> I
```

### Transport Implementations

1. **Stdio Transport**: Process-based communication via stdin/stdout
2. **HTTP Transport**: RESTful API over HTTP/HTTPS
3. **SSE Transport**: Server-Sent Events for real-time communication
4. **In-Process Transport**: Direct function calls for embedded scenarios

## Session Management

The server implements sophisticated session management for multi-client scenarios:

### Session Architecture

```mermaid
sequenceDiagram
    participant C as Client
    participant S as MCPServer
    participant SM as SessionManager
    participant T as Tools
    
    C->>S: Initialize Request
    S->>SM: Register Session
    SM->>SM: Create Session Context
    S->>C: Initialize Response
    
    C->>S: List Tools Request
    S->>SM: Get Session Context
    SM->>S: Session Info
    S->>T: Apply Session Filters
    T->>S: Filtered Tools
    S->>C: List Tools Response
    
    C->>S: Call Tool Request
    S->>SM: Get Session Context
    SM->>S: Session Info
    S->>T: Execute Tool (with session context)
    T->>S: Tool Result
    S->>C: Call Tool Response
    
    Note over S: Session-specific tools and notifications
    S->>SM: Send Notification
    SM->>C: Session-specific Notification
```

### Session Features

- **Per-Session State**: Isolated state for each client connection
- **Session-Specific Tools**: Dynamic tool registration per session
- **Tool Filtering**: Context-aware tool availability
- **Notification Routing**: Targeted notifications to specific sessions

## Protocol Implementation

The MCP protocol implementation follows the JSON-RPC 2.0 specification with MCP-specific extensions:

### Protocol Flow

```mermaid
sequenceDiagram
    participant Client
    participant Server
    participant Handler
    participant Resource
    
    Client->>Server: Initialize Request
    Server->>Server: Negotiate Protocol Version
    Server->>Client: Initialize Response
    
    Client->>Server: List Resources Request
    Server->>Handler: Process Request
    Handler->>Resource: Query Resources
    Resource->>Handler: Resource List
    Handler->>Server: Response Data
    Server->>Client: List Resources Response
    
    Client->>Server: Read Resource Request
    Server->>Handler: Process Request
    Handler->>Resource: Read Resource
    Resource->>Handler: Resource Content
    Handler->>Server: Response Data
    Server->>Client: Read Resource Response
    
    Client->>Server: Call Tool Request
    Server->>Handler: Process Request
    Handler->>Handler: Execute Tool Logic
    Handler->>Server: Tool Result
    Server->>Client: Call Tool Response
```

### Request Processing Pipeline

```mermaid
graph TD
    A[Incoming Request] --> B[Parse JSON-RPC]
    B --> C[Validate Request]
    C --> D[Route to Handler]
    D --> E[Execute Handler]
    E --> F[Apply Middleware]
    F --> G[Format Response]
    G --> H[Send Response]
    
    C --> I[Invalid Request]
    I --> J[Error Response]
    J --> H
    
    E --> K[Handler Error]
    K --> L[Error Response]
    L --> H
```

## Data Flow

### Client-Server Communication

```mermaid
graph TB
    subgraph "Client Side"
        A[Application Code]
        B[MCPClient Interface]
        C[Transport Layer]
    end
    
    subgraph "Network/Transport"
        D[Stdio/HTTP/SSE]
    end
    
    subgraph "Server Side"
        E[Transport Layer]
        F[Request Router]
        G[MCPServer]
        H[Handlers]
        I[Resources/Tools/Prompts]
    end
    
    A --> B
    B --> C
    C --> D
    D --> E
    E --> F
    F --> G
    G --> H
    H --> I
    I --> H
    H --> G
    G --> F
    F --> E
    E --> D
    D --> C
    C --> B
    B --> A
```

### Resource Access Flow

```mermaid
sequenceDiagram
    participant C as Client
    participant S as Server
    participant R as Resource Handler
    participant D as Data Source
    
    C->>S: Read Resource Request
    Note over S: Validate URI and permissions
    S->>R: Route to Handler
    R->>D: Query Data Source
    D->>R: Return Data
    R->>R: Format as Resource Content
    R->>S: Resource Contents
    S->>C: Read Resource Response
```

## Design Patterns

### 1. Strategy Pattern - Transport Layer

Different transport mechanisms are implemented as strategies:

```mermaid
classDiagram
    class TransportStrategy {
        <<interface>>
        +Connect() error
        +Send(message) error
        +Receive() Message
        +Close() error
    }
    
    class StdioStrategy {
        +Connect() error
        +Send(message) error
        +Receive() Message
        +Close() error
    }
    
    class HTTPStrategy {
        +Connect() error
        +Send(message) error
        +Receive() Message
        +Close() error
    }
    
    class SSEStrategy {
        +Connect() error
        +Send(message) error
        +Receive() Message
        +Close() error
    }
    
    TransportStrategy <|.. StdioStrategy
    TransportStrategy <|.. HTTPStrategy
    TransportStrategy <|.. SSEStrategy
```

### 2. Observer Pattern - Notifications

The notification system uses the observer pattern for event handling:

```mermaid
classDiagram
    class NotificationSubject {
        +Attach(observer) void
        +Detach(observer) void
        +Notify(event) void
    }
    
    class NotificationObserver {
        <<interface>>
        +Update(event) void
    }
    
    class ClientNotificationHandler {
        +Update(event) void
    }
    
    NotificationSubject --> NotificationObserver
    NotificationObserver <|.. ClientNotificationHandler
```

### 3. Middleware Pattern - Request Processing

Request processing uses middleware for cross-cutting concerns:

```mermaid
graph LR
    A[Request] --> B[Recovery Middleware]
    B --> C[Logging Middleware]
    C --> D[Authentication Middleware]
    D --> E[Handler]
    E --> F[Response]
    F --> G[Logging Middleware]
    G --> H[Recovery Middleware]
    H --> I[Client]
```

### 4. Factory Pattern - Resource Creation

Resources, tools, and prompts are created using builder patterns:

```mermaid
classDiagram
    class ToolBuilder {
        +WithDescription(desc) ToolBuilder
        +WithString(name, opts) ToolBuilder
        +WithNumber(name, opts) ToolBuilder
        +WithBoolean(name, opts) ToolBuilder
        +Build() Tool
    }
    
    class ResourceBuilder {
        +WithDescription(desc) ResourceBuilder
        +WithMIMEType(mime) ResourceBuilder
        +Build() Resource
    }
    
    class PromptBuilder {
        +WithDescription(desc) PromptBuilder
        +WithArgument(name, opts) PromptBuilder
        +Build() Prompt
    }
```

## Extension Points

### 1. Custom Transport Implementations

Developers can implement custom transport mechanisms by implementing the `Transport` interface:

```go
type CustomTransport struct {
    // Custom implementation
}

func (t *CustomTransport) Send(request JSONRPCMessage) error {
    // Custom send logic
}

func (t *CustomTransport) Receive() (JSONRPCMessage, error) {
    // Custom receive logic
}
```

### 2. Custom Session Types

Sessions can be extended with custom functionality:

```go
type CustomSession struct {
    // Embed base session
    *BaseSession
    // Custom fields
    UserID string
    Permissions []string
}

func (s *CustomSession) GetSessionTools() map[string]ServerTool {
    // Custom tool filtering logic
}
```

### 3. Middleware Extensions

Custom middleware can be added for cross-cutting concerns:

```go
func CustomMiddleware(next ToolHandlerFunc) ToolHandlerFunc {
    return func(ctx context.Context, request CallToolRequest) (*CallToolResult, error) {
        // Pre-processing
        result, err := next(ctx, request)
        // Post-processing
        return result, err
    }
}
```

### 4. Custom Handlers

Resource, tool, and prompt handlers can be customized:

```go
func CustomResourceHandler(ctx context.Context, request ReadResourceRequest) ([]ResourceContents, error) {
    // Custom resource handling logic
}
```

## Security Considerations

### 1. Input Validation

All inputs are validated according to JSON Schema specifications:

```mermaid
graph TD
    A[Incoming Request] --> B[Schema Validation]
    B --> C{Valid?}
    C -->|Yes| D[Process Request]
    C -->|No| E[Return Validation Error]
    D --> F[Execute Handler]
    F --> G[Return Response]
```

### 2. Session Isolation

Sessions are isolated to prevent cross-session data leakage:

```mermaid
graph TB
    subgraph "Session A"
        A1[Client A]
        A2[Session Context A]
        A3[Tools A]
        A4[Resources A]
    end
    
    subgraph "Session B"
        B1[Client B]
        B2[Session Context B]
        B3[Tools B]
        B4[Resources B]
    end
    
    A1 --> A2
    A2 --> A3
    A2 --> A4
    
    B1 --> B2
    B2 --> B3
    B2 --> B4
    
    A2 -.->|Isolated| B2
```

## Performance Considerations

### 1. Concurrent Request Handling

The server handles multiple concurrent requests efficiently:

```mermaid
graph TD
    A[Request Pool] --> B[Worker 1]
    A --> C[Worker 2]
    A --> D[Worker 3]
    A --> E[Worker N]
    
    B --> F[Handler 1]
    C --> G[Handler 2]
    D --> H[Handler 3]
    E --> I[Handler N]
```

### 2. Resource Caching

Resources can be cached for improved performance:

```mermaid
graph LR
    A[Resource Request] --> B{Cache Hit?}
    B -->|Yes| C[Return Cached]
    B -->|No| D[Load from Source]
    D --> E[Cache Result]
    E --> F[Return Data]
```

## Testing Strategy

### 1. Unit Testing

Each component is thoroughly unit tested:

```mermaid
graph TD
    A[Unit Tests] --> B[Client Tests]
    A --> C[Server Tests]
    A --> D[Transport Tests]
    A --> E[Protocol Tests]
    
    B --> F[Interface Tests]
    C --> G[Handler Tests]
    D --> H[Connection Tests]
    E --> I[Message Tests]
```

### 2. Integration Testing

End-to-end integration tests verify complete workflows:

```mermaid
sequenceDiagram
    participant T as Test Suite
    participant C as Test Client
    participant S as Test Server
    
    T->>C: Start Client
    T->>S: Start Server
    C->>S: Initialize
    S->>C: Initialize Response
    C->>S: List Tools
    S->>C: Tools Response
    C->>S: Call Tool
    S->>C: Tool Response
    T->>C: Verify Results
    T->>S: Shutdown
```

## Conclusion

MCP-Go provides a robust, extensible, and well-architected implementation of the Model Context Protocol. The layered architecture, comprehensive transport support, sophisticated session management, and extensive customization options make it suitable for a wide range of LLM integration scenarios.

The design emphasizes:
- **Modularity**: Clear separation of concerns across packages
- **Extensibility**: Multiple extension points for customization
- **Performance**: Efficient concurrent request handling
- **Reliability**: Comprehensive error handling and recovery
- **Security**: Input validation and session isolation
- **Testability**: Extensive test coverage and mocking support

This architecture enables developers to build sophisticated MCP servers and clients while maintaining clean, maintainable code and following Go best practices.