package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSSEServerRejectsTrailingJSONValues(t *testing.T) {
	tests := []struct {
		name       string
		trailing   string
		statusCode int
	}{
		{name: "second JSON value", trailing: " {}", statusCode: http.StatusBadRequest},
		{name: "non-JSON data", trailing: " garbage", statusCode: http.StatusBadRequest},
		{name: "whitespace", trailing: " \n\t ", statusCode: http.StatusAccepted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sseServer := NewSSEServer(NewMCPServer("test", "1.0.0"))
			ts := httptest.NewServer(sseServer)
			t.Cleanup(ts.Close)
			sseServer.baseURL = ts.URL

			resp, err := http.Get(ts.URL + "/sse")
			require.NoError(t, err)
			t.Cleanup(func() { _ = resp.Body.Close() })
			endpoint, err := readSSEEvent(resp)
			require.NoError(t, err)
			messageURL := strings.TrimSpace(strings.SplitN(strings.SplitN(endpoint, "data: ", 2)[1], "\n", 2)[0])

			postResp, err := http.Post(messageURL, "application/json", strings.NewReader(
				`{"jsonrpc":"2.0","id":1,"method":"ping"}`+tt.trailing,
			))
			require.NoError(t, err)
			defer postResp.Body.Close()
			require.Equal(t, tt.statusCode, postResp.StatusCode)
		})
	}
}
