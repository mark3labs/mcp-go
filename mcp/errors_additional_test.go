package mcp

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnsupportedProtocolVersionError_Is(t *testing.T) {
	err1 := UnsupportedProtocolVersionError{Version: "1.0"}
	err2 := UnsupportedProtocolVersionError{Version: "2.0"}

	t.Run("matches same type", func(t *testing.T) {
		assert.True(t, err1.Is(UnsupportedProtocolVersionError{}))
		assert.True(t, err2.Is(UnsupportedProtocolVersionError{Version: "different"}))
	})

	t.Run("does not match different type", func(t *testing.T) {
		assert.False(t, err1.Is(errors.New("different error")))
		assert.False(t, err1.Is(ErrMethodNotFound))
	})
}

func TestIsUnsupportedProtocolVersion(t *testing.T) {
	t.Run("returns true for UnsupportedProtocolVersionError", func(t *testing.T) {
		err := UnsupportedProtocolVersionError{Version: "1.0"}
		assert.True(t, IsUnsupportedProtocolVersion(err))
	})

	t.Run("returns false for other errors", func(t *testing.T) {
		assert.False(t, IsUnsupportedProtocolVersion(errors.New("other error")))
		assert.False(t, IsUnsupportedProtocolVersion(ErrMethodNotFound))
	})

	t.Run("returns false for wrapped errors", func(t *testing.T) {
		// Create a wrapped error - IsUnsupportedProtocolVersion checks direct type, not wrapped
		err := UnsupportedProtocolVersionError{Version: "1.0"}
		wrapped := errors.New("wrapped: " + err.Error())
		assert.False(t, IsUnsupportedProtocolVersion(wrapped))
	})
}

func TestJSONRPCErrorDetails_AsError_EmptyMessage(t *testing.T) {
	t.Run("with empty message", func(t *testing.T) {
		details := JSONRPCErrorDetails{
			Code:    METHOD_NOT_FOUND,
			Message: "",
		}

		err := details.AsError()
		// Should return the sentinel error when message is empty
		assert.Equal(t, ErrMethodNotFound, err)
	})

	t.Run("with message matching sentinel", func(t *testing.T) {
		details := JSONRPCErrorDetails{
			Code:    PARSE_ERROR,
			Message: "parse error",
		}

		err := details.AsError()
		assert.Equal(t, ErrParseError, err)
	})
}

func TestJSONRPCErrorDetails_AsError_AllCodes(t *testing.T) {
	tests := []struct {
		name        string
		code        int
		message     string
		sentinel    error
		shouldMatch bool
	}{
		{
			name:        "PARSE_ERROR",
			code:        PARSE_ERROR,
			message:     "custom parse error",
			sentinel:    ErrParseError,
			shouldMatch: true,
		},
		{
			name:        "INVALID_REQUEST",
			code:        INVALID_REQUEST,
			message:     "custom invalid request",
			sentinel:    ErrInvalidRequest,
			shouldMatch: true,
		},
		{
			name:        "METHOD_NOT_FOUND",
			code:        METHOD_NOT_FOUND,
			message:     "custom method not found",
			sentinel:    ErrMethodNotFound,
			shouldMatch: true,
		},
		{
			name:        "INVALID_PARAMS",
			code:        INVALID_PARAMS,
			message:     "custom invalid params",
			sentinel:    ErrInvalidParams,
			shouldMatch: true,
		},
		{
			name:        "INTERNAL_ERROR",
			code:        INTERNAL_ERROR,
			message:     "custom internal error",
			sentinel:    ErrInternalError,
			shouldMatch: true,
		},
		{
			name:        "REQUEST_INTERRUPTED",
			code:        REQUEST_INTERRUPTED,
			message:     "custom interrupted",
			sentinel:    ErrRequestInterrupted,
			shouldMatch: true,
		},
		{
			name:        "RESOURCE_NOT_FOUND",
			code:        RESOURCE_NOT_FOUND,
			message:     "custom resource not found",
			sentinel:    ErrResourceNotFound,
			shouldMatch: true,
		},
		{
			name:        "unknown code",
			code:        -99999,
			message:     "unknown error",
			sentinel:    nil,
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			details := JSONRPCErrorDetails{
				Code:    tt.code,
				Message: tt.message,
			}

			err := details.AsError()
			require.NotNil(t, err)

			if tt.shouldMatch {
				assert.True(t, errors.Is(err, tt.sentinel))
				// Custom message should be wrapped
				assert.Contains(t, err.Error(), tt.message)
			} else {
				// Unknown codes just return the message
				assert.Equal(t, tt.message, err.Error())
			}
		})
	}
}

func TestErrorChaining_WithAs(t *testing.T) {
	t.Run("errors.As does not work with wrapped sentinel", func(t *testing.T) {
		details := &JSONRPCErrorDetails{
			Code:    METHOD_NOT_FOUND,
			Message: "Method 'foo' not found",
		}

		err := details.AsError()

		// Since we wrap with fmt.Errorf, errors.As won't find the exact type
		// but errors.Is will work because of the %w verb
		assert.True(t, errors.Is(err, ErrMethodNotFound))
	})
}

func TestSentinelErrors_Comparison(t *testing.T) {
	// Ensure all sentinel errors are distinct
	sentinels := []error{
		ErrParseError,
		ErrInvalidRequest,
		ErrMethodNotFound,
		ErrInvalidParams,
		ErrInternalError,
		ErrRequestInterrupted,
		ErrResourceNotFound,
	}

	for i, err1 := range sentinels {
		for j, err2 := range sentinels {
			if i == j {
				assert.True(t, errors.Is(err1, err2), "Same sentinel should match itself")
			} else {
				assert.False(t, errors.Is(err1, err2), "Different sentinels should not match")
			}
		}
	}
}

func TestUnsupportedProtocolVersionError_Error(t *testing.T) {
	err := UnsupportedProtocolVersionError{Version: "3.0"}
	assert.Equal(t, `unsupported protocol version: "3.0"`, err.Error())
}

func TestJSONRPCErrorDetails_WithData(t *testing.T) {
	details := &JSONRPCErrorDetails{
		Code:    INVALID_PARAMS,
		Message: "Invalid parameter 'foo'",
		Data: map[string]any{
			"param":    "foo",
			"expected": "string",
			"got":      "number",
		},
	}

	err := details.AsError()

	// The error should still wrap properly
	assert.True(t, errors.Is(err, ErrInvalidParams))
	assert.Contains(t, err.Error(), "Invalid parameter 'foo'")

	// Data is not included in the error string, but it's preserved in the details
	assert.NotNil(t, details.Data)
}
