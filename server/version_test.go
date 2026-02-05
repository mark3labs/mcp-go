package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestMCPServer_VersionNegotiation_Explicit(t *testing.T) {
	tests := []struct {
		name            string
		clientVersion   string
		expectedVersion string
	}{
		{
			name:            "Client sends 2025-11-25 (Latest)",
			clientVersion:   "2025-11-25",
			expectedVersion: "2025-11-25",
		},
		{
			name:            "Client sends 2025-06-18 (Previous)",
			clientVersion:   "2025-06-18",
			expectedVersion: "2025-06-18",
		},
		{
			name:            "Client sends 2025-03-26 (Older)",
			clientVersion:   "2025-03-26",
			expectedVersion: "2025-03-26",
		},
		{
			name:            "Client sends 2024-11-05 (Oldest supported)",
			clientVersion:   "2024-11-05",
			expectedVersion: "2024-11-05",
		},
		{
			name:            "Client sends unknown version",
			clientVersion:   "2099-01-01",
			expectedVersion: "2025-11-25", // Should fallback to the latest
		},
		{
			name:            "Client sends empty version",
			clientVersion:   "",
			expectedVersion: "2025-03-26", // Default behavior for backward compatibility
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewMCPServer("test-server", "1.0.0")

			params := struct {
				ProtocolVersion string                 `json:"protocolVersion"`
				ClientInfo      mcp.Implementation     `json:"clientInfo"`
				Capabilities    mcp.ClientCapabilities `json:"capabilities"`
			}{
				ProtocolVersion: tt.clientVersion,
				ClientInfo: mcp.Implementation{
					Name:    "test-client",
					Version: "1.0.0",
				},
			}

			initReq := mcp.JSONRPCRequest{
				JSONRPC: mcp.JSONRPC_VERSION,
				ID:      mcp.NewRequestId(int64(1)),
				Request: mcp.Request{
					Method: "initialize",
				},
				Params: params,
			}

			messageBytes, err := json.Marshal(initReq)
			assert.NoError(t, err)

			response := server.HandleMessage(context.Background(), messageBytes)
			assert.NotNil(t, response)

			resp, ok := response.(mcp.JSONRPCResponse)
			assert.True(t, ok)

			initResult, ok := resp.Result.(mcp.InitializeResult)
			assert.True(t, ok)

			assert.Equal(t, tt.expectedVersion, initResult.ProtocolVersion)
		})
	}
}
