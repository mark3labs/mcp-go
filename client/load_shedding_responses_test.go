package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

type retryRootsHandler struct{}

// ListRoots supplies the answers for the fresh, nonempty input-request round.
func (retryRootsHandler) ListRoots(context.Context, mcp.ListRootsRequest) (*mcp.ListRootsResult, error) {
	return &mcp.ListRootsResult{Roots: []mcp.Root{{URI: "file:///fresh", Name: "fresh"}}}, nil
}

// TestClientPreservesResponsesAcrossLoadShedding verifies answer retention and replacement on the HTTP wire.
func TestClientPreservesResponsesAcrossLoadShedding(t *testing.T) {
	tests := []struct {
		name, method, result string
		call                 func(*Client, mcp.MultiRoundTripParams) error
	}{
		{"tool", "tools/call", `{"content":[]}`, func(c *Client, p mcp.MultiRoundTripParams) error {
			_, err := c.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "resume", MultiRoundTripParams: p}})
			return err
		}},
		{"prompt", "prompts/get", `{"messages":[]}`, func(c *Client, p mcp.MultiRoundTripParams) error {
			_, err := c.GetPrompt(t.Context(), mcp.GetPromptRequest{Params: mcp.GetPromptParams{Name: "resume", MultiRoundTripParams: p}})
			return err
		}},
		{"resource", "resources/read", `{"contents":[]}`, func(c *Client, p mcp.MultiRoundTripParams) error {
			_, err := c.ReadResource(t.Context(), mcp.ReadResourceRequest{Params: mcp.ReadResourceParams{URI: "file:///resume", MultiRoundTripParams: p}})
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var rounds []mcp.MultiRoundTripParams
			httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				var request struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
					Params json.RawMessage `json:"params"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decode HTTP request: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if request.ID == nil {
					w.WriteHeader(http.StatusAccepted)
					return
				}
				var result interface{}
				switch request.Method {
				case string(mcp.MethodServerDiscover):
					result = map[string]interface{}{
						"supportedVersions": []string{mcp.ProtocolVersion20260728},
						"capabilities":      map[string]interface{}{},
						"serverInfo":        map[string]string{"name": "retry", "version": "1"},
					}
				case tt.method:
					var params mcp.MultiRoundTripParams
					if err := json.Unmarshal(request.Params, &params); err != nil {
						t.Errorf("decode round-trip params: %v", err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					mu.Lock()
					round := len(rounds)
					rounds = append(rounds, params)
					mu.Unlock()
					switch round {
					case 0:
						result = mcp.NewInputRequiredResult(mcp.InputRequests{}, "shed-initial")
					case 1:
						result = mcp.NewInputRequiredResult(mcp.InputRequests{"fresh": mcp.NewRootsInputRequest()}, "fresh-request")
					case 2:
						result = mcp.NewInputRequiredResult(mcp.InputRequests{}, "shed-fresh")
					default:
						result = json.RawMessage(tt.result)
					}
				default:
					t.Errorf("unexpected method: %s", request.Method)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": request.ID, "result": result}); err != nil {
					t.Errorf("encode response: %v", err)
				}
			}))
			t.Cleanup(httpServer.Close)
			tr, err := transport.NewStreamableHTTP(httpServer.URL)
			require.NoError(t, err)
			c := NewClient(tr, WithRootsHandler(retryRootsHandler{}))
			t.Cleanup(func() { require.NoError(t, c.Close()) })
			require.NoError(t, c.Start(t.Context()))
			_, err = c.Initialize(t.Context(), mcp.InitializeRequest{})
			require.NoError(t, err)
			initial := mcp.MultiRoundTripParams{RequestState: "caller-state", InputResponses: mcp.InputResponses{"old": mcp.NewRootsInputResponse(mcp.ListRootsResult{Roots: []mcp.Root{{URI: "file:///original", Name: "original"}}})}}
			require.NoError(t, tt.call(c, initial))
			mu.Lock()
			defer mu.Unlock()
			require.Len(t, rounds, 4)
			require.Equal(t, []string{"caller-state", "shed-initial", "fresh-request", "shed-fresh"}, []string{rounds[0].RequestState, rounds[1].RequestState, rounds[2].RequestState, rounds[3].RequestState})
			initialJSON, err := json.Marshal(initial.InputResponses)
			require.NoError(t, err)
			firstJSON, err := json.Marshal(rounds[0].InputResponses)
			require.NoError(t, err)
			require.JSONEq(t, string(initialJSON), string(firstJSON))
			require.Equal(t, rounds[0].InputResponses, rounds[1].InputResponses, "empty requests retain caller answers")
			require.NotContains(t, rounds[2].InputResponses, "old", "fresh input requests replace prior answers")
			freshJSON, err := json.Marshal(rounds[2].InputResponses)
			require.NoError(t, err)
			require.JSONEq(t, `{"fresh":{"roots":[{"uri":"file:///fresh","name":"fresh"}]}}`, string(freshJSON))
			require.Equal(t, rounds[2].InputResponses, rounds[3].InputResponses, "empty requests retain newly fulfilled answers")
		})
	}
}
