# Go MCP SDK Sampling Implementation Specification

## Overview

Implement MCP sampling capabilities in the existing Go SDK at `github.com/mark3labs/mcp-go`. This feature allows MCP servers to request LLM completions from clients, enabling sophisticated agentic workflows while maintaining security and user control.

## Implementation Requirements

### Phase 1: Core Types and Protocol Support

#### 1.1 Add Sampling Types to `mcp/types.go`

Add the following types to the existing `mcp/types.go` file:

```go
// Sampling message types
type SamplingMessage struct {
    Role    Role    `json:"role"`
    Content Content `json:"content"` // Can be TextContent, ImageContent, or AudioContent
}

// Model preferences for sampling
type ModelPreferences struct {
    Hints              []ModelHint `json:"hints,omitempty"`
    CostPriority       *float64    `json:"costPriority,omitempty"`
    SpeedPriority      *float64    `json:"speedPriority,omitempty"`
    IntelligencePriority *float64  `json:"intelligencePriority,omitempty"`
}

type ModelHint struct {
    Name string `json:"name,omitempty"`
}

// Context inclusion options
type ContextInclusion string
const (
    ContextNone       ContextInclusion = "none"
    ContextThisServer ContextInclusion = "thisServer"
    ContextAllServers ContextInclusion = "allServers"
)

// Sampling request (sent from server to client)
type CreateMessageRequest struct {
    Request
    Params CreateMessageParams `json:"params"`
}

type CreateMessageParams struct {
    Messages         []SamplingMessage `json:"messages"`
    ModelPreferences *ModelPreferences `json:"modelPreferences,omitempty"`
    SystemPrompt     *string           `json:"systemPrompt,omitempty"`
    IncludeContext   *ContextInclusion `json:"includeContext,omitempty"`
    Temperature      *float64          `json:"temperature,omitempty"`
    MaxTokens        *int              `json:"maxTokens,omitempty"`
    StopSequences    []string          `json:"stopSequences,omitempty"`
    Metadata         map[string]any    `json:"metadata,omitempty"`
}

// Sampling response (sent from client to server)
type CreateMessageResult struct {
    Result
    Role       Role    `json:"role"`
    Content    Content `json:"content"`
    Model      string  `json:"model"`
    StopReason *string `json:"stopReason,omitempty"`
}
```

#### 1.2 Add Sampling Method Constants

Add to the existing method constants in `mcp/types.go`:

```go
const (
    // ... existing methods ...
    
    // MethodSamplingCreateMessage allows servers to request LLM completions from clients
    // https://modelcontextprotocol.io/docs/concepts/sampling
    MethodSamplingCreateMessage MCPMethod = "sampling/createMessage"
)
```

#### 1.3 Update Server and Client Capabilities

Modify the existing capability types in `mcp/types.go`:

```go
// Update ClientCapabilities
type ClientCapabilities struct {
    // ... existing fields ...
    
    // Present if the client supports sampling from an LLM
    Sampling *struct{} `json:"sampling,omitempty"`
}

// Update ServerCapabilities  
type ServerCapabilities struct {
    // ... existing fields ...
    
    // Present if the server supports requesting sampling from clients
    Sampling *struct{} `json:"sampling,omitempty"`
}
```

### Phase 2: Server-Side Sampling Context API

#### 2.1 Create Sampling Context Interface

Create a new file `server/sampling.go`:

