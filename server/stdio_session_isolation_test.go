package server

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

type isolatedStdioOutput struct {
	messages chan json.RawMessage
}

func (w *isolatedStdioOutput) Write(p []byte) (int, error) {
	w.messages <- append(json.RawMessage(nil), p...)
	return len(p), nil
}

type isolatedStdioPeer struct {
	server *MCPServer
	ctx    context.Context
	input  *io.PipeWriter
	output *isolatedStdioOutput
}

func startIsolatedStdioPeer(t *testing.T, contextFuncs ...func(context.Context, *MCPServer) context.Context) *isolatedStdioPeer {
	t.Helper()
	peer := &isolatedStdioPeer{
		server: NewMCPServer("test", "1.0.0"),
		output: &isolatedStdioOutput{messages: make(chan json.RawMessage, 16)},
	}
	stdin, input := io.Pipe()
	peer.input = input
	ctx, cancel := context.WithCancel(t.Context())
	ready := make(chan context.Context, 1)
	done := make(chan error, 1)
	stdio := NewStdioServer(peer.server)
	stdio.SetErrorLogger(log.New(io.Discard, "", 0))
	stdio.SetContextFunc(func(ctx context.Context) context.Context {
		for _, contextFunc := range contextFuncs {
			ctx = contextFunc(ctx, peer.server)
		}
		ready <- ctx
		return ctx
	})
	go func() { done <- stdio.Listen(ctx, stdin, peer.output) }()
	t.Cleanup(func() {
		cancel()
		_ = input.Close()
		defer stdin.Close()
		select {
		case err := <-done:
			if err != nil {
				assert.ErrorIs(t, err, context.Canceled)
			}
		case <-time.After(5 * time.Second):
			t.Error("stdio listener did not stop")
		}
	})
	select {
	case peer.ctx = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("stdio listener did not start")
	}
	return peer
}

type isolatedStdioSessionInterfaces interface {
	SessionWithClientInfo
	SessionWithSampling
	SessionWithElicitation
	SessionWithRoots
	SessionWithProtocolVersion
	SessionWithLogging
}

type isolatedStdioSessionDecorator struct{ isolatedStdioSessionInterfaces }

func TestStdioSessionDecoratorPreservesResponseRouting(t *testing.T) {
	for _, tt := range isolatedStdioRequestCases() {
		t.Run(tt.name, func(t *testing.T) {
			peer := startIsolatedStdioPeer(t, func(ctx context.Context, s *MCPServer) context.Context {
				session := ClientSessionFromContext(ctx).(isolatedStdioSessionInterfaces)
				return s.WithContext(ctx, &isolatedStdioSessionDecorator{session})
			})
			peer.initialize(t, "decorated-client", mcp.ProtocolVersion20251125, mcp.ClientCapabilities{})
			ctx, cancel := context.WithTimeout(peer.ctx, 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- tt.request(ctx, peer.server) }()
			var request struct {
				ID     mcp.RequestId `json:"id"`
				Method mcp.MCPMethod `json:"method"`
			}
			require.NoError(t, json.Unmarshal(peer.receive(t), &request))
			require.Equal(t, tt.method, request.Method)
			peer.send(t, map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": tt.result})
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-ctx.Done():
				t.Fatal("decorated session did not receive its client's response")
			}
		})
	}
}

func (p *isolatedStdioPeer) send(t *testing.T, message any) {
	t.Helper()
	data, err := json.Marshal(message)
	require.NoError(t, err)
	_, err = p.input.Write(append(data, '\n'))
	require.NoError(t, err)
}

func (p *isolatedStdioPeer) receive(t *testing.T) json.RawMessage {
	t.Helper()
	select {
	case message := <-p.output.messages:
		return message
	case <-time.After(5 * time.Second):
		t.Fatal("stdio listener did not write a message")
		return nil
	}
}

func (p *isolatedStdioPeer) initialize(t *testing.T, name, version string, capabilities mcp.ClientCapabilities) {
	t.Helper()
	p.send(t, map[string]any{
		"jsonrpc": "2.0", "id": 100, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": version,
			"clientInfo":      mcp.Implementation{Name: name, Version: "1.0.0"},
			"capabilities":    capabilities,
		},
	})
	var response map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(p.receive(t), &response))
	require.NotContains(t, response, "error")
	require.JSONEq(t, "100", string(response["id"]))
	p.send(t, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	p.send(t, map[string]any{"jsonrpc": "2.0", "id": 101, "method": "ping"})
	require.NoError(t, json.Unmarshal(p.receive(t), &response))
	require.NotContains(t, response, "error")
	require.JSONEq(t, "101", string(response["id"]))
}

