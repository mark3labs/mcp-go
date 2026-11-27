package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestSubscriptionsListen_TaskIDsRequireExtension verifies capability gating
// through JSON decoding and omits unsupported task subscriptions from the ack.
func TestSubscriptionsListen_TaskIDsRequireExtension(t *testing.T) {
	tests := []struct {
		name          string
		notifications map[string]any
		capabilities  *mcp.ClientCapabilities
		wantError     bool
	}{
		{
			name:          "empty capabilities",
			notifications: map[string]any{"taskIds": []string{"some-task"}},
			capabilities:  &mcp.ClientCapabilities{},
			wantError:     true,
		},
		{
			name:          "unrelated extension",
			notifications: map[string]any{"taskIds": []string{"some-task"}},
			capabilities:  &mcp.ClientCapabilities{Extensions: map[string]any{"example/other": map[string]any{}}},
			wantError:     true,
		},
		{
			name:          "declared Tasks extension",
			notifications: map[string]any{"taskIds": []string{"some-task"}},
			capabilities:  &mcp.ClientCapabilities{Extensions: map[string]any{mcp.ExtensionTasks: map[string]any{}}},
		},
		{
			name:          "other subscriptions need no Tasks extension",
			notifications: map[string]any{"promptsListChanged": true},
			capabilities:  &mcp.ClientCapabilities{},
		},
		{
			name:          "empty task IDs need no Tasks extension",
			notifications: map[string]any{"taskIds": []string{}},
			capabilities:  &mcp.ClientCapabilities{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewMCPServer("gate-test", "1.0.0", WithTasksExtension())
			session := newListenSession()
			ctx := srv.WithContext(t.Context(), session)
			meta := map[string]any{
				mcp.MetaKeyProtocolVersion:    mcp.ProtocolVersion20260728,
				mcp.MetaKeyClientCapabilities: tt.capabilities,
			}
			request, err := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": 12, "method": mcp.MethodSubscriptionsListen,
				"params": map[string]any{"_meta": meta, "notifications": tt.notifications},
			})
			require.NoError(t, err)
			response := srv.HandleMessage(ctx, request)
			if tt.wantError {
				errorResponse, ok := response.(mcp.JSONRPCError)
				require.True(t, ok, "expected capability error, got %T", response)
				assert.Equal(t, mcp.MISSING_REQUIRED_CLIENT_CAPABILITY, errorResponse.Error.Code)
				assert.Equal(t, missingExtensionData(mcp.ExtensionTasks), errorResponse.Error.Data)
				assert.Empty(t, session.notify, "rejected requests must not acknowledge subscriptions")
				return
			}
			_, ok := response.(mcp.JSONRPCResponse)
			require.True(t, ok, "expected success, got %T", response)
			select {
			case ack := <-session.notify:
				assert.Equal(t, mcp.MethodNotificationSubscriptionsAcknowledged, ack.Method)
				filter, ok := ack.Params.AdditionalFields["notifications"].(mcp.SubscriptionFilter)
				require.True(t, ok)
				assert.Empty(t, filter.TaskIDs, "unsupported task subscriptions must not be acknowledged")
				wire, err := json.Marshal(ack)
				require.NoError(t, err)
				assert.NotContains(t, string(wire), "taskIds")
			default:
				t.Fatal("missing subscription acknowledgement")
			}
		})
	}
}
