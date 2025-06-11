# MCP Sampling Implementation TODO

## Phase 1: Core Types and Protocol Support ⏳
- [ ] 1.1 Add Sampling Types to `mcp/types.go`
  - [ ] SamplingMessage struct
  - [ ] ModelPreferences struct
  - [ ] ModelHint struct
  - [ ] ContextInclusion enum
  - [ ] CreateMessageRequest struct
  - [ ] CreateMessageParams struct
  - [ ] CreateMessageResult struct
- [ ] 1.2 Add Sampling Method Constants
  - [ ] MethodSamplingCreateMessage constant
- [ ] 1.3 Update Server and Client Capabilities
  - [ ] Update ClientCapabilities with Sampling field
  - [ ] Update ServerCapabilities with Sampling field

## Phase 2: Server-Side Sampling Context API ⏳
- [ ] 2.1 Create Sampling Context Interface
  - [ ] Create `server/sampling.go` file
  - [ ] SamplingContext interface
  - [ ] SampleInput interface and implementations
  - [ ] SampleResult struct with helper methods
  - [ ] SampleOption functions
  - [ ] Error definitions
- [ ] 2.2 Implement Sampling Context
  - [ ] samplingContext struct implementation
  - [ ] Sample method implementation
  - [ ] SampleWithParams method implementation
  - [ ] Support methods (supportsClientSampling, sendSamplingRequest)
- [ ] 2.3 Update Server to Support Sampling Context
  - [ ] Add sampling capability to serverCapabilities
  - [ ] Add WithSampling() ServerOption
  - [ ] Add hasSamplingCapability() method
  - [ ] Update WithContext to include sampling context
  - [ ] Add SamplingContextFromContext helper

## Phase 3: Client-Side Sampling Handler Support
- [ ] 3.1 Add Sampling Handler Interface to Client
  - [ ] Create `client/sampling.go` file
  - [ ] SamplingHandler interface
  - [ ] SamplingHandlerFunc adapter
  - [ ] SimpleSamplingHandler type
  - [ ] SamplingError struct and constants
- [ ] 3.2 Update Client to Support Sampling
  - [ ] Add samplingHandler field to Client struct
  - [ ] Add WithSamplingHandler ClientOption
  - [ ] Add WithSimpleSamplingHandler helper
  - [ ] Add handleSamplingRequest method

## Phase 4: Request Handler Updates
- [ ] 4.1 Update Request Handler Generation
  - [ ] Modify `server/request_handler.go` for sampling method
  - [ ] Add handleSamplingCreateMessage to server

## Phase 5: Transport Layer Updates (Complex)
- [ ] 5.1 Enable Server-to-Client Requests
  - [ ] Update SSE transport for bidirectional communication
  - [ ] Update other transports (stdio, etc.)
  - [ ] Add request handler support to transports
- [ ] 5.2 Connect Client Sampling Handler
  - [ ] Update Client.Start method
  - [ ] Add handleIncomingRequest method

## Phase 6: Helper Functions and Utilities
- [ ] 6.1 Add Utility Functions to `mcp/utils.go`
  - [ ] NewSamplingMessage helper
  - [ ] NewCreateMessageRequest helper
  - [ ] NewCreateMessageResult helper
  - [ ] Parse helpers for sampling types

## Phase 7: Documentation and Examples
- [ ] 7.1 Add Usage Examples
  - [ ] Create `examples/sampling/server/main.go`
  - [ ] Create `examples/sampling/client/main.go`
  - [ ] Update README with sampling documentation

## Testing Requirements
- [ ] Unit Tests for all new types and functions
- [ ] Integration Tests for end-to-end sampling
- [ ] Transport Tests for sampling over all transports
- [ ] Error Tests for all error conditions
- [ ] Performance Tests for sampling latency

## Current Status
- **Started**: Phase 1 - Adding core types and protocol support
- **Next**: Complete Phase 1.1 by adding all sampling types to mcp/types.go

## Notes
- Need to examine existing codebase structure first
- Most complex part will be Phase 5 (bidirectional communication)
- Focus on backward compatibility throughout
- Follow existing code patterns and conventions