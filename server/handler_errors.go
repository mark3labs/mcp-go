package server

import (
	"context"
	"errors"

	"github.com/mark3labs/mcp-go/mcp"
)

// JSONRPCErrorProvider provides a typed JSON-RPC error response.
type JSONRPCErrorProvider interface {
	error
	JSONRPCError() mcp.JSONRPCError
}

type exposedHandlerError struct {
	provider JSONRPCErrorProvider
}

func (e *exposedHandlerError) Error() string {
	return e.provider.Error()
}

func (e *exposedHandlerError) Unwrap() error {
	return e.provider
}

// NewHandlerError marks a typed JSON-RPC error as safe to return from a
// server handler. Errors are not exposed automatically because they may have
// originated from another server.
func NewHandlerError(provider JSONRPCErrorProvider) error {
	if provider == nil {
		return nil
	}
	return &exposedHandlerError{provider: provider}
}

// handlerError reports an error returned by a tool, prompt or resource
// handler, which is otherwise an internal error.
//
// A handler returns mcp.URLElicitationRequiredError when the request cannot
// proceed until the user completes a URL elicitation. Protocol version
// 2025-11-25 reports that as -32042, carrying the elicitations the client
// has to start. Protocol version 2026-07-28 reserves the code and asks for
// the elicitation through a multi round-trip request instead, so the code is
// chosen per request, as for resourceNotFoundCode.
func handlerError(ctx context.Context, id any, err error) *requestError {
	if required, ok := asURLElicitationRequired(err); ok {
		if !mcp.IsModernProtocol(RequestProtocolVersion(ctx)) {
			details := required.JSONRPCError().Error
			return &requestError{id: id, code: details.Code, err: err, data: details.Data}
		}
		return &requestError{id: id, code: mcp.INTERNAL_ERROR, err: err}
	}

	var exposed *exposedHandlerError
	if errors.As(err, &exposed) {
		details := exposed.provider.JSONRPCError().Error
		return &requestError{
			id:      id,
			code:    details.Code,
			err:     err,
			message: &details.Message,
			data:    details.Data,
		}
	}
	return &requestError{id: id, code: mcp.INTERNAL_ERROR, err: err}
}

// asURLElicitationRequired finds a URLElicitationRequiredError in err's chain,
// whether the handler returned the value or a pointer to it.
func asURLElicitationRequired(err error) (mcp.URLElicitationRequiredError, bool) {
	var required mcp.URLElicitationRequiredError
	if errors.As(err, &required) {
		return required, true
	}
	var pointer *mcp.URLElicitationRequiredError
	if errors.As(err, &pointer) && pointer != nil {
		return *pointer, true
	}
	return required, false
}
