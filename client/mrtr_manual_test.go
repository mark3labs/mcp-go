package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

type recordingElicitationHandler struct{ calls atomic.Int32 }

// Elicit counts the call and accepts the request.
func (h *recordingElicitationHandler) Elicit(context.Context, mcp.ElicitationRequest) (*mcp.ElicitationResult, error) {
	h.calls.Add(1)
	return &mcp.ElicitationResult{ElicitationResponse: mcp.ElicitationResponse{Action: mcp.ElicitationResponseActionAccept}}, nil
}

// TestManualInputRoundTrips verifies that WithManualInputRoundTrips hands input-required results to the caller.
func TestManualInputRoundTrips(t *testing.T) {
	type outcome struct {
		err        error
		needsInput bool
		requests   mcp.InputRequests
		state      string
	}
	methods := []struct {
		name, method, final string
		call                func(*Client, mcp.MultiRoundTripParams) outcome
	}{
		{"tool", "tools/call", `{"content":[]}`, func(c *Client, p mcp.MultiRoundTripParams) outcome {
			r, err := c.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "x", MultiRoundTripParams: p}})
			if err != nil {
				return outcome{err: err}
			}
			return outcome{nil, r.NeedsInput(), r.InputRequests, r.RequestState}
		}},
		{"prompt", "prompts/get", `{"messages":[]}`, func(c *Client, p mcp.MultiRoundTripParams) outcome {
			r, err := c.GetPrompt(t.Context(), mcp.GetPromptRequest{Params: mcp.GetPromptParams{Name: "x", MultiRoundTripParams: p}})
			if err != nil {
				return outcome{err: err}
			}
			return outcome{nil, r.NeedsInput(), r.InputRequests, r.RequestState}
		}},
		{"resource", "resources/read", `{"contents":[]}`, func(c *Client, p mcp.MultiRoundTripParams) outcome {
			r, err := c.ReadResource(t.Context(), mcp.ReadResourceRequest{Params: mcp.ReadResourceParams{URI: "file:///x", MultiRoundTripParams: p}})
			if err != nil {
				return outcome{err: err}
			}
			return outcome{nil, r.NeedsInput(), r.InputRequests, r.RequestState}
		}},
	}
	elicit := mcp.InputRequests{"q": mcp.NewElicitationInputRequest(mcp.ElicitationParams{Message: "name?"})}

	tests := []struct {
		name      string
		manual    bool
		handler   bool
		maxTrips  int
		requests  mcp.InputRequests
		retry     bool
		wantErr   string
		wantCalls int
		wantHandl int32
	}{
		{name: "manual returns input request unchanged", manual: true, handler: true, requests: elicit, wantCalls: 1},
		{name: "manual ignores max round trips", manual: true, maxTrips: 1, requests: elicit, wantCalls: 1},
		{name: "default without handler fails", requests: elicit, wantErr: "no elicitation handler configured", wantCalls: 1},
		{name: "default with handler retries automatically", handler: true, requests: elicit, wantCalls: 2, wantHandl: 1},
		{name: "manual caller-driven retry", manual: true, handler: true, requests: elicit, retry: true, wantCalls: 2},
		{name: "manual empty input requests returned unchanged", manual: true, requests: mcp.InputRequests{}, wantCalls: 1},
	}

	for _, m := range methods {
		for _, tt := range tests {
			t.Run(m.name+"/"+tt.name, func(t *testing.T) {
				var mu sync.Mutex
				var seen []mcp.MultiRoundTripParams
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var req struct {
						ID     json.RawMessage `json:"id"`
						Method string          `json:"method"`
						Params json.RawMessage `json:"params"`
					}
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					if req.ID == nil {
						w.WriteHeader(http.StatusAccepted)
						return
					}
					var result any
					switch req.Method {
					case string(mcp.MethodServerDiscover):
						result = map[string]any{
							"supportedVersions": []string{mcp.ProtocolVersion20260728},
							"capabilities":      map[string]any{},
							"serverInfo":        map[string]string{"name": "manual", "version": "1"},
						}
					case m.method:
						var p mcp.MultiRoundTripParams
						if err := json.Unmarshal(req.Params, &p); err != nil {
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						mu.Lock()
						seen = append(seen, p)
						n := len(seen)
						mu.Unlock()
						if n == 1 {
							result = mcp.NewInputRequiredResult(tt.requests, "s1")
						} else {
							result = json.RawMessage(m.final)
						}
					default:
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
				}))
				t.Cleanup(srv.Close)

				handler := &recordingElicitationHandler{}
				opts := []ClientOption{}
				if tt.manual {
					opts = append(opts, WithManualInputRoundTrips())
				}
				if tt.handler {
					opts = append(opts, WithElicitationHandler(handler))
				}
				if tt.maxTrips > 0 {
					opts = append(opts, WithMaxInputRoundTrips(tt.maxTrips))
				}
				tr, err := transport.NewStreamableHTTP(srv.URL)
				require.NoError(t, err)
				c := NewClient(tr, opts...)
				t.Cleanup(func() { require.NoError(t, c.Close()) })
				require.NoError(t, c.Start(t.Context()))
				_, err = c.Initialize(t.Context(), mcp.InitializeRequest{})
				require.NoError(t, err)

				got := m.call(c, mcp.MultiRoundTripParams{})
				firstCalls := tt.wantCalls
				if tt.retry {
					firstCalls = 1
				}
				mu.Lock()
				require.Len(t, seen, firstCalls)
				mu.Unlock()
				assert.Equal(t, tt.wantHandl, handler.calls.Load())
				if tt.wantErr != "" {
					require.ErrorContains(t, got.err, tt.wantErr)
					return
				}
				require.NoError(t, got.err)
				if !tt.manual {
					assert.False(t, got.needsInput)
					mu.Lock()
					defer mu.Unlock()
					assert.Equal(t, "s1", seen[1].RequestState)
					assert.Contains(t, seen[1].InputResponses, "q")
					return
				}
				assert.True(t, got.needsInput)
				assert.Equal(t, "s1", got.state)
				assert.Len(t, got.requests, len(tt.requests))
				if q, ok := tt.requests["q"]; ok {
					require.Contains(t, got.requests, "q")
					assert.Equal(t, q.Method, got.requests["q"].Method)
					require.NotNil(t, got.requests["q"].Elicitation)
					assert.Equal(t, q.Elicitation.Message, got.requests["q"].Elicitation.Message)
				}

				if tt.retry {
					responses := mcp.InputResponses{"q": mcp.NewElicitationInputResponse(mcp.ElicitationResult{
						ElicitationResponse: mcp.ElicitationResponse{Action: mcp.ElicitationResponseActionAccept},
					})}
					got = m.call(c, mcp.MultiRoundTripParams{InputResponses: responses, RequestState: got.state})
					require.NoError(t, got.err)
					assert.False(t, got.needsInput)
				}

				mu.Lock()
				defer mu.Unlock()
				if tt.retry {
					require.Len(t, seen, 2)
					assert.Equal(t, "s1", seen[1].RequestState)
					assert.Contains(t, seen[1].InputResponses, "q")
				}
			})
		}
	}
}
