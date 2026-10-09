package client

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

type initialRoundTripTransport struct {
	params json.RawMessage
	result json.RawMessage
}

func (s *initialRoundTripTransport) Start(context.Context) error { return nil }
func (s *initialRoundTripTransport) SendNotification(context.Context, mcp.JSONRPCNotification) error {
	return nil
}
func (s *initialRoundTripTransport) SetNotificationHandler(func(mcp.JSONRPCNotification)) {}
func (s *initialRoundTripTransport) Close() error                                         { return nil }
func (s *initialRoundTripTransport) GetSessionId() string                                 { return "" }
func (s *initialRoundTripTransport) SendRequest(_ context.Context, r transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	var err error
	s.params, err = json.Marshal(r.Params)
	if err != nil {
		return nil, err
	}
	return &transport.JSONRPCResponse{JSONRPC: "2.0", ID: r.ID, Result: s.result}, nil
}

func TestClientPreservesInitialRoundTripResponses(t *testing.T) {
	tests := []struct {
		name, result string
		call         func(*Client, mcp.MultiRoundTripParams) error
	}{
		{"tool", `{"content":[]}`, func(c *Client, p mcp.MultiRoundTripParams) error {
			_, err := c.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "resume", MultiRoundTripParams: p}})
			return err
		}},
		{"prompt", `{"messages":[]}`, func(c *Client, p mcp.MultiRoundTripParams) error {
			_, err := c.GetPrompt(t.Context(), mcp.GetPromptRequest{Params: mcp.GetPromptParams{Name: "resume", MultiRoundTripParams: p}})
			return err
		}},
		{"resource", `{"contents":[]}`, func(c *Client, p mcp.MultiRoundTripParams) error {
			_, err := c.ReadResource(t.Context(), mcp.ReadResourceRequest{Params: mcp.ReadResourceParams{URI: "file:///resume", MultiRoundTripParams: p}})
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &initialRoundTripTransport{result: json.RawMessage(tt.result)}
			c := NewClient(tr)
			c.initialized.Store(true)
			p := mcp.MultiRoundTripParams{RequestState: "opaque-state", InputResponses: mcp.InputResponses{"roots": mcp.NewRootsInputResponse(mcp.ListRootsResult{Roots: []mcp.Root{}})}}
			require.NoError(t, tt.call(c, p))
			var got mcp.MultiRoundTripParams
			require.NoError(t, json.Unmarshal(tr.params, &got))
			require.Equal(t, p.RequestState, got.RequestState)
			require.Contains(t, got.InputResponses, "roots")
			expectedResponses, err := json.Marshal(p.InputResponses)
			require.NoError(t, err)
			actualResponses, err := json.Marshal(got.InputResponses)
			require.NoError(t, err)
			require.JSONEq(t, string(expectedResponses), string(actualResponses))
		})
	}
}