```go
package server

import (
    "context"
    "errors"
    "fmt"
    
    "github.com/mark3labs/mcp-go/mcp"
)

// SamplingContext provides sampling capabilities to server tools
type SamplingContext interface {
    // Sample requests LLM completion with flexible input types
    Sample(ctx context.Context, input SampleInput, opts ...SampleOption) (*SampleResult, error)
    
    // SampleWithParams provides direct parameter control
    SampleWithParams(ctx context.Context, req *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error)
}

// SampleInput represents flexible input types for sampling
type SampleInput interface {
    toSamplingRequest() (*mcp.CreateMessageRequest, error)
}

// SampleResult wraps the sampling response for easier access
type SampleResult struct {
    response *mcp.CreateMessageResult
}

// Text returns the text content from the sampling result
func (r *SampleResult) Text() string {
    if textContent, ok := mcp.AsTextContent(r.response.Content); ok {
        return textContent.Text
    }
    return ""
}

// Content returns the raw content from the sampling result
func (r *SampleResult) Content() mcp.Content {
    return r.response.Content
}

// Model returns the model name used for sampling
func (r *SampleResult) Model() string {
    return r.response.Model
}

// StopReason returns the reason sampling stopped
func (r *SampleResult) StopReason() string {
    if r.response.StopReason != nil {
        return *r.response.StopReason
    }
    return ""
}

// Response returns the raw CreateMessageResult
func (r *SampleResult) Response() *mcp.CreateMessageResult {
    return r.response
}

// SampleOption configures sampling requests
type SampleOption func(*mcp.CreateMessageParams)

// Common sample options
func WithTemperature(temp float64) SampleOption {
    return func(p *mcp.CreateMessageParams) {
        p.Temperature = &temp
    }
}

func WithMaxTokens(max int) SampleOption {
    return func(p *mcp.CreateMessageParams) {
        p.MaxTokens = &max
    }
}

func WithModel(hints ...string) SampleOption {
    return func(p *mcp.CreateMessageParams) {
        if p.ModelPreferences == nil {
            p.ModelPreferences = &mcp.ModelPreferences{}
        }
        for _, hint := range hints {
            p.ModelPreferences.Hints = append(p.ModelPreferences.Hints, mcp.ModelHint{Name: hint})
        }
    }
}

func WithSystemPrompt(prompt string) SampleOption {
    return func(p *mcp.CreateMessageParams) {
        p.SystemPrompt = &prompt
    }
}

func WithContext(include mcp.ContextInclusion) SampleOption {
    return func(p *mcp.CreateMessageParams) {
        p.IncludeContext = &include
    }
}

func WithStopSequences(sequences ...string) SampleOption {
    return func(p *mcp.CreateMessageParams) {
        p.StopSequences = sequences
    }
}

// Input type implementations
type StringInput string

func (s StringInput) toSamplingRequest() (*mcp.CreateMessageRequest, error) {
    return &mcp.CreateMessageRequest{
        Params: mcp.CreateMessageParams{
            Messages: []mcp.SamplingMessage{
                {
                    Role:    mcp.RoleUser,
                    Content: mcp.NewTextContent(string(s)),
                },
            },
        },
    }, nil
}

type MessagesInput []mcp.SamplingMessage

func (m MessagesInput) toSamplingRequest() (*mcp.CreateMessageRequest, error) {
    return &mcp.CreateMessageRequest{
        Params: mcp.CreateMessageParams{
            Messages: []mcp.SamplingMessage(m),
        },
    }, nil
}

type RequestInput struct {
    *mcp.CreateMessageRequest
}

func (r RequestInput) toSamplingRequest() (*mcp.CreateMessageRequest, error) {
    return r.CreateMessageRequest, nil
}

// Errors
var (
    ErrSamplingNotSupported = errors.New("sampling not supported by client")
    ErrSamplingNotEnabled   = errors.New("sampling capability not enabled on server")
    ErrSamplingTimeout      = errors.New("sampling request timed out")
    ErrSamplingRejected     = errors.New("sampling request rejected by client")
)
```

#### 2.2 Implement Sampling Context

Continue in `server/sampling.go`:

