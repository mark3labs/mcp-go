// Sampling-related client options to be added to client.go after WithClientCapabilities

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