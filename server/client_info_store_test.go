package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestClientInfoStore_DefaultsToZeroValues(t *testing.T) {
	var s clientInfoStore

	assert.Equal(t, mcp.Implementation{}, s.GetClientInfo())
	assert.Equal(t, mcp.ClientCapabilities{}, s.GetClientCapabilities())
}

func TestClientInfoStore_RoundTrip(t *testing.T) {
	var s clientInfoStore

	info := mcp.Implementation{Name: "test-client", Version: "1.2.3"}
	caps := mcp.ClientCapabilities{Sampling: &mcp.SamplingCapability{}}

	s.SetClientInfo(info)
	s.SetClientCapabilities(caps)

	assert.Equal(t, info, s.GetClientInfo())
	assert.Equal(t, caps, s.GetClientCapabilities())
}

// clientInfoMethods captures the four methods clientInfoStore is meant to
// supply via embedding.
type clientInfoMethods interface {
	GetClientInfo() mcp.Implementation
	SetClientInfo(mcp.Implementation)
	GetClientCapabilities() mcp.ClientCapabilities
	SetClientCapabilities(mcp.ClientCapabilities)
}

func TestClientInfoStore_EmbeddedPromotion(t *testing.T) {
	// Verify that embedding clientInfoStore promotes the four methods on
	// the outer type. This is the property each production session relies
	// on to satisfy SessionWithClientInfo without declaring its own
	// getters/setters.
	type fakeSession struct {
		clientInfoStore
	}

	var s clientInfoMethods = &fakeSession{}

	want := mcp.Implementation{Name: "promotion", Version: "0.0.1"}
	s.SetClientInfo(want)
	assert.Equal(t, want, s.GetClientInfo())

	wantCaps := mcp.ClientCapabilities{Sampling: &mcp.SamplingCapability{}}
	s.SetClientCapabilities(wantCaps)
	assert.Equal(t, wantCaps, s.GetClientCapabilities())
}

func TestWriteJSONRPCError_HTTPResponseWriter(t *testing.T) {
	rec := httptest.NewRecorder()

	writeJSONRPCError(rec, "req-1", mcp.INVALID_PARAMS, "bad params", nil)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var decoded mcp.JSONRPCError
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &decoded))
	assert.Equal(t, mcp.JSONRPC_VERSION, decoded.JSONRPC)
	assert.Equal(t, mcp.INVALID_PARAMS, decoded.Error.Code)
	assert.Equal(t, "bad params", decoded.Error.Message)
}

// failingWriter records the WriteHeader call and the Content-Type header,
// then returns an error from Write to exercise the encode-error path.
type failingWriter struct {
	header     http.Header
	statusCode int
	writeCalls atomic.Int32
}

func newFailingWriter() *failingWriter {
	return &failingWriter{header: make(http.Header)}
}

func (f *failingWriter) Header() http.Header        { return f.header }
func (f *failingWriter) WriteHeader(statusCode int) { f.statusCode = statusCode }
func (f *failingWriter) Write(_ []byte) (int, error) {
	f.writeCalls.Add(1)
	return 0, errors.New("boom")
}

func TestWriteJSONRPCError_InvokesOnEncodeErr(t *testing.T) {
	w := newFailingWriter()

	var got error
	writeJSONRPCError(w, nil, mcp.PARSE_ERROR, "parse error", func(err error) {
		got = err
	})

	assert.Equal(t, http.StatusBadRequest, w.statusCode)
	assert.Equal(t, "application/json", w.header.Get("Content-Type"))
	require.Error(t, got)
	assert.Contains(t, got.Error(), "boom")
	assert.Greater(t, w.writeCalls.Load(), int32(0))
}

func TestWriteJSONRPCError_NilCallbackIsSafe(t *testing.T) {
	w := newFailingWriter()

	assert.NotPanics(t, func() {
		writeJSONRPCError(w, nil, mcp.PARSE_ERROR, "parse error", nil)
	})
}

// TestJSONRPCErrorResponseWriter_StaticAssertions is a compile-time check
// that the two response-writer types used by the SSE and streamable HTTP
// transports both satisfy jsonrpcErrorResponseWriter.
func TestJSONRPCErrorResponseWriter_StaticAssertions(t *testing.T) {
	var _ jsonrpcErrorResponseWriter = (http.ResponseWriter)(nil)
	var _ jsonrpcErrorResponseWriter = (HTTPResponseWriter)(nil)
	// Sanity: a recorder is also acceptable as a concrete adapter.
	var _ jsonrpcErrorResponseWriter = httptest.NewRecorder()
}
