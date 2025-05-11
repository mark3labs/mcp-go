package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPContextFunc is a function that takes an existing context and the current
// request and returns a potentially modified context based on the request
// content. This can be used to inject context values from headers, for example.
type HTTPContextFunc func(ctx context.Context, r *http.Request) context.Context

// httpTransportConfigurable is an internal interface for shared HTTP transport configuration.
type httpTransportConfigurable interface {
	setBasePath(string)
	setDynamicBasePath(DynamicBasePathFunc)
	setKeepAliveInterval(time.Duration)
	setKeepAlive(bool)
	setContextFunc(HTTPContextFunc)
	setHTTPServer(*http.Server)
	setBaseURL(string)
}

// HTTPTransportOption is a function that configures an httpTransportConfigurable.
type HTTPTransportOption func(httpTransportConfigurable)

// Option interfaces and wrappers for server configuration
// Base option interface
type HTTPServerOption interface {
	isHTTPServerOption()
}

// SSE-specific option interface
type SSEOption interface {
	HTTPServerOption
	applyToSSE(*SSEServer)
}

// StreamableHTTP-specific option interface
type StreamableHTTPOption interface {
	HTTPServerOption
	applyToStreamableHTTP(*StreamableHTTPServer)
}

// Common options that work with both server types
type CommonHTTPServerOption interface {
	SSEOption
	StreamableHTTPOption
}

// Wrapper for SSE-specific functional options
type sseOption func(*SSEServer)

func (o sseOption) isHTTPServerOption()     {}
func (o sseOption) applyToSSE(s *SSEServer) { o(s) }

// Wrapper for StreamableHTTP-specific functional options
type streamableHTTPOption func(*StreamableHTTPServer)

func (o streamableHTTPOption) isHTTPServerOption()                           {}
func (o streamableHTTPOption) applyToStreamableHTTP(s *StreamableHTTPServer) { o(s) }

// Refactor commonOption to use a single apply func(httpTransportConfigurable)
type commonOption struct {
	apply func(httpTransportConfigurable)
}

func (o commonOption) isHTTPServerOption()                           {}
func (o commonOption) applyToSSE(s *SSEServer)                       { o.apply(s) }
func (o commonOption) applyToStreamableHTTP(s *StreamableHTTPServer) { o.apply(s) }

// TODO: This is a stub implementation of StreamableHTTPServer just to show how
// to use it with the new options interfaces.
type StreamableHTTPServer struct{}

// Add stub methods to satisfy httpTransportConfigurable

func (s *StreamableHTTPServer) setBasePath(string)                     {}
func (s *StreamableHTTPServer) setDynamicBasePath(DynamicBasePathFunc) {}
func (s *StreamableHTTPServer) setKeepAliveInterval(time.Duration)     {}
func (s *StreamableHTTPServer) setKeepAlive(bool)                      {}
func (s *StreamableHTTPServer) setContextFunc(HTTPContextFunc)         {}
func (s *StreamableHTTPServer) setHTTPServer(srv *http.Server)         {}
func (s *StreamableHTTPServer) setBaseURL(baseURL string)              {}

// Ensure the option types implement the correct interfaces
var (
	_ httpTransportConfigurable = (*StreamableHTTPServer)(nil)
	_ SSEOption                 = sseOption(nil)
	_ StreamableHTTPOption      = streamableHTTPOption(nil)
	_ CommonHTTPServerOption    = commonOption{}
)

// WithStaticBasePath adds a new option for setting a static base path.
// This is useful for mounting the server at a known, fixed path.
func WithStaticBasePath(basePath string) CommonHTTPServerOption {
	return commonOption{
		apply: func(c httpTransportConfigurable) {
			c.setBasePath(basePath)
		},
	}
}

// DynamicBasePathFunc allows the user to provide a function to generate the
// base path for a given request and sessionID. This is useful for cases where
// the base path is not known at the time of SSE server creation, such as when
// using a reverse proxy or when the base path is dynamically generated. The
// function should return the base path (e.g., "/mcp/tenant123").
type DynamicBasePathFunc func(r *http.Request, sessionID string) string

// WithDynamicBasePath accepts a function for generating the base path.
// This is useful for cases where the base path is not known at the time of server creation,
// such as when using a reverse proxy or when the server is mounted at a dynamic path.
func WithDynamicBasePath(fn DynamicBasePathFunc) CommonHTTPServerOption {
	return commonOption{
		apply: func(c httpTransportConfigurable) {
			c.setDynamicBasePath(fn)
		},
	}
}

// WithKeepAliveInterval sets the keep-alive interval for the transport.
// When enabled, the server will periodically send ping events to keep the connection alive.
func WithKeepAliveInterval(interval time.Duration) CommonHTTPServerOption {
	return commonOption{
		apply: func(c httpTransportConfigurable) {
			c.setKeepAliveInterval(interval)
		},
	}
}

// WithKeepAlive enables or disables keep-alive for the transport.
// When enabled, the server will send periodic keep-alive events to clients.
func WithKeepAlive(keepAlive bool) CommonHTTPServerOption {
	return commonOption{
		apply: func(c httpTransportConfigurable) {
			c.setKeepAlive(keepAlive)
		},
	}
}

// WithHTTPContextFunc sets a function that will be called to customize the context
// for the server using the incoming request. This is useful for injecting
// context values from headers or other request properties.
func WithHTTPContextFunc(fn HTTPContextFunc) CommonHTTPServerOption {
	return commonOption{
		apply: func(c httpTransportConfigurable) {
			c.setContextFunc(fn)
		},
	}
}

// WithBaseURL sets the base URL for the HTTP transport server.
// This is useful for configuring the externally visible base URL for clients.
func WithBaseURL(baseURL string) CommonHTTPServerOption {
	return commonOption{
		apply: func(c httpTransportConfigurable) {
			if baseURL != "" {
				u, err := url.Parse(baseURL)
				if err != nil {
					return
				}
				if u.Scheme != "http" && u.Scheme != "https" {
					return
				}
				if u.Host == "" || strings.HasPrefix(u.Host, ":") {
					return
				}
				if len(u.Query()) > 0 {
					return
				}
			}
			c.setBaseURL(strings.TrimSuffix(baseURL, "/"))
		},
	}
}

// WithHTTPServer sets the HTTP server instance for the transport.
// This is useful for advanced scenarios where you want to provide your own http.Server.
func WithHTTPServer(srv *http.Server) CommonHTTPServerOption {
	return commonOption{
		apply: func(c httpTransportConfigurable) {
			c.setHTTPServer(srv)
		},
	}
}
