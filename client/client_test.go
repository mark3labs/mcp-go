package client

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestStateRestoration(t *testing.T) {
	mcpServer := server.NewMCPServer(
		"test-server",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// First client lifecycle
	client1, err := NewInProcessClient(mcpServer)
	if err != nil {
		t.Fatalf("Failed to create first client: %v", err)
	}

	ctx := context.Background()

	if err := client1.Start(ctx); err != nil {
		t.Fatalf("Failed to start first client: %v", err)
	}

	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "test-client",
		Version: "1.0.0",
	}

	if _, err := client1.Initialize(ctx, initRequest); err != nil {
		t.Fatalf("Failed to initialize first client: %v", err)
	}

	if err := client1.Ping(ctx); err != nil {
		t.Fatalf("Ping on first client failed: %v", err)
	}

	exportedState := client1.ExportState()

	if err := client1.Close(); err != nil {
		t.Fatalf("Failed to close first client: %v", err)
	}

	// Second client lifecycle (state restoration)
	client2, err := NewInProcessClient(mcpServer)
	if err != nil {
		t.Fatalf("Failed to create second client: %v", err)
	}
	defer client2.Close()

	if err := client2.Start(ctx); err != nil {
		t.Fatalf("Failed to start second client: %v", err)
	}

	if err := client2.ImportState(ctx, exportedState); err != nil {
		t.Fatalf("Failed to import state into second client: %v", err)
	}

	if err := client2.Ping(ctx); err != nil {
		t.Fatalf("Ping on restored client failed: %v", err)
	}

	restoredState := client2.ExportState()

	if !restoredState.Initialized {
		t.Errorf("Expected restored client to be initialized")
	}

	if restoredState.RequestID <= exportedState.RequestID {
		t.Errorf("Expected request ID to increase after restoration; before=%d, after=%d", exportedState.RequestID, restoredState.RequestID)
	}
}
