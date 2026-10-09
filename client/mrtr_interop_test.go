package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type acceptingElicitationHandler struct{}

func (acceptingElicitationHandler) Elicit(context.Context, mcp.ElicitationRequest) (*mcp.ElicitationResult, error) {
	return &mcp.ElicitationResult{ElicitationResponse: mcp.ElicitationResponse{
		Action:  mcp.ElicitationResponseActionAccept,
		Content: map[string]any{"confirmed": true},
	}}, nil
}

// Other SDKs answer a tools/call that needs input with an InputRequiredResult,
// which has no content. The client must still recognise it, fulfil the input
// requests and retry, rather than reject the result for its missing content.
func TestCallToolInputRequiredWithoutContent(t *testing.T) {
	const state = `{"step":1}`
	var retry struct {
		RequestState   string         `json:"requestState"`
		InputResponses map[string]any `json:"inputResponses"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading request: %v", err)
			return
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Errorf("decoding request: %v", err)
			return
		}
		if request.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		var result any
		switch request.Method {
		case string(mcp.MethodServerDiscover):
			result = map[string]any{
				"supportedVersions": []string{mcp.ProtocolVersion20260728},
				"capabilities":      map[string]any{"tools": map[string]any{}},
				"serverInfo":        map[string]any{"name": "mrtr", "version": "1.0.0"},
			}
		case string(mcp.MethodToolsCall):
			var params struct {
				RequestState string `json:"requestState"`
			}
			if err := json.Unmarshal(request.Params, &params); err != nil {
				t.Errorf("decoding params: %v", err)
				return
			}
			if params.RequestState == "" {
				result = map[string]any{
					"resultType": "input_required",
					"inputRequests": map[string]any{
						"confirm": map[string]any{
							"method": "elicitation/create",
							"params": map[string]any{
								"message": "Continue?",
								"requestedSchema": map[string]any{
									"type":       "object",
									"properties": map[string]any{"confirmed": map[string]any{"type": "boolean"}},
								},
							},
						},
					},
					"requestState": state,
				}
				break
			}
			if err := json.Unmarshal(request.Params, &retry); err != nil {
				t.Errorf("decoding retry: %v", err)
				return
			}
			result = map[string]any{
				"resultType": "complete",
				"content":    []any{map[string]any{"type": "text", "text": "done"}},
			}
		default:
			t.Errorf("unexpected method %q", request.Method)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	defer server.Close()

	httpTransport, err := transport.NewStreamableHTTP(server.URL)
	require.NoError(t, err)
	client := NewClient(httpTransport, WithElicitationHandler(acceptingElicitationHandler{}))
	defer client.Close()
	require.NoError(t, client.Start(t.Context()))
	initialized, err := client.Initialize(t.Context(), mcp.InitializeRequest{})
	require.NoError(t, err)
	require.Equal(t, mcp.ProtocolVersion20260728, initialized.ProtocolVersion)

	result, err := client.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "needs_confirmation"}})
	require.NoError(t, err)
	require.Len(t, result.Content, 1)
	assert.Equal(t, "done", result.Content[0].(mcp.TextContent).Text)

	assert.Equal(t, state, retry.RequestState, "the request state must come back unchanged")
	assert.Contains(t, retry.InputResponses, "confirm")
}
