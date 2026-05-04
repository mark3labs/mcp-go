package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureHandler is a slog.Handler that records every emitted record so
// tests can assert on the structured output of the LoggingTransport.
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
	level   slog.Level
}

func newCaptureHandler() *captureHandler {
	return &captureHandler{level: slog.LevelDebug}
}

func (h *captureHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func (h *captureHandler) snapshot() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]slog.Record, len(h.records))
	copy(out, h.records)
	return out
}

func (h *captureHandler) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = nil
}

func attrsOf(r slog.Record) map[string]slog.Value {
	m := make(map[string]slog.Value)
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value
		return true
	})
	return m
}

// fakeTransport is a minimal Interface implementation used to drive the
// LoggingTransport in tests.
type fakeTransport struct {
	startErr        error
	requestResp     *JSONRPCResponse
	requestErr      error
	notificationErr error
	notifyHandler   func(notification mcp.JSONRPCNotification)
	sentRequests    []JSONRPCRequest
	sentNotifs      []mcp.JSONRPCNotification
	sessionID       string
	closed          bool
}

func (f *fakeTransport) Start(_ context.Context) error { return f.startErr }

func (f *fakeTransport) SendRequest(_ context.Context, req JSONRPCRequest) (*JSONRPCResponse, error) {
	f.sentRequests = append(f.sentRequests, req)
	if f.requestErr != nil {
		return nil, f.requestErr
	}
	return f.requestResp, nil
}

func (f *fakeTransport) SendNotification(_ context.Context, n mcp.JSONRPCNotification) error {
	f.sentNotifs = append(f.sentNotifs, n)
	return f.notificationErr
}

func (f *fakeTransport) SetNotificationHandler(handler func(notification mcp.JSONRPCNotification)) {
	f.notifyHandler = handler
}

func (f *fakeTransport) Close() error {
	f.closed = true
	return nil
}

func (f *fakeTransport) GetSessionId() string { return f.sessionID }

// fakeBidirTransport additionally implements BidirectionalInterface.
type fakeBidirTransport struct {
	fakeTransport
	requestHandler RequestHandler
}

func (f *fakeBidirTransport) SetRequestHandler(handler RequestHandler) {
	f.requestHandler = handler
}

// fakeHTTPTransport additionally implements HTTPConnection.
type fakeHTTPTransport struct {
	fakeTransport
	protocolVersion string
}

func (f *fakeHTTPTransport) SetProtocolVersion(version string) {
	f.protocolVersion = version
}

// fakeBidirHTTPTransport implements both optional interfaces.
type fakeBidirHTTPTransport struct {
	fakeBidirTransport
	protocolVersion string
}

func (f *fakeBidirHTTPTransport) SetProtocolVersion(version string) {
	f.protocolVersion = version
}

func TestLoggingTransport_BasicInterface(t *testing.T) {
	inner := &fakeTransport{
		requestResp: &JSONRPCResponse{
			JSONRPC: mcp.JSONRPC_VERSION,
			ID:      mcp.NewRequestId(int64(1)),
			Result:  json.RawMessage(`{"ok":true}`),
		},
		sessionID: "session-1",
	}
	handler := newCaptureHandler()
	logger := slog.New(handler)

	transport := NewLogging(inner, logger)

	// Should not satisfy optional interfaces.
	_, isBidir := transport.(BidirectionalInterface)
	assert.False(t, isBidir, "plain transport should not implement BidirectionalInterface")
	_, isHTTP := transport.(HTTPConnection)
	assert.False(t, isHTTP, "plain transport should not implement HTTPConnection")

	require.NoError(t, transport.Start(t.Context()))

	resp, err := transport.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(1)),
		Method:  "tools/list",
		Params:  map[string]any{"cursor": "abc"},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	require.NoError(t, transport.SendNotification(t.Context(), mcp.JSONRPCNotification{
		JSONRPC: mcp.JSONRPC_VERSION,
		Notification: mcp.Notification{
			Method: "notifications/initialized",
		},
	}))

	assert.Equal(t, "session-1", transport.GetSessionId())
	require.NoError(t, transport.Close())
	assert.True(t, inner.closed)

	records := handler.snapshot()
	require.Len(t, records, 3)

	// Outgoing request
	assert.Equal(t, "→ request", records[0].Message)
	reqAttrs := attrsOf(records[0])
	assert.Equal(t, "tools/list", reqAttrs["method"].String())
	require.Contains(t, reqAttrs, "id")
	require.Contains(t, reqAttrs, "params")

	// Incoming response
	assert.Equal(t, "← response", records[1].Message)
	respAttrs := attrsOf(records[1])
	assert.Equal(t, "tools/list", respAttrs["method"].String())
	require.Contains(t, respAttrs, "duration")
	require.Contains(t, respAttrs, "result")
	assert.Equal(t, `{"ok":true}`, respAttrs["result"].String())

	// Outgoing notification
	assert.Equal(t, "→ notification", records[2].Message)
	notifAttrs := attrsOf(records[2])
	assert.Equal(t, "notifications/initialized", notifAttrs["method"].String())
}

