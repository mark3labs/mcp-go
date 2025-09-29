package mcp

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJSONRPCErrorDetails_AsError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		details         JSONRPCErrorDetails
		expectedType    error
		expectedMessage string
	}{
		{
			name: "parse error with custom message",
			details: JSONRPCErrorDetails{
				Code:    PARSE_ERROR,
				Message: "Custom parse error message",
			},
			expectedType:    ErrParseError,
			expectedMessage: "parse error: Custom parse error message",
		},
		{
			name: "parse error with standard message",
			details: JSONRPCErrorDetails{
				Code:    PARSE_ERROR,
				Message: "parse error",
			},
			expectedType:    ErrParseError,
			expectedMessage: "parse error",
		},
		{
			name: "method not found with custom message",
			details: JSONRPCErrorDetails{
				Code:    METHOD_NOT_FOUND,
				Message: "Custom method not found message",
			},
			expectedType:    ErrMethodNotFound,
			expectedMessage: "method not found: Custom method not found message",
		},
		{
			name: "method not found with standard message",
			details: JSONRPCErrorDetails{
				Code:    METHOD_NOT_FOUND,
				Message: "method not found",
			},
			expectedType:    ErrMethodNotFound,
			expectedMessage: "method not found",
		},
		{
			name: "request interrupted with custom message",
			details: JSONRPCErrorDetails{
				Code:    REQUEST_INTERRUPTED,
				Message: "request was cancelled",
			},
			expectedType:    ErrRequestInterrupted,
			expectedMessage: "request interrupted: request was cancelled",
		},
		{
			name: "resource not found with custom message",
			details: JSONRPCErrorDetails{
				Code:    RESOURCE_NOT_FOUND,
				Message: "resource 'foo' not found",
			},
			expectedType:    ErrResourceNotFound,
			expectedMessage: "resource not found: resource 'foo' not found",
		},
		{
			name: "unknown error code",
			details: JSONRPCErrorDetails{
				Code:    -99999,
				Message: "Unknown error occurred",
			},
			expectedType:    nil, // No sentinel error for unknown codes
			expectedMessage: "Unknown error occurred",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := tc.details.AsError()

			// Check the full error message
			require.EqualError(t, result, tc.expectedMessage)

			// Check the error type (if expected)
			if tc.expectedType != nil {
				require.True(t, errors.Is(result, tc.expectedType),
					"Expected error to be of type %v", tc.expectedType)
			}
		})
	}
}

func TestJSONRPCErrorDetails_AsError_WithPointer(t *testing.T) {
	t.Parallel()

	details := &JSONRPCErrorDetails{
		Code:    METHOD_NOT_FOUND,
		Message: "Method not found",
		Data:    map[string]string{"extra": "info"},
	}

	result := details.AsError()
	require.True(t, errors.Is(result, ErrMethodNotFound))
}

func TestSentinelErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err error
		msg string
	}{
		"ErrParseError": {
			err: ErrParseError,
			msg: "parse error",
		},
		"ErrInvalidRequest": {
			err: ErrInvalidRequest,
			msg: "invalid request",
		},
		"ErrMethodNotFound": {
			err: ErrMethodNotFound,
			msg: "method not found",
		},
		"ErrInvalidParams": {
			err: ErrInvalidParams,
			msg: "invalid params",
		},
		"ErrInternalError": {
			err: ErrInternalError,
			msg: "internal error",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.msg, tc.err.Error())
		})
	}
}

func TestErrorChaining(t *testing.T) {
	t.Parallel()

	// Test that errors.Is works correctly with wrapped errors
	details := &JSONRPCErrorDetails{
		Code:    METHOD_NOT_FOUND,
		Message: "Method 'foo' not found",
	}

	err := details.AsError()
	wrappedErr := errors.New("failed to call method: " + err.Error())

	// The wrapped error should not match our sentinel error
	require.False(t, errors.Is(wrappedErr, ErrMethodNotFound))

	// But the original error should
	require.True(t, errors.Is(err, ErrMethodNotFound))
}