```go
// samplingContext implements SamplingContext
type samplingContext struct {
    session ClientSession
    server  *MCPServer
}

func (sc *samplingContext) Sample(ctx context.Context, input SampleInput, opts ...SampleOption) (*SampleResult, error) {
    req, err := input.toSamplingRequest()
    if err != nil {
        return nil, fmt.Errorf("failed to convert input to sampling request: %w", err)
    }
    
    // Apply options
    for _, opt := range opts {
        opt(&req.Params)
    }
    
    result, err := sc.SampleWithParams(ctx, req)
    if err != nil {
        return nil, err
    }
    
    return &SampleResult{response: result}, nil
}

func (sc *samplingContext) SampleWithParams(ctx context.Context, req *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
    // Check if client supports sampling
    if !sc.supportsClientSampling() {
        return nil, ErrSamplingNotSupported
    }
    
    // Check if server has sampling capability enabled
    if !sc.server.hasSamplingCapability() {
        return nil, ErrSamplingNotEnabled
    }
    
    // Send sampling request to client (this will be implemented in Phase 3)
    return sc.sendSamplingRequest(ctx, req)
}

func (sc *samplingContext) supportsClientSampling() bool {
    // This would check the client's capabilities during initialization
    // Implementation depends on session capability tracking
    return true // Placeholder
}

func (sc *samplingContext) sendSamplingRequest(ctx context.Context, req *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
    // This will be implemented when we add client-to-server request support
    return nil, fmt.Errorf("sampling request mechanism not yet implemented")
}
```

#### 2.3 Update Server to Support Sampling Context

Modify `server/server.go` to add sampling capabilities:

```go
// Add to serverCapabilities struct
type serverCapabilities struct {
    // ... existing fields ...
    sampling *bool
}

// Add server option
func WithSampling() ServerOption {
    return func(s *MCPServer) {
        s.capabilities.sampling = mcp.ToBoolPtr(true)
    }
}

// Add method to check sampling capability
func (s *MCPServer) hasSamplingCapability() bool {
    return s.capabilities.sampling != nil && *s.capabilities.sampling
}

// Add context key for sampling context
type samplingContextKey struct{}

// Add method to get sampling context from context
func SamplingContextFromContext(ctx context.Context) SamplingContext {
    if sc, ok := ctx.Value(samplingContextKey{}).(SamplingContext); ok {
        return sc
    }
    return nil
}

// Modify WithContext method to include sampling context
func (s *MCPServer) WithContext(ctx context.Context, session ClientSession) context.Context {
    ctx = context.WithValue(ctx, clientSessionKey{}, session)
    
    // Add sampling context if capability is enabled
    if s.hasSamplingCapability() {
        samplingCtx := &samplingContext{
            session: session,
            server:  s,
        }
        ctx = context.WithValue(ctx, samplingContextKey{}, samplingCtx)
    }
    
    return ctx
}
```

### Phase 3: Client-Side Sampling Handler Support

#### 3.1 Add Sampling Handler Interface to Client

Create new file `client/sampling.go`:

```go
package client

import (
    "context"
    "encoding/json"
    "fmt"
    
    "github.com/mark3labs/mcp-go/mcp"
)

// SamplingHandler processes sampling requests from servers
type SamplingHandler interface {
    HandleSampling(ctx context.Context, req *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error)
}

// SamplingHandlerFunc is a functional adapter for SamplingHandler
type SamplingHandlerFunc func(context.Context, *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error)

func (f SamplingHandlerFunc) HandleSampling(ctx context.Context, req *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
    return f(ctx, req)
}

// SimpleSamplingHandler provides a simple string-based sampling interface
type SimpleSamplingHandler func(messages []mcp.SamplingMessage) (string, error)

// ToSamplingHandler converts a SimpleSamplingHandler to a SamplingHandler
func (s SimpleSamplingHandler) ToSamplingHandler() SamplingHandler {
    return SamplingHandlerFunc(func(ctx context.Context, req *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
        response, err := s(req.Params.Messages)
        if err != nil {
            return nil, err
        }
        
        return &mcp.CreateMessageResult{
            Role:    mcp.RoleAssistant,
            Content: mcp.NewTextContent(response),
            Model:   "unknown", // Client should set appropriate model name
        }, nil
    })
}

// Sampling errors
type SamplingError struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
    Data    any    `json:"data,omitempty"`
}

func (e *SamplingError) Error() string {
    return e.Message
}

// Error codes for sampling
const (
    ErrCodeSamplingNotSupported = -32001
    ErrCodeSamplingFailed       = -32002
    ErrCodeSamplingRejected     = -32003
    ErrCodeSamplingTimeout      = -32004
)

func NewSamplingError(code int, message string) *SamplingError {
    return &SamplingError{Code: code, Message: message}
}
```

