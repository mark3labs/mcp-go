package mcp

// This file contains updates to the existing types.go to implement the MCP sampling specification.
// These additions should be integrated into the main types.go file.

// Additional method constant for sampling
const (
	// MethodSamplingCreateMessage allows servers to request LLM completions from clients
	// https://modelcontextprotocol.io/docs/concepts/sampling
	MethodSamplingCreateMessage MCPMethod = "sampling/createMessage"
)

// Context inclusion options for sampling
type ContextInclusion string

const (
	ContextNone       ContextInclusion = "none"
	ContextThisServer ContextInclusion = "thisServer"
	ContextAllServers ContextInclusion = "allServers"
)

// Additional Role constants
const (
	RoleSystem Role = "system"
)