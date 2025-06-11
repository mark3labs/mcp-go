// Sampling-related client options and methods

// WithSamplingHandler sets a sampling handler for the client and enables sampling capability
func WithSamplingHandler(handler SamplingHandler) ClientOption {
	return func(c *Client) {
		c.samplingHandler = handler
		// Enable sampling capability
		if c.clientCapabilities.Sampling == nil {
			c.clientCapabilities.Sampling = &struct{}{}
		}
	}
}

// WithSimpleSamplingHandler sets a simple sampling handler for the client
func WithSimpleSamplingHandler(handler SimpleSamplingHandler) ClientOption {
	return WithSamplingHandler(handler.ToSamplingHandler())
}

// handleSamplingRequest processes a sampling request from the server
func (c *Client) handleSamplingRequest(ctx context.Context, req *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	if c.samplingHandler == nil {
		return nil, NewSamplingError(ErrCodeSamplingNotSupported, "sampling not supported")
	}

	return c.samplingHandler.HandleSampling(ctx, req)
}

// handleIncomingRequest processes incoming requests from the server (for bidirectional communication)
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