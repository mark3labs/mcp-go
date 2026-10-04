package transport

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// getErrorBody finishes reads promptly but blocks Close() until the request
// context is canceled, like http2.transportResponseBody under cc.wmu contention.
type getErrorBody struct {
	data   []byte
	offset int
	reqCtx context.Context
}

func (b *getErrorBody) Read(p []byte) (int, error) {
	if b.offset >= len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.offset:])
	b.offset += n
	return n, nil
}

func (b *getErrorBody) Close() error {
	<-b.reqCtx.Done()
	return nil
}

// mockGETErrorTransport returns a GET error response whose body blocks in Close()
// until the request context is canceled.
type mockGETErrorTransport struct{}

func (mockGETErrorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return &http.Response{
			StatusCode: http.StatusMethodNotAllowed,
			Body:       http.NoBody,
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusInternalServerError,
		Header:     http.Header{"Content-Type": {"text/plain"}},
		Body: &getErrorBody{
			data:   []byte("error"),
			reqCtx: req.Context(),
		},
	}, nil
}

// TestStreamableHTTP_GETCloseCancelBeforeClose ensures createGETConnectionToServer
// cancels its request context before closing the body on error responses.
func TestStreamableHTTP_GETCloseCancelBeforeClose(t *testing.T) {
	client := &http.Client{Transport: mockGETErrorTransport{}}

	transport, err := NewStreamableHTTP("http://mock-get-server/mcp",
		WithHTTPBasicClient(client),
	)
	require.NoError(t, err)
	require.NoError(t, transport.Start(t.Context()))
	defer transport.Close()

	done := make(chan struct{})
	var connectErr error

	go func() {
		defer close(done)
		connectErr = transport.createGETConnectionToServer(t.Context())
	}()

	select {
	case <-done:
		require.Error(t, connectErr)
	case <-time.After(5 * time.Second):
		t.Fatal("BUG: createGETConnectionToServer hung for 5s on GET error response.\n" +
			"resp.Body.Close() blocks until the request context is canceled, but cancel() " +
			"must run before Close() (see SendRequest).")
	}
}