func TestLoggingTransport_LogsRequestErrors(t *testing.T) {
	inner := &fakeTransport{
		requestErr: errors.New("boom"),
	}
	handler := newCaptureHandler()
	logger := slog.New(handler)

	transport := NewLogging(inner, logger)
	_, err := transport.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(7)),
		Method:  "tools/call",
	})
	require.Error(t, err)

	records := handler.snapshot()
	require.Len(t, records, 2)
	assert.Equal(t, "→ request", records[0].Message)
	assert.Equal(t, "← response error", records[1].Message)
	errAttrs := attrsOf(records[1])
	assert.Equal(t, "boom", errAttrs["error"].String())
}

func TestLoggingTransport_LogsJSONRPCError(t *testing.T) {
	inner := &fakeTransport{
		requestResp: &JSONRPCResponse{
			JSONRPC: mcp.JSONRPC_VERSION,
			ID:      mcp.NewRequestId(int64(2)),
			Error: &mcp.JSONRPCErrorDetails{
				Code:    mcp.METHOD_NOT_FOUND,
				Message: "Method not found",
			},
		},
	}
	handler := newCaptureHandler()
	logger := slog.New(handler)
	transport := NewLogging(inner, logger)

	resp, err := transport.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(2)),
		Method:  "tools/missing",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)

	records := handler.snapshot()
	require.Len(t, records, 2)
	assert.Equal(t, "← response error", records[1].Message)
	errAttrs := attrsOf(records[1])
	assert.Equal(t, int64(mcp.METHOD_NOT_FOUND), errAttrs["code"].Int64())
	assert.Equal(t, "Method not found", errAttrs["error"].String())
}

func TestLoggingTransport_LogsIncomingNotifications(t *testing.T) {
	inner := &fakeTransport{}
	handler := newCaptureHandler()
	logger := slog.New(handler)
	transport := NewLogging(inner, logger)

	var received mcp.JSONRPCNotification
	called := make(chan struct{})
	transport.SetNotificationHandler(func(n mcp.JSONRPCNotification) {
		received = n
		close(called)
	})

	require.NotNil(t, inner.notifyHandler, "wrapper should register a handler on inner transport")
	notif := mcp.JSONRPCNotification{
		JSONRPC: mcp.JSONRPC_VERSION,
		Notification: mcp.Notification{
			Method: "notifications/tools/list_changed",
		},
	}
	inner.notifyHandler(notif)
	<-called

	assert.Equal(t, notif.Method, received.Method)

	records := handler.snapshot()
	require.Len(t, records, 1)
	assert.Equal(t, "← notification", records[0].Message)
	attrs := attrsOf(records[0])
	assert.Equal(t, "notifications/tools/list_changed", attrs["method"].String())
}

func TestLoggingTransport_NotificationDeliveredWithoutHandler(t *testing.T) {
	inner := &fakeTransport{}
	handler := newCaptureHandler()
	logger := slog.New(handler)
	transport := NewLogging(inner, logger)
	// Sanity: still wraps even if no user handler.
	require.NotNil(t, transport)
	require.NotNil(t, inner.notifyHandler)

	inner.notifyHandler(mcp.JSONRPCNotification{
		JSONRPC: mcp.JSONRPC_VERSION,
		Notification: mcp.Notification{
			Method: "notifications/progress",
		},
	})

	records := handler.snapshot()
	require.Len(t, records, 1)
	assert.Equal(t, "← notification", records[0].Message)
}

