// Package server provides MCP (Model Control Protocol) server implementations.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
)

// resourceEntry holds both a resource and its handler
type resourceEntry struct {
	resource mcp.Resource
	handler  ResourceHandlerFunc
}

// resourceTemplateEntry holds both a template and its handler
type resourceTemplateEntry struct {
	template mcp.ResourceTemplate
	handler  ResourceTemplateHandlerFunc
}

// ServerOption is a function that configures an MCPServer.
type ServerOption func(*MCPServer)

// ResourceHandlerFunc is a function that returns resource contents.
type ResourceHandlerFunc func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error)

// ResourceTemplateHandlerFunc is a function that returns a resource template.
type ResourceTemplateHandlerFunc func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error)

// PromptHandlerFunc handles prompt requests with given arguments.
type PromptHandlerFunc func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error)

// ToolHandlerFunc handles tool calls with given arguments.
type ToolHandlerFunc func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)

// ServerTool combines a Tool with its ToolHandlerFunc.
type ServerTool struct {
	Tool    mcp.Tool
	Handler ToolHandlerFunc
}

// ClientSession represents an active session that can be used by MCPServer to interact with client.
type ClientSession interface {
	// Initialize marks session as fully initialized and ready for notifications
	Initialize()
	// Initialized returns if session is ready to accept notifications
	Initialized() bool
	// NotificationChannel provides a channel suitable for sending notifications to client.
	NotificationChannel() chan<- mcp.JSONRPCNotification
	// SessionID is a unique identifier used to track user session.
	SessionID() string
}

// clientSessionKey is the context key for storing current client notification channel.
type clientSessionKey struct{}

// ClientSessionFromContext retrieves current client notification context from context.
func ClientSessionFromContext(ctx context.Context) ClientSession {
	if session, ok := ctx.Value(clientSessionKey{}).(ClientSession); ok {
		return session
	}
	return nil
}

// UnparseableMessageError is attached to the RequestError when json.Unmarshal
// fails on the request.
type UnparseableMessageError struct {
	message json.RawMessage
	method  mcp.MCPMethod
	err     error
}

func (e *UnparseableMessageError) Error() string {
	return fmt.Sprintf("unparseable %s request: %s", e.method, e.err)
}

func (e *UnparseableMessageError) Unwrap() error {
	return e.err
}

func (e *UnparseableMessageError) GetMessage() json.RawMessage {
	return e.message
}

func (e *UnparseableMessageError) GetMethod() mcp.MCPMethod {
	return e.method
}

// RequestError is an error that can be converted to a JSON-RPC error.
// Implements Unwrap() to allow inspecting the error chain.
type requestError struct {
	id   any
	code int
	err  error
}

func (e *requestError) Error() string {
	return fmt.Sprintf("request error: %s", e.err)
}

func (e *requestError) ToJSONRPCError() mcp.JSONRPCError {
	return mcp.JSONRPCError{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      e.id,
		Error: struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    any    `json:"data,omitempty"`
		}{
			Code:    e.code,
			Message: e.err.Error(),
		},
	}
}

func (e *requestError) Unwrap() error {
	return e.err
}

var (
	ErrUnsupported      = errors.New("not supported")
	ErrResourceNotFound = errors.New("resource not found")
	ErrPromptNotFound   = errors.New("prompt not found")
	ErrToolNotFound     = errors.New("tool not found")
)

// NotificationHandlerFunc handles incoming notifications.
type NotificationHandlerFunc func(ctx context.Context, notification mcp.JSONRPCNotification)

// MCPServer implements a Model Control Protocol server that can handle various types of requests
// including resources, prompts, and tools.
type MCPServer struct {
	mu                   sync.RWMutex // Add mutex for protecting shared resources
	name                 string
	version              string
	instructions         string
	resources            map[string]resourceEntry
	resourceTemplates    map[string]resourceTemplateEntry
	prompts              map[string]mcp.Prompt
	promptHandlers       map[string]PromptHandlerFunc
	tools                map[string]ServerTool
	notificationHandlers map[string]NotificationHandlerFunc
	capabilities         serverCapabilities
	sessions             sync.Map
	hooks                *Hooks
}

// serverKey is the context key for storing the server instance
type serverKey struct{}

