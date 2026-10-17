package transport

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// newBareSSE builds an SSE transport with no live connection so individual
// branches (accessors, event handling, request error paths) can be exercised
// deterministically.
func newBareSSE() *SSE {
	baseURL, _ := url.Parse("http://127.0.0.1:1/")
	return &SSE{
		baseURL:    baseURL,
		httpClient: http.DefaultClient,
		responses:  make(map[string]chan *JSONRPCResponse),
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// TestSSE_Accessors verifies the no-connection accessors: session ID, OAuth
// state, protocol version, endpoint, and base URL.
func TestSSE_Accessors(t *testing.T) {
	sse := newBareSSE()
	require.Equal(t, "", sse.GetSessionId())
	require.Nil(t, sse.GetOAuthHandler())
	require.False(t, sse.IsOAuthEnabled())

	// Protocol version is stored for later header attachment.
	sse.SetProtocolVersion("2025-03-26")
	require.Equal(t, "2025-03-26", sse.protocolVersion.Load())

	// Endpoint and base URL accessors.
	endpoint, _ := url.Parse("http://127.0.0.1:1/message")
	sse.endpoint = endpoint
	require.Equal(t, endpoint, sse.GetEndpoint())
	require.Equal(t, sse.baseURL, sse.GetBaseURL())
}

// TestSSE_ClientOptions verifies that every WithSSE* option is applied when
// constructing the transport, including the fallbacks for a zero endpoint
// timeout and a nil logger.
func TestSSE_ClientOptions(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := &http.Client{}
	headers := map[string]string{"X-Test": "1"}
	headerFunc := func(ctx context.Context) map[string]string {
		return map[string]string{"X-Func": "2"}
	}

	sse, err := NewSSE(
		"http://127.0.0.1:1/",
		WithSSELogger(logger),
		WithHeaders(headers),
		WithHeaderFunc(headerFunc),
		WithHTTPClient(client),
		WithEndpointTimeout(10*time.Second),
	)
	require.NoError(t, err)
	require.Same(t, logger, sse.logger)
	require.Equal(t, headers, sse.headers)
	require.NotNil(t, sse.headerFunc)
	require.Same(t, client, sse.httpClient)
	require.Equal(t, 10*time.Second, sse.endpointTimeout)

	// Non-positive timeout keeps the default.
	sse, err = NewSSE("http://127.0.0.1:1/", WithEndpointTimeout(0))
	require.NoError(t, err)
	require.NotZero(t, sse.endpointTimeout)

	// Nil logger falls back to slog.Default.
	sse, err = NewSSE("http://127.0.0.1:1/", WithSSELogger(nil))
	require.NoError(t, err)
	require.Same(t, slog.Default(), sse.logger)
}

// TestSSE_SetConnectionLostHandler verifies the connection-lost callback is
// stored and invoked with the connection error.
func TestSSE_SetConnectionLostHandler(t *testing.T) {
	sse := newBareSSE()
	var got error
	sse.SetConnectionLostHandler(func(err error) { got = err })
	require.NotNil(t, sse.onConnectionLost)

	sse.onConnectionLost(errors.New("connection lost"))
	require.EqualError(t, got, "connection lost")
}

// TestSSE_HandleSSEEvent_MessageInvalidJSON verifies a message event with
// invalid JSON is ignored without panicking.
func TestSSE_HandleSSEEvent_MessageInvalidJSON(t *testing.T) {
	sse := newBareSSE()
	require.NotPanics(t, func() {
		sse.handleSSEEvent("message", "not-json", make(chan struct{}), &sync.Once{})
	})
}

// TestSSE_HandleSSEEvent_NotificationWithoutHandler verifies a notification
// message with no registered handler is dropped without panicking.
func TestSSE_HandleSSEEvent_NotificationWithoutHandler(t *testing.T) {
	sse := newBareSSE()
	require.NotPanics(t, func() {
		sse.handleSSEEvent("message", `{"jsonrpc":"2.0","method":"notify"}`, make(chan struct{}), &sync.Once{})
	})
}

// TestSSE_HandleSSEEvent_EndpointParseError verifies an unparsable endpoint
// event is ignored and leaves the endpoint unset.
func TestSSE_HandleSSEEvent_EndpointParseError(t *testing.T) {
	sse := newBareSSE()

	// An unparsable endpoint event must be ignored and leave endpoint unset.
	require.NotPanics(t, func() {
		sse.handleSSEEvent("endpoint", "http://[::1", make(chan struct{}), &sync.Once{})
	})
	require.Nil(t, sse.endpoint)
}

// TestSSE_HandleSSEEvent_UnknownResponseID verifies a message whose ID has no
// pending response is a no-op.
func TestSSE_HandleSSEEvent_UnknownResponseID(t *testing.T) {
	sse := newBareSSE()

	// No pending request with this ID; delivery must be a no-op.
	require.NotPanics(t, func() {
		sse.handleSSEEvent("message", `{"jsonrpc":"2.0","id":42,"result":{}}`, make(chan struct{}), &sync.Once{})
	})
}

// TestSSE_SendRequest_NotStarted verifies SendRequest fails with
// "transport not started yet" before Start has run.
func TestSSE_SendRequest_NotStarted(t *testing.T) {
	_, err := newBareSSE().SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)),
	})
	require.EqualError(t, err, "transport not started yet")
}

