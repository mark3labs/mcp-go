package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// h2BodySimulator simulates the behavior of Go's http2.transportResponseBody
// when the HTTP/2 stream cannot be cleanly torn down (e.g. due to cc.wmu
// contention preventing cleanupWriteRequest from closing cs.donec).
//
// In real HTTP/2, transportResponseBody.Close() ends with:
//
//	select {
//	case <-cs.donec:      // closed by cleanupWriteRequest (needs cc.wmu)
//	case <-cs.ctx.Done(): // request context canceled
//	case <-cs.reqCancel:  // request canceled
//	}
//
// When cc.wmu is contended, cs.donec never closes, so the only escape is
// cs.ctx.Done(). This simulator reproduces that behavior by blocking Close()
// until the request context is canceled.
type h2BodySimulator struct {
	data   io.Reader
	reader *blockingSSEReader
	readCh chan struct{} // closed on first Close() to unblock pending reads
	reqCtx context.Context
	once   sync.Once
}

func newH2BodySimulator(reqCtx context.Context, ssePayload []byte) *h2BodySimulator {
	reader := newBlockingSSEReader(ssePayload)
	return &h2BodySimulator{
		data:   reader,
		reader: reader,
		readCh: make(chan struct{}),
		reqCtx: reqCtx,
	}
}

func (b *h2BodySimulator) Read(p []byte) (int, error) {
	n, err := b.data.Read(p)
	if n > 0 {
		return n, err
	}
	// SSE data exhausted. Block until Close() is called, simulating an
	// open SSE stream where the server hasn't sent END_STREAM.
	<-b.readCh
	return 0, io.EOF
}

func (b *h2BodySimulator) Close() error {
	// Unblock any pending Read() — mirrors bufPipe.BreakWithError in real HTTP/2.
	b.once.Do(func() {
		close(b.readCh)
		close(b.reader.done)
	})

	// Simulate the blocking select in http2.transportResponseBody.Close():
	//   select {
	//   case <-cs.donec:      // never fires (cc.wmu contention)
	//   case <-cs.ctx.Done(): // fires only if cancel() was called
	//   }
	//
	// With the buggy LIFO defer ordering in SendRequest:
	//   defer cancel()          // registered 1st → runs 2nd
	//   defer resp.Body.Close() // registered 2nd → runs 1st
	//
	// Close() runs BEFORE cancel(), so ctx.Done() hasn't fired yet.
	// This blocks indefinitely — the exact production hang.
	<-b.reqCtx.Done()
	return nil
}

// blockingSSEReader is an io.Reader that returns SSE data then blocks until
// its done channel is closed (simulating an open SSE stream).
type blockingSSEReader struct {
	buf    []byte
	offset int
	done   chan struct{}
}

func newBlockingSSEReader(data []byte) *blockingSSEReader {
	return &blockingSSEReader{buf: data, done: make(chan struct{})}
}

func (r *blockingSSEReader) Read(p []byte) (int, error) {
	if r.offset >= len(r.buf) {
		// All data consumed; block until done is closed.
		<-r.done
		return 0, io.EOF
	}
	n := copy(p, r.buf[r.offset:])
	r.offset += n
	return n, nil
}

// mockH2Transport is an http.RoundTripper that simulates an HTTP/2 server
// returning SSE responses with a body that exhibits the HTTP/2 Close() blocking
// behavior when cs.donec can't close due to cc.wmu contention.
type mockH2Transport struct{}

func (rt *mockH2Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Parse the JSON-RPC request to get the ID.
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	var rpcReq map[string]any
	if err := json.Unmarshal(body, &rpcReq); err != nil {
		return nil, err
	}

	id := rpcReq["id"]

	// Build JSON-RPC response.
	rpcResp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"serverInfo": map[string]any{
				"name":    "mock-h2-server",
				"version": "1.0",
			},
		},
	}
	data, err := json.Marshal(rpcResp)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON-RPC response: %w", err)
	}
	ssePayload := fmt.Sprintf("event: message\ndata: %s\n\n", data)

	return &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"Content-Type": {"text/event-stream"},
		},
		Body: newH2BodySimulator(req.Context(), []byte(ssePayload)),
	}, nil
}

// TestStreamableHTTP_CloseBlocksBeforeCancel demonstrates that SendRequest
// hangs when resp.Body.Close() blocks waiting for the request context to be
// canceled, but cancel() runs AFTER Close() due to Go's LIFO defer ordering.
//
// This is a deterministic reproduction of the production hang observed on
// HTTP/2 connections to MCP servers that leave SSE streams open after sending
// the JSON-RPC response. In real HTTP/2, Close() blocks in a select waiting
// for cs.donec (stream cleanup) or cs.ctx.Done() (context cancellation).
// When cc.wmu contention prevents cleanup from completing, ctx.Done() is
// the only escape — but it never fires because cancel() is deferred AFTER
// Close() and Go defers run LIFO.
//
// The fix is to call cancel() before Close():
//
//	defer func() { cancel(); resp.Body.Close() }()
func TestStreamableHTTP_CloseBlocksBeforeCancel(t *testing.T) {
	mockTransport := &mockH2Transport{}
	client := &http.Client{Transport: mockTransport}

	// Use a dummy URL — the mock transport intercepts all requests.
	transport, err := NewStreamableHTTP("http://mock-h2-server/mcp",
		WithHTTPBasicClient(client),
	)
	require.NoError(t, err)
	require.NoError(t, transport.Start(context.Background()))
	defer transport.Close()

	done := make(chan struct{})
	var sendErr error
	var resp *JSONRPCResponse

	go func() {
		defer close(done)
		resp, sendErr = transport.SendRequest(context.Background(), JSONRPCRequest{
			JSONRPC: mcp.JSONRPC_VERSION,
			ID:      mcp.NewRequestId(1),
			Method:  "initialize",
			Params: json.RawMessage(`{
				"protocolVersion": "2025-03-26",
				"capabilities": {},
				"clientInfo": {"name": "test-client", "version": "1.0"}
			}`),
		})
	}()

	select {
	case <-done:
		require.NoError(t, sendErr)
		require.NotNil(t, resp)
	case <-time.After(5 * time.Second):
		t.Fatal("BUG: SendRequest hung for 5s.\n" +
			"resp.Body.Close() is blocking because it waits on ctx.Done(),\n" +
			"but cancel() runs AFTER Close() due to LIFO defer ordering.\n\n" +
			"Fix: change 'defer resp.Body.Close()' to 'defer func() { cancel(); resp.Body.Close() }()'\n" +
			"in SendRequest, SendNotification, and sendResponseToServer.")
	}
}
