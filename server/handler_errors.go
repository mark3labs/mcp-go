package server

import (
	"context"
	"errors"

	"github.com/mark3labs/mcp-go/mcp"
)

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
	var required mcp.URLElicitationRequiredError
	if errors.As(err, &required) && !mcp.IsModernProtocol(RequestProtocolVersion(ctx)) {
		details := required.JSONRPCError().Error
		return &requestError{id: id, code: details.Code, err: err, data: details.Data}
	}
	return &requestError{id: id, code: mcp.INTERNAL_ERROR, err: err}
}
