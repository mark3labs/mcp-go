package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTransportIntegerRequestIDs(t *testing.T) {
	for _, raw := range []string{"1", "9007199254740993", "18446744073709551615", "1e1000000000000000000"} {
		t.Run(raw, func(t *testing.T) {
			for _, method := range []string{"ping", "unknown"} {
				message := `{"jsonrpc":"2.0","id":` + raw + `,"method":"` + method + `"}`
				stdio := NewStdioServer(NewMCPServer("probe", "1.0"))
				var output bytes.Buffer
				require.NoError(t, stdio.processMessage(t.Context(), message, &output))
				stdio.requestWg.Wait()
				assertResponseID(t, output.Bytes(), raw)
				httpServer := NewStreamableHTTPServer(NewMCPServer("probe", "1.0"), WithStateLess(true))
				req := httptest.NewRequest("POST", "/mcp", strings.NewReader(message))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", "application/json, text/event-stream")
				recorder := httptest.NewRecorder()
				httpServer.ServeHTTP(recorder, req)
				assertResponseID(t, recorder.Body.Bytes(), raw)
			}
		})
	}
}

func assertResponseID(t *testing.T, body []byte, raw string) {
	t.Helper()
	var response map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &response), string(body))
	require.Equal(t, raw, string(response["id"]))
}

func TestHTTPPreDispatchErrorPreservesIntegerID(t *testing.T) {
	for _, raw := range []string{"9007199254740993", "18446744073709551615", "1e1000000000000000000"} {
		t.Run(raw, func(t *testing.T) {
			httpServer := NewStreamableHTTPServer(NewMCPServer("probe", "1.0"), WithStateLess(true))
			req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":`+raw+`,"method":"tools/list"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("MCP-Protocol-Version", "2026-07-28")
			recorder := httptest.NewRecorder()
			httpServer.ServeHTTP(recorder, req)
			require.Equal(t, 400, recorder.Code)
			assertResponseID(t, recorder.Body.Bytes(), raw)
		})
	}
}