// ServerFromContext retrieves the MCPServer instance from a context
func ServerFromContext(ctx context.Context) *MCPServer {
	if srv, ok := ctx.Value(serverKey{}).(*MCPServer); ok {
		return srv
	}
	return nil
}

// WithContext sets the current client session and returns the provided context
func (s *MCPServer) WithContext(
	ctx context.Context,
	session ClientSession,
) context.Context {
	return context.WithValue(ctx, clientSessionKey{}, session)
}

// RegisterSession saves session that should be notified in case if some server attributes changed.
func (s *MCPServer) RegisterSession(
	session ClientSession,
) error {
	sessionID := session.SessionID()
	if _, exists := s.sessions.LoadOrStore(sessionID, session); exists {
		return fmt.Errorf("session %s is already registered", sessionID)
	}
	return nil
}

// UnregisterSession removes from storage session that is shut down.
func (s *MCPServer) UnregisterSession(
	sessionID string,
) {
	s.sessions.Delete(sessionID)
}

// sendNotificationToAllClients sends a notification to all the currently active clients.
func (s *MCPServer) sendNotificationToAllClients(
	method string,
	params map[string]any,
) {
	notification := mcp.JSONRPCNotification{
		JSONRPC: mcp.JSONRPC_VERSION,
		Notification: mcp.Notification{
			Method: method,
			Params: mcp.NotificationParams{
				AdditionalFields: params,
			},
		},
	}

	s.sessions.Range(func(k, v any) bool {
		if session, ok := v.(ClientSession); ok && session.Initialized() {
			select {
			case session.NotificationChannel() <- notification:
			default:
				// TODO: log blocked channel in the future versions
			}
		}
		return true
	})
}

// SendNotificationToClient sends a notification to the current client
func (s *MCPServer) SendNotificationToClient(
	ctx context.Context,
	method string,
	params map[string]any,
) error {
	session := ClientSessionFromContext(ctx)
	if session == nil || !session.Initialized() {
		return fmt.Errorf("notification channel not initialized")
	}

	notification := mcp.JSONRPCNotification{
		JSONRPC: mcp.JSONRPC_VERSION,
		Notification: mcp.Notification{
			Method: method,
			Params: mcp.NotificationParams{
				AdditionalFields: params,
			},
		},
	}

	select {
	case session.NotificationChannel() <- notification:
		return nil
	default:
		return fmt.Errorf("notification channel full or blocked")
	}
}

// serverCapabilities defines the supported features of the MCP server
type serverCapabilities struct {
	tools     *toolCapabilities
	resources *resourceCapabilities
	prompts   *promptCapabilities
	logging   bool
}

// resourceCapabilities defines the supported resource-related features
type resourceCapabilities struct {
	subscribe   bool
	listChanged bool
}

// promptCapabilities defines the supported prompt-related features
type promptCapabilities struct {
	listChanged bool
}

// toolCapabilities defines the supported tool-related features
type toolCapabilities struct {
	listChanged bool
}

// WithResourceCapabilities configures resource-related server capabilities
func WithResourceCapabilities(subscribe, listChanged bool) ServerOption {
	return func(s *MCPServer) {
		// Always create a non-nil capability object
		s.capabilities.resources = &resourceCapabilities{
			subscribe:   subscribe,
			listChanged: listChanged,
		}
	}
}

// WithHooks allows adding hooks that will be called before or after
// either [all] requests or before / after specific request methods, or else
// prior to returning an error to the client.
func WithHooks(hooks *Hooks) ServerOption {
	return func(s *MCPServer) {
		s.hooks = hooks
	}
}

// WithPromptCapabilities configures prompt-related server capabilities
func WithPromptCapabilities(listChanged bool) ServerOption {
	return func(s *MCPServer) {
		// Always create a non-nil capability object
		s.capabilities.prompts = &promptCapabilities{
			listChanged: listChanged,
		}
	}
}

// WithToolCapabilities configures tool-related server capabilities
func WithToolCapabilities(listChanged bool) ServerOption {
	return func(s *MCPServer) {
		// Always create a non-nil capability object
		s.capabilities.tools = &toolCapabilities{
			listChanged: listChanged,
		}
	}
}