#### 3.2 Update Client to Support Sampling

Modify `client/client.go`:

```go
// Add sampling handler field to Client struct
type Client struct {
    // ... existing fields ...
    samplingHandler SamplingHandler
}

// Add client option for sampling
func WithSamplingHandler(handler SamplingHandler) ClientOption {
    return func(c *Client) {
        c.samplingHandler = handler
        // Enable sampling capability
        if c.clientCapabilities.Sampling == nil {
            c.clientCapabilities.Sampling = &struct{}{}
        }
    }
}

// Convenience function for simple handlers
func WithSimpleSamplingHandler(handler SimpleSamplingHandler) ClientOption {
    return WithSamplingHandler(handler.ToSamplingHandler())
}

// Add method to handle incoming sampling requests
func (c *Client) handleSamplingRequest(ctx context.Context, request *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
    if c.samplingHandler == nil {
        return nil, NewSamplingError(ErrCodeSamplingNotSupported, "sampling not supported")
    }
    
    return c.samplingHandler.HandleSampling(ctx, request)
}
```

### Phase 4: Request Handler Updates

#### 4.1 Update Request Handler Generation

The existing code generation in `server/internal/gen/` needs to be updated to handle server-to-client requests. This is a significant architectural change.

For the initial implementation, modify `server/request_handler.go` to add sampling method handling:

```go
// Add to the switch statement in HandleMessage
case mcp.MethodSamplingCreateMessage:
    // This case handles sampling requests FROM clients TO servers
    // (This would be unusual - sampling typically goes server->client)
    var request mcp.CreateMessageRequest
    var result *mcp.CreateMessageResult
    if unmarshalErr := json.Unmarshal(message, &request); unmarshalErr != nil {
        err = &requestError{
            id:   baseMessage.ID,
            code: mcp.INVALID_REQUEST,
            err:  &UnparsableMessageError{message: message, err: unmarshalErr, method: baseMessage.Method},
        }
    } else {
        result, err = s.handleSamplingCreateMessage(ctx, baseMessage.ID, request)
    }
    if err != nil {
        s.hooks.onError(ctx, baseMessage.ID, baseMessage.Method, &request, err)
        return err.ToJSONRPCError()
    }
    return createResponse(baseMessage.ID, *result)
```

Add the handler method to `server/server.go`:

```go
func (s *MCPServer) handleSamplingCreateMessage(
    ctx context.Context,
    id any,
    request mcp.CreateMessageRequest,
) (*mcp.CreateMessageResult, *requestError) {
    // This would typically not be implemented on the server side
    // Sampling requests usually go FROM server TO client
    return nil, &requestError{
        id:   id,
        code: mcp.METHOD_NOT_FOUND,
        err:  fmt.Errorf("sampling requests should be sent from server to client"),
    }
}
```

### Phase 5: Transport Layer Updates

#### 5.1 Enable Server-to-Client Requests

This is the most complex part. The current SDK only supports client-to-server requests. For sampling, we need bidirectional communication.

For each transport (`client/transport/*.go`), add support for handling incoming requests from servers:

**For SSE Transport (`client/transport/sse.go`):**

