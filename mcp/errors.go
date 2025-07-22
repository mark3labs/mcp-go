package mcp

import "fmt"

// UnsupportedProtocolVersionError is returned when the server responds with
// a protocol version that the client doesn't support.
type UnsupportedProtocolVersionError struct {
	Version string
}

func (e UnsupportedProtocolVersionError) Error() string {
	return fmt.Sprintf("unsupported protocol version: %q", e.Version)
}

// IsUnsupportedProtocolVersion checks if an error is an UnsupportedProtocolVersionError
func IsUnsupportedProtocolVersion(err error) bool {
	_, ok := err.(UnsupportedProtocolVersionError)
	return ok
}