func TestLoggingTransport_BidirectionalPassThrough(t *testing.T) {
	inner := &fakeBidirTransport{}
	handler := newCaptureHandler()
	logger := slog.New(handler)

	transport := NewLogging(inner, logger)
	bidir, ok := transport.(BidirectionalInterface)
	require.True(t, ok, "wrapper around BidirectionalInterface must implement it")

	bidir.SetRequestHandler(func(_ context.Context, req JSONRPCRequest) (*JSONRPCResponse, error) {
		return &JSONRPCResponse{
			JSONRPC: mcp.JSONRPC_VERSION,
			ID:      req.ID,
			Result:  json.RawMessage(`{"echo":true}`),
		}, nil
	})
	require.NotNil(t, inner.requestHandler)

	resp, err := inner.requestHandler(t.Context(), JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(99)),
		Method:  "sampling/createMessage",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	records := handler.snapshot()
	require.Len(t, records, 2)
	assert.Equal(t, "← request", records[0].Message)
	assert.Equal(t, "→ response", records[1].Message)
	respAttrs := attrsOf(records[1])
	assert.Equal(t, `{"echo":true}`, respAttrs["result"].String())
}

func TestLoggingTransport_BidirectionalHandlerError(t *testing.T) {
	inner := &fakeBidirTransport{}
	handler := newCaptureHandler()
	logger := slog.New(handler)
	transport := NewLogging(inner, logger).(BidirectionalInterface)

	transport.SetRequestHandler(func(_ context.Context, _ JSONRPCRequest) (*JSONRPCResponse, error) {
		return nil, fmt.Errorf("nope")
	})
	_, err := inner.requestHandler(t.Context(), JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(1)),
		Method:  "sampling/createMessage",
	})
	require.Error(t, err)

	records := handler.snapshot()
	require.Len(t, records, 2)
	assert.Equal(t, "→ response error", records[1].Message)

	// Resetting to nil should clear the handler on the inner.
	handler.reset()
	transport.SetRequestHandler(nil)
	assert.Nil(t, inner.requestHandler)
}

func TestLoggingTransport_HTTPPassThrough(t *testing.T) {
	inner := &fakeHTTPTransport{}
	handler := newCaptureHandler()
	logger := slog.New(handler)

	transport := NewLogging(inner, logger)
	httpConn, ok := transport.(HTTPConnection)
	require.True(t, ok, "wrapper around HTTPConnection must implement it")

	httpConn.SetProtocolVersion("2025-06-18")
	assert.Equal(t, "2025-06-18", inner.protocolVersion)

	// Plain wrapper must NOT advertise BidirectionalInterface.
	_, isBidir := transport.(BidirectionalInterface)
	assert.False(t, isBidir)
}

func TestLoggingTransport_BidirectionalAndHTTP(t *testing.T) {
	inner := &fakeBidirHTTPTransport{}
	handler := newCaptureHandler()
	logger := slog.New(handler)

	transport := NewLogging(inner, logger)
	httpConn, ok := transport.(HTTPConnection)
	require.True(t, ok)
	bidir, ok := transport.(BidirectionalInterface)
	require.True(t, ok)

	httpConn.SetProtocolVersion("2025-06-18")
	assert.Equal(t, "2025-06-18", inner.protocolVersion)

	bidir.SetRequestHandler(func(_ context.Context, req JSONRPCRequest) (*JSONRPCResponse, error) {
		return &JSONRPCResponse{ID: req.ID, JSONRPC: mcp.JSONRPC_VERSION, Result: json.RawMessage(`null`)}, nil
	})
	require.NotNil(t, inner.requestHandler)
}

func TestLoggingTransport_NilLoggerUsesDefault(t *testing.T) {
	inner := &fakeTransport{requestResp: &JSONRPCResponse{JSONRPC: mcp.JSONRPC_VERSION, ID: mcp.NewRequestId(int64(1))}}
	transport := NewLogging(inner, nil)

	_, err := transport.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(1)),
		Method:  "ping",
	})
	require.NoError(t, err)
}

func TestLoggingTransport_LevelOption(t *testing.T) {
	inner := &fakeTransport{requestResp: &JSONRPCResponse{JSONRPC: mcp.JSONRPC_VERSION, ID: mcp.NewRequestId(int64(1))}}
	handler := newCaptureHandler()
	handler.level = slog.LevelInfo
	logger := slog.New(handler)
	transport := NewLogging(inner, logger, WithLoggingLevel(slog.LevelInfo))

	_, err := transport.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(1)),
		Method:  "ping",
	})
	require.NoError(t, err)

	records := handler.snapshot()
	require.Len(t, records, 2)
	for _, r := range records {
		assert.Equal(t, slog.LevelInfo, r.Level)
	}
}