```go
// Add to SSE struct
type SSE struct {
    // ... existing fields ...
    requestHandler func(context.Context, mcp.JSONRPCRequest) (any, error)
}

// Add method to set request handler
func (c *SSE) SetRequestHandler(handler func(context.Context, mcp.JSONRPCRequest) (any, error)) {
    c.requestHandler = handler
}

// Modify handleSSEEvent to handle requests
func (c *SSE) handleSSEEvent(event, data string) {
    switch event {
    case "message":
        var baseMessage struct {
            ID     *mcp.RequestId `json:"id"`
            Method string         `json:"method"`
        }
        
        if err := json.Unmarshal([]byte(data), &baseMessage); err != nil {
            return
        }
        
        // Check if this is a request (has ID and method)
        if baseMessage.ID != nil && baseMessage.Method != "" {
            if c.requestHandler != nil {
                var request mcp.JSONRPCRequest
                if err := json.Unmarshal([]byte(data), &request); err == nil {
                    // Handle the request asynchronously
                    go c.handleIncomingRequest(request)
                }
            }
            return
        }
        
        // ... existing response handling code ...
    }
}

func (c *SSE) handleIncomingRequest(request mcp.JSONRPCRequest) {
    if c.requestHandler == nil {
        return
    }
    
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    result, err := c.requestHandler(ctx, request)
    
    var response mcp.JSONRPCResponse
    if err != nil {
        // Convert to JSON-RPC error response
        response = mcp.JSONRPCResponse{
            JSONRPC: mcp.JSONRPC_VERSION,
            ID:      request.ID,
            Result: mcp.JSONRPCError{
                Error: struct {
                    Code    int    `json:"code"`
                    Message string `json:"message"`
                    Data    any    `json:"data,omitempty"`
                }{
                    Code:    mcp.INTERNAL_ERROR,
                    Message: err.Error(),
                },
            },
        }
    } else {
        response = mcp.JSONRPCResponse{
            JSONRPC: mcp.JSONRPC_VERSION,
            ID:      request.ID,
            Result:  result,
        }
    }
    
    // Send response back through the connection
    c.sendResponse(response)
}
```

#### 5.2 Connect Client Sampling Handler

Modify `client/client.go` to connect the sampling handler to the transport:

```go
// Update Start method
func (c *Client) Start(ctx context.Context) error {
    // ... existing code ...
    
    // Set up request handler for sampling if supported
    if c.samplingHandler != nil {
        if sseTransport, ok := c.transport.(*transport.SSE); ok {
            sseTransport.SetRequestHandler(c.handleIncomingRequest)
        }
        // Add similar handling for other transports
    }
    
    return nil
}

func (c *Client) handleIncomingRequest(ctx context.Context, request mcp.JSONRPCRequest) (any, error) {
    switch request.Method {
    case string(mcp.MethodSamplingCreateMessage):
        var samplingRequest mcp.CreateMessageRequest
        if err := json.Unmarshal(request.Params.(json.RawMessage), &samplingRequest); err != nil {
            return nil, fmt.Errorf("failed to unmarshal sampling request: %w", err)
        }
        
        return c.handleSamplingRequest(ctx, &samplingRequest)
    default:
        return nil, fmt.Errorf("unsupported server request method: %s", request.Method)
    }
}
```

### Phase 6: Helper Functions and Utilities

#### 6.1 Add Utility Functions to `mcp/utils.go`

```go
// Helper functions for sampling
func NewSamplingMessage(role Role, content Content) SamplingMessage {
    return SamplingMessage{
        Role:    role,
        Content: content,
    }
}

func NewCreateMessageRequest(messages []SamplingMessage) *CreateMessageRequest {
    return &CreateMessageRequest{
        Params: CreateMessageParams{
            Messages: messages,
        },
    }
}

func NewCreateMessageResult(role Role, content Content, model string) *CreateMessageResult {
    return &CreateMessageResult{
        Role:    role,
        Content: content,
        Model:   model,
    }
}

// Parsing helpers
func ParseCreateMessageRequest(rawMessage *json.RawMessage) (*CreateMessageRequest, error) {
    if rawMessage == nil {
        return nil, fmt.Errorf("message is nil")
    }
    
    var request CreateMessageRequest
    if err := json.Unmarshal(*rawMessage, &request); err != nil {
        return nil, fmt.Errorf("failed to unmarshal sampling request: %w", err)
    }
    
    return &request, nil
}

func ParseCreateMessageResult(rawMessage *json.RawMessage) (*CreateMessageResult, error) {
    if rawMessage == nil {
        return nil, fmt.Errorf("message is nil")
    }
    
    var result CreateMessageResult
    if err := json.Unmarshal(*rawMessage, &result); err != nil {
        return nil, fmt.Errorf("failed to unmarshal sampling result: %w", err)
    }
    
    return &result, nil
}
```

