package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

// A server request whose handler is cancelled must still be answered: the
// error response cannot be sent with the handler's cancelled context.
func TestStreamableHTTP_CancelledRequestHandlerStillResponds(t *testing.T) {
	serverResponses := make(chan JSONRPCResponse, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var message map[string]json.RawMessage
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&message)) {
			return
		}
		if _, isRequest := message["method"]; !isRequest {
			// The client's answer to the server request below.
			var response JSONRPCResponse
			raw, _ := json.Marshal(message)
			if assert.NoError(t, json.Unmarshal(raw, &response)) {
				serverResponses <- response
			}
			w.WriteHeader(http.StatusAccepted)
			return
		}

		// Ask the client for its roots, then end the stream with the
		// response, which cancels the handler still working on the request.
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":\"srv-1\",\"method\":\"roots/list\"}\n\n")
		fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n")
	}))
	defer server.Close()

	trans, err := NewStreamableHTTP(server.URL)
	require.NoError(t, err)
	require.NoError(t, trans.Start(t.Context()))
	defer trans.Close()

	trans.SetRequestHandler(func(ctx context.Context, _ JSONRPCRequest) (*JSONRPCResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})

	_, err = trans.SendRequest(t.Context(), JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(1)),
		Method:  "tools/list",
	})
	require.NoError(t, err)

	select {
	case response := <-serverResponses:
		assert.Equal(t, "srv-1", response.ID.Value())
		require.NotNil(t, response.Error)
		assert.Equal(t, mcp.REQUEST_INTERRUPTED, response.Error.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("server never received a response to its request")
	}
}