func TestLoggingTransport_DisablePayloads(t *testing.T) {
	inner := &fakeTransport{
		requestResp: &JSONRPCResponse{
			JSONRPC: mcp.JSONRPC_VERSION,
			ID:      mcp.NewRequestId(int64(1)),
			Result:  json.RawMessage(`{"ok":true}`),
		},
	}
	handler := newCaptureHandler()
	logger := slog.New(handler)
	transport := NewLogging(inner, logger, WithLoggingPayloads(false))

	_, err := transport.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(1)),
		Method:  "ping",
		Params:  map[string]any{"x": 1},
	})
	require.NoError(t, err)

	records := handler.snapshot()
	require.Len(t, records, 2)
	reqAttrs := attrsOf(records[0])
	_, hasParams := reqAttrs["params"]
	assert.False(t, hasParams, "params must be omitted when payloads are disabled")
	respAttrs := attrsOf(records[1])
	_, hasResult := respAttrs["result"]
	assert.False(t, hasResult, "result must be omitted when payloads are disabled")
}

func TestLoggingTransport_DisablePayloadsHidesJSONRPCError(t *testing.T) {
	inner := &fakeTransport{
		requestResp: &JSONRPCResponse{
			JSONRPC: mcp.JSONRPC_VERSION,
			ID:      mcp.NewRequestId(int64(1)),
			Error: &mcp.JSONRPCErrorDetails{
				Code:    mcp.METHOD_NOT_FOUND,
				Message: "sensitive: user 'alice@example.com' not found",
			},
		},
	}
	handler := newCaptureHandler()
	logger := slog.New(handler)
	transport := NewLogging(inner, logger, WithLoggingPayloads(false))

	resp, err := transport.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(1)),
		Method:  "tools/missing",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error, "error must still be returned to the caller")

	records := handler.snapshot()
	require.Len(t, records, 2)
	assert.Equal(t, "← response error", records[1].Message,
		"the error marker should still be emitted so the failure is visible")
	respAttrs := attrsOf(records[1])
	_, hasCode := respAttrs["code"]
	assert.False(t, hasCode, "JSON-RPC error code must be omitted when payloads are disabled")
	_, hasErr := respAttrs["error"]
	assert.False(t, hasErr, "JSON-RPC error message must be omitted when payloads are disabled")
}

func TestLoggingTransport_DisablePayloadsBidirectionalError(t *testing.T) {
	inner := &fakeBidirTransport{}
	handler := newCaptureHandler()
	logger := slog.New(handler)
	transport := NewLogging(inner, logger, WithLoggingPayloads(false)).(BidirectionalInterface)

	transport.SetRequestHandler(func(_ context.Context, req JSONRPCRequest) (*JSONRPCResponse, error) {
		return &JSONRPCResponse{
			JSONRPC: mcp.JSONRPC_VERSION,
			ID:      req.ID,
			Error: &mcp.JSONRPCErrorDetails{
				Code:    mcp.INVALID_PARAMS,
				Message: "sensitive bidirectional error message",
			},
		}, nil
	})
	require.NotNil(t, inner.requestHandler)

	_, err := inner.requestHandler(t.Context(), JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(1)),
		Method:  "sampling/createMessage",
	})
	require.NoError(t, err)

	records := handler.snapshot()
	require.Len(t, records, 2)
	assert.Equal(t, "→ response error", records[1].Message)
	respAttrs := attrsOf(records[1])
	_, hasCode := respAttrs["code"]
	assert.False(t, hasCode)
	_, hasErr := respAttrs["error"]
	assert.False(t, hasErr)
}

func TestLoggingTransport_LevelFilteredEarly(t *testing.T) {
	// When the logger has a higher minimum level than the transport's level,
	// no records should be emitted.
	inner := &fakeTransport{requestResp: &JSONRPCResponse{JSONRPC: mcp.JSONRPC_VERSION, ID: mcp.NewRequestId(int64(1))}}
	handler := newCaptureHandler()
	handler.level = slog.LevelWarn
	logger := slog.New(handler)
	transport := NewLogging(inner, logger) // default debug level

	_, err := transport.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(1)),
		Method:  "ping",
	})
	require.NoError(t, err)
	assert.Empty(t, handler.snapshot())
}

func TestLoggingTransport_StartError(t *testing.T) {
	inner := &fakeTransport{startErr: errors.New("cannot start")}
	transport := NewLogging(inner, slog.New(newCaptureHandler()))
	err := transport.Start(t.Context())
	require.Error(t, err)
	assert.Equal(t, "cannot start", err.Error())
}