### Phase 7: Documentation and Examples

#### 7.1 Add Usage Examples

Create `examples/sampling/` directory with:

**`examples/sampling/server/main.go`:**

```go
package main

import (
    "context"
    "fmt"
    
    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"
)

func main() {
    // Create server with sampling capability
    mcpServer := server.NewMCPServer(
        "sampling-example", 
        "1.0.0",
        server.WithSampling(),
    )
    
    // Add a tool that uses sampling
    mcpServer.AddTool(
        mcp.NewTool(
            "analyze_text",
            mcp.WithDescription("Analyzes text using LLM sampling"),
            mcp.WithString("text", mcp.Required(), mcp.Description("Text to analyze")),
        ),
        func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
            text := request.GetString("text", "")
            
            // Get sampling context
            samplingCtx := server.SamplingContextFromContext(ctx)
            if samplingCtx == nil {
                return mcp.NewToolResultError("Sampling not available"), nil
            }
            
            // Request analysis from LLM
            result, err := samplingCtx.Sample(ctx, 
                server.StringInput(fmt.Sprintf("Please analyze this text: %s", text)),
                server.WithTemperature(0.3),
                server.WithMaxTokens(200),
            )
            if err != nil {
                return mcp.NewToolResultError("Sampling failed: " + err.Error()), nil
            }
            
            return mcp.NewToolResultText(result.Text()), nil
        },
    )
    
    // Start stdio server
    server.ServeStdio(mcpServer)
}
```

**`examples/sampling/client/main.go`:**

```go
package main

import (
    "context"
    "fmt"
    "strings"
    
    "github.com/mark3labs/mcp-go/client"
    "github.com/mark3labs/mcp-go/mcp"
)

func main() {
    // Create client with sampling handler
    mcpClient := client.NewStdioMCPClient(
        "node", nil, "server.js",
        client.WithSamplingHandler(client.SamplingHandlerFunc(handleSampling)),
    )
    defer mcpClient.Close()
    
    // Initialize client
    _, err := mcpClient.Initialize(context.Background(), mcp.InitializeRequest{
        Params: mcp.InitializeParams{
            ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
            ClientInfo: mcp.Implementation{
                Name:    "sampling-client",
                Version: "1.0.0",
            },
            Capabilities: mcp.ClientCapabilities{
                Sampling: &struct{}{}, // Enable sampling capability
            },
        },
    })
    if err != nil {
        panic(err)
    }
    
    // Use the server...
}

func handleSampling(ctx context.Context, req *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
    // Simple echo handler for demonstration
    var responseText strings.Builder
    responseText.WriteString("Analysis: ")
    
    for _, msg := range req.Params.Messages {
        if textContent, ok := mcp.AsTextContent(msg.Content); ok {
            responseText.WriteString(textContent.Text)
        }
    }
    
    return &mcp.CreateMessageResult{
        Role:    mcp.RoleAssistant,
        Content: mcp.NewTextContent(responseText.String()),
        Model:   "example-model",
    }, nil
}
```

## Implementation Guidelines

1. **Phase Approach**: Implement in the specified phases to manage complexity
2. **Backward Compatibility**: Ensure all changes are backward compatible
3. **Error Handling**: Use structured error types consistent with existing patterns
4. **Testing**: Add comprehensive tests for each component
5. **Documentation**: Update all relevant documentation and examples
6. **Type Safety**: Maintain strong typing throughout the implementation

## Testing Requirements

1. **Unit Tests**: Test all new types, interfaces, and functions
2. **Integration Tests**: Test end-to-end sampling workflows
3. **Transport Tests**: Test sampling over all transport types
4. **Error Tests**: Test all error conditions and edge cases
5. **Performance Tests**: Verify sampling doesn't introduce significant latency

## Notes

- The most complex part is implementing bidirectional communication since the current SDK only supports client-to-server requests
- Consider using goroutines and channels for handling concurrent sampling requests
- The implementation should be thread-safe for multiple simultaneous sampling requests
- Follow the existing code generation patterns where applicable
- Maintain compatibility with the MCP specification v2025-03-26