// WithLogging enables logging capabilities for the server
func WithLogging() ServerOption {
	return func(s *MCPServer) {
		s.capabilities.logging = true
	}
}

// WithInstructions sets the server instructions for the client returned in the initialize response
func WithInstructions(instructions string) ServerOption {
	return func(s *MCPServer) {
		s.instructions = instructions
	}
}

// NewMCPServer creates a new MCP server instance with the given name, version and options
func NewMCPServer(
	name, version string,
	opts ...ServerOption,
) *MCPServer {
	s := &MCPServer{
		resources:            make(map[string]resourceEntry),
		resourceTemplates:    make(map[string]resourceTemplateEntry),
		prompts:              make(map[string]mcp.Prompt),
		promptHandlers:       make(map[string]PromptHandlerFunc),
		tools:                make(map[string]ServerTool),
		name:                 name,
		version:              version,
		notificationHandlers: make(map[string]NotificationHandlerFunc),
		capabilities: serverCapabilities{
			tools:     nil,
			resources: nil,
			prompts:   nil,
			logging:   false,
		},
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// AddResource registers a new resource and its handler
func (s *MCPServer) AddResource(
	resource mcp.Resource,
	handler ResourceHandlerFunc,
) {
	if s.capabilities.resources == nil {
		s.capabilities.resources = &resourceCapabilities{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resources[resource.URI] = resourceEntry{
		resource: resource,
		handler:  handler,
	}
}

// AddResourceTemplate registers a new resource template and its handler
func (s *MCPServer) AddResourceTemplate(
	template mcp.ResourceTemplate,
	handler ResourceTemplateHandlerFunc,
) {
	if s.capabilities.resources == nil {
		s.capabilities.resources = &resourceCapabilities{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resourceTemplates[template.URITemplate.Raw()] = resourceTemplateEntry{
		template: template,
		handler:  handler,
	}
}

// AddPrompt registers a new prompt handler with the given name
func (s *MCPServer) AddPrompt(prompt mcp.Prompt, handler PromptHandlerFunc) {
	if s.capabilities.prompts == nil {
		s.capabilities.prompts = &promptCapabilities{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompts[prompt.Name] = prompt
	s.promptHandlers[prompt.Name] = handler
}

// AddTool registers a new tool and its handler
func (s *MCPServer) AddTool(tool mcp.Tool, handler ToolHandlerFunc) {
	s.AddTools(ServerTool{Tool: tool, Handler: handler})
}

// AddTools registers multiple tools at once
func (s *MCPServer) AddTools(tools ...ServerTool) {
	if s.capabilities.tools == nil {
		s.capabilities.tools = &toolCapabilities{}
	}
	s.mu.Lock()
	for _, entry := range tools {
		s.tools[entry.Tool.Name] = entry
	}
	s.mu.Unlock()

	// Send notification to all initialized sessions
	s.sendNotificationToAllClients("notifications/tools/list_changed", nil)
}

// SetTools replaces all existing tools with the provided list
func (s *MCPServer) SetTools(tools ...ServerTool) {
	s.mu.Lock()
	s.tools = make(map[string]ServerTool)
	s.mu.Unlock()
	s.AddTools(tools...)
}

// DeleteTools removes a tool from the server
func (s *MCPServer) DeleteTools(names ...string) {
	s.mu.Lock()
	for _, name := range names {
		delete(s.tools, name)
	}
	s.mu.Unlock()

	// Send notification to all initialized sessions
	s.sendNotificationToAllClients("notifications/tools/list_changed", nil)
}

// AddNotificationHandler registers a new handler for incoming notifications
func (s *MCPServer) AddNotificationHandler(
	method string,
	handler NotificationHandlerFunc,
) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notificationHandlers[method] = handler
}

func (s *MCPServer) handleInitialize(
	ctx context.Context,
	id interface{},
	request mcp.InitializeRequest,
) (*mcp.InitializeResult, *requestError) {
	capabilities := mcp.ServerCapabilities{}

	// Only add resource capabilities if they're configured
	if s.capabilities.resources != nil {
		capabilities.Resources = &struct {
			Subscribe   bool `json:"subscribe,omitempty"`
			ListChanged bool `json:"listChanged,omitempty"`
		}{
			Subscribe:   s.capabilities.resources.subscribe,
			ListChanged: s.capabilities.resources.listChanged,
		}
	}

	// Only add prompt capabilities if they're configured
	if s.capabilities.prompts != nil {
		capabilities.Prompts = &struct {
			ListChanged bool `json:"listChanged,omitempty"`
		}{
			ListChanged: s.capabilities.prompts.listChanged,
		}
	}

	// Only add tool capabilities if they're configured
	if s.capabilities.tools != nil {
		capabilities.Tools = &struct {
			ListChanged bool `json:"listChanged,omitempty"`
		}{
			ListChanged: s.capabilities.tools.listChanged,
		}
	}

	if s.capabilities.logging {
		capabilities.Logging = &struct{}{}
	}

	result := mcp.InitializeResult{
		ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
		ServerInfo: mcp.Implementation{
			Name:    s.name,
			Version: s.version,
		},
		Capabilities: capabilities,
		Instructions: s.instructions,
	}

	if session := ClientSessionFromContext(ctx); session != nil {
		session.Initialize()
	}
	return &result, nil
}

func (s *MCPServer) handlePing(
	ctx context.Context,
	id interface{},
	request mcp.PingRequest,
) (*mcp.EmptyResult, *requestError) {
	return &mcp.EmptyResult{}, nil
}

func (s *MCPServer) handleListResources(
	ctx context.Context,
	id interface{},
	request mcp.ListResourcesRequest,
) (*mcp.ListResourcesResult, *requestError) {
	s.mu.RLock()
	resources := make([]mcp.Resource, 0, len(s.resources))
	for _, entry := range s.resources {
		resources = append(resources, entry.resource)
	}
	s.mu.RUnlock()

	result := mcp.ListResourcesResult{
		Resources: resources,
	}
	if request.Params.Cursor != "" {
		result.NextCursor = "" // Handle pagination if needed
	}
	return &result, nil
}

func (s *MCPServer) handleListResourceTemplates(
	ctx context.Context,
	id interface{},
	request mcp.ListResourceTemplatesRequest,
) (*mcp.ListResourceTemplatesResult, *requestError) {
	s.mu.RLock()
	templates := make([]mcp.ResourceTemplate, 0, len(s.resourceTemplates))
	for _, entry := range s.resourceTemplates {
		templates = append(templates, entry.template)
	}
	s.mu.RUnlock()

	result := mcp.ListResourceTemplatesResult{
		ResourceTemplates: templates,
	}
	if request.Params.Cursor != "" {
		result.NextCursor = "" // Handle pagination if needed
	}
	return &result, nil
}

func (s *MCPServer) handleReadResource(
	ctx context.Context,
	id interface{},
	request mcp.ReadResourceRequest,
) (*mcp.ReadResourceResult, *requestError) {
	s.mu.RLock()
	// First try direct resource handlers
	if entry, ok := s.resources[request.Params.URI]; ok {
		handler := entry.handler
		s.mu.RUnlock()
		contents, err := handler(ctx, request)
		if err != nil {
			return nil, &requestError{
				id:   id,
				code: mcp.INTERNAL_ERROR,
				err:  err,
			}
		}
		return &mcp.ReadResourceResult{Contents: contents}, nil
	}

	// If no direct handler found, try matching against templates
	var matchedHandler ResourceTemplateHandlerFunc
	var matched bool
	for _, entry := range s.resourceTemplates {
		template := entry.template
		if matchesTemplate(request.Params.URI, template.URITemplate) {
			matchedHandler = entry.handler
			matched = true
			matchedVars := template.URITemplate.Match(request.Params.URI)
			// Convert matched variables to a map
			request.Params.Arguments = make(map[string]interface{})
			for name, value := range matchedVars {
				request.Params.Arguments[name] = value.V
			}
			break
		}
	}
	s.mu.RUnlock()

	if matched {
		contents, err := matchedHandler(ctx, request)
		if err != nil {
			return nil, &requestError{
				id:   id,
				code: mcp.INTERNAL_ERROR,
				err:  err,
			}
		}
		return &mcp.ReadResourceResult{Contents: contents}, nil
	}

	return nil, &requestError{
		id:   id,
		code: mcp.INVALID_PARAMS,
		err:  fmt.Errorf("handler not found for resource URI '%s': %w", request.Params.URI, ErrResourceNotFound),
	}
}

// matchesTemplate checks if a URI matches a URI template pattern
func matchesTemplate(uri string, template *mcp.URITemplate) bool {
	return template.Regexp().MatchString(uri)
}

func (s *MCPServer) handleListPrompts(
	ctx context.Context,
	id interface{},
	request mcp.ListPromptsRequest,
) (*mcp.ListPromptsResult, *requestError) {
	s.mu.RLock()
	prompts := make([]mcp.Prompt, 0, len(s.prompts))
	for _, prompt := range s.prompts {
		prompts = append(prompts, prompt)
	}
	s.mu.RUnlock()

	result := mcp.ListPromptsResult{
		Prompts: prompts,
	}
	if request.Params.Cursor != "" {
		result.NextCursor = "" // Handle pagination if needed
	}
	return &result, nil
}

func (s *MCPServer) handleGetPrompt(
	ctx context.Context,
	id interface{},
	request mcp.GetPromptRequest,
) (*mcp.GetPromptResult, *requestError) {
	s.mu.RLock()
	handler, ok := s.promptHandlers[request.Params.Name]
	s.mu.RUnlock()

	if !ok {
		return nil, &requestError{
			id:   id,
			code: mcp.INVALID_PARAMS,
			err:  fmt.Errorf("prompt '%s' not found: %w", request.Params.Name, ErrPromptNotFound),
		}
	}

	result, err := handler(ctx, request)
	if err != nil {
		return nil, &requestError{
			id:   id,
			code: mcp.INTERNAL_ERROR,
			err:  err,
		}
	}

	return result, nil
}

func (s *MCPServer) handleListTools(
	ctx context.Context,
	id interface{},
	request mcp.ListToolsRequest,
) (*mcp.ListToolsResult, *requestError) {
	s.mu.RLock()
	tools := make([]mcp.Tool, 0, len(s.tools))

	// Get all tool names for consistent ordering
	toolNames := make([]string, 0, len(s.tools))
	for name := range s.tools {
		toolNames = append(toolNames, name)
	}

	// Sort the tool names for consistent ordering
	sort.Strings(toolNames)

	// Add tools in sorted order
	for _, name := range toolNames {
		tools = append(tools, s.tools[name].Tool)
	}
	s.mu.RUnlock()

	result := mcp.ListToolsResult{
		Tools: tools,
	}
	if request.Params.Cursor != "" {
		result.NextCursor = "" // Handle pagination if needed
	}
	return &result, nil
}
func (s *MCPServer) handleToolCall(
	ctx context.Context,
	id interface{},
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, *requestError) {
	s.mu.RLock()
	tool, ok := s.tools[request.Params.Name]
	s.mu.RUnlock()

	if !ok {
		return nil, &requestError{
			id:   id,
			code: mcp.INVALID_PARAMS,
			err:  fmt.Errorf("tool '%s' not found: %w", request.Params.Name, ErrToolNotFound),
		}
	}

	result, err := tool.Handler(ctx, request)
	if err != nil {
		return nil, &requestError{
			id:   id,
			code: mcp.INTERNAL_ERROR,
			err:  err,
		}
	}

	return result, nil
}

func (s *MCPServer) handleNotification(
	ctx context.Context,
	notification mcp.JSONRPCNotification,
) mcp.JSONRPCMessage {
	s.mu.RLock()
	handler, ok := s.notificationHandlers[notification.Method]
	s.mu.RUnlock()

	if ok {
		handler(ctx, notification)
	}
	return nil
}

func createResponse(id interface{}, result interface{}) mcp.JSONRPCMessage {
	return mcp.JSONRPCResponse{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      id,
		Result:  result,
	}
}

func createErrorResponse(
	id interface{},
	code int,
	message string,
) mcp.JSONRPCMessage {
	return mcp.JSONRPCError{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      id,
		Error: struct {
			Code    int         `json:"code"`
			Message string      `json:"message"`
			Data    interface{} `json:"data,omitempty"`
		}{
			Code:    code,
			Message: message,
		},
	}
}
