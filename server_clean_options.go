// WithLogging enables logging capabilities for the server
func WithLogging() ServerOption {
	return func(s *MCPServer) {
		s.capabilities.logging = mcp.ToBoolPtr(true)
	}
}

// WithSampling enables sampling capabilities for the server
func WithSampling() ServerOption {
	return func(s *MCPServer) {
		s.capabilities.sampling = mcp.ToBoolPtr(true)
	}
}