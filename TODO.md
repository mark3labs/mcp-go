# MCP Sampling Implementation TODO

## Phase 1: Core Types and Protocol Support ✅ COMPLETED
- [x] 1.1 Add Sampling Types to `mcp/types.go`
  - [x] SamplingMessage struct ✅
  - [x] ModelPreferences struct ✅
  - [x] ModelHint struct ✅
  - [x] ContextInclusion enum ✅
  - [x] CreateMessageRequest struct ✅
  - [x] CreateMessageParams struct ✅
  - [x] CreateMessageResult struct ✅
- [x] 1.2 Add Sampling Method Constants
  - [x] MethodSamplingCreateMessage constant ✅
- [x] 1.3 Update Server and Client Capabilities
  - [x] Update ClientCapabilities with Sampling field ✅
  - [x] Update ServerCapabilities with Sampling field ✅

## Phase 2: Server-Side Sampling Context API ✅ COMPLETED
- [x] 2.1 Create Sampling Context Interface
  - [x] Create `server/sampling.go` file ✅
  - [x] SamplingContext interface ✅
  - [x] SampleInput interface and implementations ✅
  - [x] SampleResult struct with helper methods ✅
  - [x] SampleOption functions ✅
  - [x] Error definitions ✅
- [x] 2.2 Implement Sampling Context
  - [x] samplingContext struct implementation ✅
  - [x] Sample method implementation ✅
  - [x] SampleWithParams method implementation ✅
  - [x] Support methods (supportsClientSampling, sendSamplingRequest) ✅
- [x] 2.3 Update Server to Support Sampling Context
  - [x] Add sampling capability to serverCapabilities ✅
  - [x] Add WithSampling() ServerOption ✅ (needs cleanup - duplicates exist)
  - [x] Add hasSamplingCapability() method ✅
  - [x] Update WithContext to include sampling context ✅
  - [x] Add SamplingContextFromContext helper ✅

## Phase 3: Client-Side Sampling Handler Support ⏳ IN PROGRESS
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

## Phase 5: Transport Layer Updates (Complex) ⚠️ CRITICAL
- [ ] 5.1 Enable Server-to-Client Requests
  - [ ] Update SSE transport for bidirectional communication
  - [ ] Update other transports (stdio, etc.)
  - [ ] Add request handler support to transports
- [ ] 5.2 Connect Client Sampling Handler
  - [ ] Update Client.Start method
  - [ ] Add handleIncomingRequest method

## Phase 6: Helper Functions and Utilities ✅ COMPLETED
- [x] 6.1 Add Utility Functions to `mcp/sampling_utils.go`
  - [x] NewSamplingMessage helper ✅
  - [x] NewCreateMessageRequest helper ✅
  - [x] NewCreateMessageResult helper ✅
  - [x] Parse helpers for sampling types ✅

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
- **Completed**: Phases 1, 2, and 6 - Core types, server-side API, and utilities
- **In Progress**: Phase 3 - Client-side sampling handler support
- **Next**: Complete Phase 3, then tackle Phase 5 (bidirectional communication)

## Key Issues Found
1. **Duplicate WithSampling functions** in server/server.go (lines 281-314) - needs cleanup
2. **Missing client-side sampling support** - need to create client/sampling.go
3. **Bidirectional communication not implemented** - sendSamplingRequest returns placeholder error
4. **Transport layer updates needed** - no support for server-to-client requests yet

## Notes
- Core types and server API are well implemented
- Main challenge is implementing bidirectional communication (server-to-client requests)
- Current sendSamplingRequest function returns "bidirectional communication needed" error
- Need to examine transport layer architecture to understand how to add server-to-client requests