// TestSSE_SendRequest_Closed verifies SendRequest fails with
// "transport has been closed" after Close has run.
func TestSSE_SendRequest_Closed(t *testing.T) {
	sse := newBareSSE()
	sse.started.Store(true)
	sse.closed.Store(true)

	_, err := sse.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)),
	})
	require.EqualError(t, err, "transport has been closed")
}

// TestSSE_SendRequest_EndpointNotReceived verifies SendRequest fails with
// "endpoint not received" when no endpoint event has arrived.
func TestSSE_SendRequest_EndpointNotReceived(t *testing.T) {
	sse := newBareSSE()
	sse.started.Store(true)

	_, err := sse.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)),
	})
	require.EqualError(t, err, "endpoint not received")
}

// TestSSE_SendRequest_MarshalError verifies SendRequest wraps JSON marshaling
// failures of the request.
func TestSSE_SendRequest_MarshalError(t *testing.T) {
	sse := newBareSSE()
	sse.started.Store(true)
	sse.endpoint = sse.baseURL

	_, err := sse.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)), Params: make(chan int),
	})
	require.ErrorContains(t, err, "failed to marshal request")
}

// errorTransport is an http.RoundTripper that always fails with a fixed error,
// making connection-error tests deterministic without relying on a particular
// port being unbound.
type errorTransport struct{ err error }

// RoundTrip always fails with the transport's configured error, regardless of
// the request.
func (t errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
}

// TestSSE_SendRequest_ConnectionError verifies SendRequest wraps HTTP
// connection failures using the injected errorTransport.
func TestSSE_SendRequest_ConnectionError(t *testing.T) {
	sse := newBareSSE()
	sse.started.Store(true)
	sse.endpoint = sse.baseURL
	sse.httpClient = &http.Client{Transport: errorTransport{err: errors.New("connection refused")}}

	_, err := sse.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)),
	})
	require.ErrorContains(t, err, "failed to send request")
}

// TestSSE_SendRequest_UnauthorizedWithoutOAuth verifies a 401 response is
// surfaced as AuthorizationRequiredError when OAuth is not configured.
func TestSSE_SendRequest_UnauthorizedWithoutOAuth(t *testing.T) {
	sse := newBareSSE()
	sse.started.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	sse.endpoint = mustURL(t, server.URL)

	_, err := sse.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)),
	})
	var authErr *AuthorizationRequiredError
	require.ErrorAs(t, err, &authErr)
}

// TestSSE_SendRequest_ServerError verifies a 500 response is surfaced with the
// status code in the error.
func TestSSE_SendRequest_ServerError(t *testing.T) {
	sse := newBareSSE()
	sse.started.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	sse.endpoint = mustURL(t, server.URL)

	_, err := sse.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)),
	})
	require.ErrorContains(t, err, "request failed with status 500")
}

// TestSSE_SendRequest_DeadlineAlreadyPassed verifies a request whose context
// deadline already passed fails with context.DeadlineExceeded.
func TestSSE_SendRequest_DeadlineAlreadyPassed(t *testing.T) {
	sse := newBareSSE()
	sse.started.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	sse.endpoint = mustURL(t, server.URL)

	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()

	_, err := sse.SendRequest(ctx, JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)),
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// TestSSE_SendRequest_HeaderOptions verifies static headers, the dynamic
// header function, the protocol version, and the host override are all
// attached to the outbound HTTP request.
func TestSSE_SendRequest_HeaderOptions(t *testing.T) {
	sse := newBareSSE()
	sse.started.Store(true)
	// The server handler runs on a different goroutine than the assertions
	// below; guard the captured request data with a mutex.
	var gotMu sync.Mutex
	var gotHeader http.Header
	var gotHost string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMu.Lock()
		gotHeader = r.Header.Clone()
		gotHost = r.Host
		gotMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	sse.endpoint = mustURL(t, server.URL)
	sse.headers = map[string]string{"X-Static": "1"}
	sse.headerFunc = func(ctx context.Context) map[string]string {
		return map[string]string{"X-Func": "2"}
	}
	sse.protocolVersion.Store("2025-03-26")
	sse.host = "example.com"

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	// The server returns 200 without delivering an SSE response, so the
	// request times out waiting; the headers are what this test verifies.
	_, err := sse.SendRequest(ctx, JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)),
	})
	require.ErrorContains(t, err, "timeout waiting for SSE response")

	gotMu.Lock()
	defer gotMu.Unlock()
	require.Equal(t, "1", gotHeader.Get("X-Static"))
	require.Equal(t, "2", gotHeader.Get("X-Func"))
	require.Equal(t, "2025-03-26", gotHeader.Get(HeaderKeyProtocolVersion))
	require.Equal(t, "example.com", gotHost)
}

