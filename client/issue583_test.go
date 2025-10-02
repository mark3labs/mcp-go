package client

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// TestDirectTransportCreation tests the bug reported in issue #583
// where using transport.NewStdioWithOptions directly followed by client.Start()
// would fail with "stdio client not started" error
func TestDirectTransportCreation(t *testing.T) {
	tempFile, err := os.CreateTemp("", "mockstdio_server")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tempFile.Close()
	mockServerPath := tempFile.Name()

	if runtime.GOOS == "windows" {
		os.Remove(mockServerPath)
		mockServerPath += ".exe"
	}

	if compileErr := compileTestServer(mockServerPath); compileErr != nil {
		t.Fatalf("Failed to compile mock server: %v", compileErr)
	}
	defer os.Remove(mockServerPath)

	// This is the pattern from issue #583 that was broken
	tport := transport.NewStdioWithOptions(mockServerPath, nil, nil)
	cli := NewClient(tport)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// This should work now (was failing before fix)
	if err := cli.Start(ctx); err != nil {
		t.Fatalf("client.Start() failed: %v", err)
	}
	defer cli.Close()

	// Verify Initialize works (was failing with "stdio client not started")
	initCtx, initCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer initCancel()

	request := mcp.InitializeRequest{}
	request.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	request.Params.ClientInfo = mcp.Implementation{
		Name:    "test-client",
		Version: "1.0.0",
	}

	result, err := cli.Initialize(initCtx, request)
	if err != nil {
		t.Fatalf("Initialize failed: %v (this was the bug in issue #583)", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}
}

// TestNewStdioMCPClientWithOptions tests that the old pattern still works
func TestNewStdioMCPClientWithOptions(t *testing.T) {
	tempFile, err := os.CreateTemp("", "mockstdio_server")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tempFile.Close()
	mockServerPath := tempFile.Name()

	if runtime.GOOS == "windows" {
		os.Remove(mockServerPath)
		mockServerPath += ".exe"
	}

	if compileErr := compileTestServer(mockServerPath); compileErr != nil {
		t.Fatalf("Failed to compile mock server: %v", compileErr)
	}
	defer os.Remove(mockServerPath)

	// This pattern was already working
	cli, err := NewStdioMCPClientWithOptions(mockServerPath, nil, nil)
	if err != nil {
		t.Fatalf("NewStdioMCPClientWithOptions failed: %v", err)
	}
	defer cli.Close()

	// Calling Start() again should be idempotent (no error)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := cli.Start(ctx); err != nil {
		t.Fatalf("client.Start() should be idempotent, got error: %v", err)
	}

	// Verify Initialize works
	initCtx, initCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer initCancel()

	request := mcp.InitializeRequest{}
	request.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	request.Params.ClientInfo = mcp.Implementation{
		Name:    "test-client",
		Version: "1.0.0",
	}

	result, err := cli.Initialize(initCtx, request)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}
}

// TestMultipleClientStartCalls tests that calling client.Start() multiple times is safe
func TestMultipleClientStartCalls(t *testing.T) {
	tempFile, err := os.CreateTemp("", "mockstdio_server")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tempFile.Close()
	mockServerPath := tempFile.Name()

	if runtime.GOOS == "windows" {
		os.Remove(mockServerPath)
		mockServerPath += ".exe"
	}

	if compileErr := compileTestServer(mockServerPath); compileErr != nil {
		t.Fatalf("Failed to compile mock server: %v", compileErr)
	}
	defer os.Remove(mockServerPath)

	tport := transport.NewStdioWithOptions(mockServerPath, nil, nil)
	cli := NewClient(tport)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First Start()
	if err := cli.Start(ctx); err != nil {
		t.Fatalf("First client.Start() failed: %v", err)
	}
	defer cli.Close()

	// Second Start() - should be idempotent
	if err := cli.Start(ctx); err != nil {
		t.Errorf("Second client.Start() should be idempotent, got error: %v", err)
	}

	// Third Start() - should still be idempotent
	if err := cli.Start(ctx); err != nil {
		t.Errorf("Third client.Start() should be idempotent, got error: %v", err)
	}

	// Verify client still works
	initCtx, initCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer initCancel()

	request := mcp.InitializeRequest{}
	request.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	request.Params.ClientInfo = mcp.Implementation{
		Name:    "test-client",
		Version: "1.0.0",
	}

	result, err := cli.Initialize(initCtx, request)
	if err != nil {
		t.Fatalf("Initialize failed after multiple Start() calls: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}
}