func TestStdioServersIsolateSessionState(t *testing.T) {
	first := startIsolatedStdioPeer(t)
	first.initialize(t, "first-client", mcp.ProtocolVersion20241105, mcp.ClientCapabilities{Sampling: &mcp.SamplingCapability{}})
	firstSession := ClientSessionFromContext(first.ctx)
	require.True(t, firstSession.Initialized())
	firstSession.(SessionWithLogging).SetLogLevel(mcp.LoggingLevelDebug)

	second := startIsolatedStdioPeer(t)
	secondSession := ClientSessionFromContext(second.ctx)
	assert.False(t, secondSession.Initialized(), "a new listener must not inherit an earlier client's handshake")
	assert.NotSame(t, firstSession, secondSession)
	assert.NotEqual(t, firstSession.NotificationChannel(), secondSession.NotificationChannel())
	second.initialize(t, "second-client", mcp.ProtocolVersion20251125, mcp.ClientCapabilities{})

	firstInfo := firstSession.(SessionWithClientInfo)
	assert.Equal(t, "first-client", firstInfo.GetClientInfo().Name)
	assert.NotNil(t, firstInfo.GetClientCapabilities().Sampling)
	assert.Equal(t, mcp.ProtocolVersion20241105, firstSession.(SessionWithProtocolVersion).ProtocolVersion())
	assert.Equal(t, mcp.LoggingLevelDebug, firstSession.(SessionWithLogging).GetLogLevel())
}

func TestStdioListenStartsWithFreshSession(t *testing.T) {
	stdio := NewStdioServer(NewMCPServer("test", "1.0.0"))
	var sessions []ClientSession
	stdio.SetContextFunc(func(ctx context.Context) context.Context {
		sessions = append(sessions, ClientSessionFromContext(ctx))
		return ctx
	})
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","clientInfo":{"name":"first-client","version":"1.0.0"},"capabilities":{}}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"
	require.NoError(t, stdio.Listen(t.Context(), strings.NewReader(input), io.Discard))
	require.True(t, sessions[0].Initialized())
	require.NoError(t, stdio.Listen(t.Context(), strings.NewReader(""), io.Discard))
	require.Len(t, sessions, 2)
	assert.False(t, sessions[1].Initialized())
	assert.Empty(t, sessions[1].(SessionWithClientInfo).GetClientInfo().Name)
	assert.Empty(t, sessions[1].(SessionWithProtocolVersion).ProtocolVersion())
}

type isolatedStdioRequestCase struct {
	name    string
	method  mcp.MCPMethod
	request func(context.Context, *MCPServer) error
	result  any
}

func isolatedStdioRequestCases() []isolatedStdioRequestCase {
	return []isolatedStdioRequestCase{
		{
			name: "roots", method: mcp.MethodListRoots,
			request: func(ctx context.Context, s *MCPServer) error {
				_, err := s.RequestRoots(ctx, mcp.ListRootsRequest{})
				return err
			},
			result: mcp.ListRootsResult{Roots: []mcp.Root{{URI: "file:///first"}}},
		},
		{
			name: "sampling", method: mcp.MethodSamplingCreateMessage,
			request: func(ctx context.Context, s *MCPServer) error {
				_, err := s.RequestSampling(ctx, mcp.CreateMessageRequest{CreateMessageParams: mcp.CreateMessageParams{
					Messages:  []mcp.SamplingMessage{{Role: mcp.RoleUser, Content: mcp.NewTextContent("test")}},
					MaxTokens: 1,
				}})
				return err
			},
			result: mcp.CreateMessageResult{SamplingMessage: mcp.SamplingMessage{Role: mcp.RoleAssistant, Content: mcp.NewTextContent("first")}, Model: "test-model"},
		},
		{
			name: "elicitation", method: mcp.MethodElicitationCreate,
			request: func(ctx context.Context, s *MCPServer) error {
				_, err := s.RequestElicitation(ctx, mcp.ElicitationRequest{Params: mcp.ElicitationParams{
					Mode: mcp.ElicitationModeURL, Message: "test", ElicitationID: "test", URL: "https://example.com/",
				}})
				return err
			},
			result: mcp.ElicitationResult{ElicitationResponse: mcp.ElicitationResponse{Action: mcp.ElicitationResponseActionCancel}},
		},
	}
}

func TestStdioServersRouteRequestsToOwnClient(t *testing.T) {
	for _, tt := range isolatedStdioRequestCases() {
		t.Run(tt.name, func(t *testing.T) {
			first := startIsolatedStdioPeer(t)
			first.initialize(t, "first-client", mcp.ProtocolVersion20251125, mcp.ClientCapabilities{})
			second := startIsolatedStdioPeer(t)
			second.initialize(t, "second-client", mcp.ProtocolVersion20251125, mcp.ClientCapabilities{})
			ctx, cancel := context.WithTimeout(first.ctx, 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- tt.request(ctx, first.server) }()
			var message json.RawMessage
			select {
			case message = <-first.output.messages:
			case message = <-second.output.messages:
				t.Fatalf("first server sent %s to the second client: %s", tt.method, message)
			case err := <-done:
				t.Fatalf("request ended before reaching its client: %v", err)
			case <-ctx.Done():
				t.Fatal("request did not reach either client")
			}
			var request struct {
				ID     mcp.RequestId `json:"id"`
				Method mcp.MCPMethod `json:"method"`
			}
			require.NoError(t, json.Unmarshal(message, &request))
			require.Equal(t, tt.method, request.Method)
			first.send(t, map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": tt.result})
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-ctx.Done():
				t.Fatal("request did not receive its client's response")
			}
		})
	}
}