// chanClosingTransport closes the request's registered response channel after
// the server responds, modeling Close() racing with an in-flight delivery.
type chanClosingTransport struct {
	base  http.RoundTripper
	sse   *SSE
	idKey string
}

// RoundTrip delegates to the base transport and then closes the registered
// response channel, modeling Close racing an in-flight delivery.
func (t *chanClosingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	t.sse.mu.RLock()
	ch := t.sse.responses[t.idKey]
	t.sse.mu.RUnlock()
	if ch != nil {
		close(ch)
	}
	return resp, err
}

// TestSSE_SendRequest_ResponseChanClosed verifies SendRequest reports
// "connection has been closed" when the response channel is closed before
// delivery.
func TestSSE_SendRequest_ResponseChanClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sse := newBareSSE()
	sse.started.Store(true)
	sse.endpoint = mustURL(t, server.URL)
	sse.responseTimeout = time.Minute
	idKey := mcp.NewRequestId(int64(1)).String()
	sse.httpClient = &http.Client{Transport: &chanClosingTransport{
		base:  http.DefaultTransport,
		sse:   sse,
		idKey: idKey,
	}}

	_, err := sse.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)),
	})
	require.EqualError(t, err, "connection has been closed")
}

// TestSSE_SendNotification_MarshalError verifies SendNotification wraps JSON
// marshaling failures of the notification.
func TestSSE_SendNotification_MarshalError(t *testing.T) {
	sse := newBareSSE()
	sse.endpoint = sse.baseURL

	err := sse.SendNotification(t.Context(), mcp.JSONRPCNotification{
		JSONRPC: "2.0",
		Notification: mcp.Notification{
			Method: "test",
			Params: mcp.NotificationParams{AdditionalFields: map[string]any{
				"bad": make(chan int),
			}},
		},
	})
	require.ErrorContains(t, err, "failed to marshal notification")
}

// TestSSE_SendNotification_ConnectionError verifies SendNotification wraps
// HTTP connection failures using the injected errorTransport.
func TestSSE_SendNotification_ConnectionError(t *testing.T) {
	sse := newBareSSE()
	sse.endpoint = sse.baseURL
	sse.httpClient = &http.Client{Transport: errorTransport{err: errors.New("connection refused")}}

	err := sse.SendNotification(t.Context(), mcp.JSONRPCNotification{
		JSONRPC: "2.0",
		Notification: mcp.Notification{
			Method: "test",
		},
	})
	require.ErrorContains(t, err, "failed to send notification")
}

// TestSSE_SendNotification_UnauthorizedWithoutOAuth verifies a 401 response to
// a notification is surfaced as AuthorizationRequiredError when OAuth is not
// configured.
func TestSSE_SendNotification_UnauthorizedWithoutOAuth(t *testing.T) {
	sse := newBareSSE()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	sse.endpoint = mustURL(t, server.URL)

	err := sse.SendNotification(t.Context(), mcp.JSONRPCNotification{
		JSONRPC: "2.0",
		Notification: mcp.Notification{
			Method: "test",
		},
	})
	var authErr *AuthorizationRequiredError
	require.ErrorAs(t, err, &authErr)
}

// TestSSE_SendNotification_ServerError verifies a 500 response to a
// notification is surfaced with the status code in the error.
func TestSSE_SendNotification_ServerError(t *testing.T) {
	sse := newBareSSE()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	sse.endpoint = mustURL(t, server.URL)

	err := sse.SendNotification(t.Context(), mcp.JSONRPCNotification{
		JSONRPC: "2.0",
		Notification: mcp.Notification{
			Method: "test",
		},
	})
	require.ErrorContains(t, err, "notification failed with status 500")
}

// TestSSE_SendNotification_Success verifies a notification accepted by the
// server returns no error.
func TestSSE_SendNotification_Success(t *testing.T) {
	sse := newBareSSE()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	sse.endpoint = mustURL(t, server.URL)

	err := sse.SendNotification(t.Context(), mcp.JSONRPCNotification{
		JSONRPC: "2.0",
		Notification: mcp.Notification{
			Method: "test",
		},
	})
	require.NoError(t, err)
}

// TestSSE_Close_WithoutStream verifies Close without a live stream closes and
// drops pending response channels and is idempotent.
func TestSSE_Close_WithoutStream(t *testing.T) {
	sse := newBareSSE()
	pending := make(chan *JSONRPCResponse)
	sse.responses["pending"] = pending

	require.NoError(t, sse.Close())
	require.True(t, sse.closed.Load())

	// The pending channel must be closed and dropped from the map.
	select {
	case _, ok := <-pending:
		require.False(t, ok)
	default:
		t.Fatal("pending response channel was not closed")
	}
	require.Empty(t, sse.responses)

	// Second close is a no-op.
	require.NoError(t, sse.Close())
}

// TestSSE_Start_AlreadyStarted verifies Start is a no-op returning no error
// when the transport is already started.
func TestSSE_Start_AlreadyStarted(t *testing.T) {
	sse := newBareSSE()
	sse.started.Store(true)

	require.NoError(t, sse.Start(t.Context()))
}

// mustURL parses s or fails the test.
func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	require.NoError(t, err)
	return u
}
