package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestResponsePreservesIntegerRequestID(t *testing.T) {
	s := NewMCPServer("probe", "1.0")
	for _, raw := range []string{"1", "9007199254740993", "-9007199254740993", "9223372036854775807", "-9223372036854775808", `"9007199254740993"`, "18446744073709551615", "1e1000000000000000000"} {
		t.Run(raw, func(t *testing.T) {
			for _, method := range []string{"ping", "unknown"} {
				response := s.HandleMessage(t.Context(), json.RawMessage(`{"jsonrpc":"2.0","id":`+raw+`,"method":"`+method+`"}`))
				wire, err := json.Marshal(response)
				require.NoError(t, err)
				var fields map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(wire, &fields))
				require.Equal(t, raw, string(fields["id"]), method)
			}
		})
	}
}

func TestCancellationPreservesIntegerRequestID(t *testing.T) {
	tests := []struct {
		name string
		ids  [2]string
	}{
		{"ordinary", [2]string{"1", "2"}},
		{"adjacent large", [2]string{"9007199254740992", "9007199254740993"}},
		{"string", [2]string{`"9007199254740992"`, `"9007199254740993"`}},
		{"int64 exponent", [2]string{"9007199254740993e0", "9007199254740994"}},
		{"uint64", [2]string{"18446744073709551615", "18446744073709551616"}},
		{"number and string", [2]string{"1", `"1"`}},
		{"huge exponent", [2]string{"1e1000000000000000000", "2e1000000000000000000"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewMCPServer("probe", "1.0")
			started := make(chan string, 2)
			cancelled := make(chan string, 2)
			s.AddTool(mcp.NewTool("block", mcp.WithString("label")), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				label := req.GetString("label", "")
				started <- label
				<-ctx.Done()
				cancelled <- label
				return mcp.NewToolResultText("cancelled"), nil
			})
			ctx, cleanup := context.WithCancel(t.Context())
			var workers sync.WaitGroup
			defer func() { cleanup(); workers.Wait() }()
			for i, id := range tt.ids {
				label := fmt.Sprintf("request-%d", i)
				workers.Add(1)
				go func(id, label string) {
					defer workers.Done()
					s.HandleMessage(ctx, json.RawMessage(`{"jsonrpc":"2.0","id":`+id+`,"method":"tools/call","params":{"name":"block","arguments":{"label":"`+label+`"}}}`))
				}(id, label)
				select {
				case actual := <-started:
					require.Equal(t, label, actual)
				case <-time.After(2 * time.Second):
					t.Fatal("handler did not start")
				}
			}
			target := tt.ids[0]
			if tt.name == "int64 exponent" {
				target = "9007199254740993"
			}
			if tt.name == "uint64" {
				target = "184467440737095516150e-1"
			}
			if tt.name == "huge exponent" {
				target = "10e999999999999999999"
			}
			s.HandleMessage(ctx, json.RawMessage(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":`+target+`}}`))
			select {
			case actual := <-cancelled:
				require.Equal(t, "request-0", actual)
			case <-time.After(2 * time.Second):
				t.Fatal("cancellation not delivered")
			}
		})
	}
}
