package server

import (
	"context"
	"fmt"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// confirmThenGreet is a tool that needs a name from the user before it can
// answer. It is written once, in the multi round-trip style, and works against
// clients of either protocol era.
func confirmThenGreet(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if answer := ElicitationResponse(request.Params.InputResponses, "who"); answer != nil {
		if answer.Action != mcp.ElicitationResponseActionAccept {
			return mcp.NewToolResultText("nobody to greet"), nil
		}
		content, _ := answer.Content.(map[string]any)
		name, _ := content["name"].(string)
		return mcp.NewToolResultText("hello " + name), nil
	}

	return NewInputRequestBuilder("step=1").
		Elicit("who", mcp.ElicitationParams{
			Mode:    mcp.ElicitationModeForm,
			Message: "What is your name?",
			RequestedSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
			},
		}).
		ToolResult(), nil
}

func newMRTRServer(t *testing.T) *MCPServer {
	t.Helper()
	srv := NewMCPServer("mrtr-test", "1.0.0", WithToolCapabilities(true), WithElicitation())
	srv.AddTool(mcp.NewTool("confirmThenGreet"), confirmThenGreet)
	return srv
}

// callToolInEra invokes a tool through the full dispatch path, in the protocol
// era selected by modern.
func callToolInEra(
	t *testing.T,
	srv *MCPServer,
	session ClientSession,
	params mcp.CallToolParams,
	modern bool,
) *mcp.CallToolResult {
	t.Helper()

	ctx := srv.WithContext(t.Context(), session)
	info := &RequestProtocolInfo{}
	if modern {
		info = &RequestProtocolInfo{
			Modern:             true,
			ProtocolVersion:    mcp.ProtocolVersion20260728,
			ClientCapabilities: &mcp.ClientCapabilities{Elicitation: &mcp.ElicitationCapability{}},
		}
	}
	ctx = WithRequestProtocolInfo(ctx, info)

	request := mcp.CallToolRequest{Params: params}
	result, reqErr := srv.handleToolCall(ctx, 1, request)
	require.Nil(t, reqErr)

	toolResult, ok := result.(*mcp.CallToolResult)
	require.True(t, ok, "expected a CallToolResult, got %T", result)
	return toolResult
}

func TestMultiRoundTrip_ModernClientReceivesInputRequest(t *testing.T) {
	srv := newMRTRServer(t)
	session := newMRTRSession("modern")

	// First call: the handler needs a name, so the client is asked for one.
	result := callToolInEra(t, srv, session, mcp.CallToolParams{Name: "confirmThenGreet"}, true)

	require.True(t, result.NeedsInput())
	assert.Equal(t, mcp.ResultTypeInputRequired, result.ResultType)
	assert.Equal(t, "step=1", result.RequestState)

	request, ok := result.InputRequests["who"]
	require.True(t, ok, "expected an input request keyed 'who'")
	assert.Equal(t, mcp.MethodElicitationCreate, request.Method)
	require.NotNil(t, request.Elicitation)
	assert.Equal(t, "What is your name?", request.Elicitation.Message)
}

func TestMultiRoundTrip_ModernClientRetriesWithAnswer(t *testing.T) {
	srv := newMRTRServer(t)
	session := newMRTRSession("modern")

	// Second call: the client has collected the name and retries, echoing the
	// opaque request state back.
	result := callToolInEra(t, srv, session, mcp.CallToolParams{
		Name: "confirmThenGreet",
		MultiRoundTripParams: mcp.MultiRoundTripParams{
			RequestState: "step=1",
			InputResponses: mcp.InputResponses{
				"who": mcp.NewElicitationInputResponse(mcp.ElicitationResult{
					ElicitationResponse: mcp.ElicitationResponse{
						Action:  mcp.ElicitationResponseActionAccept,
						Content: map[string]any{"name": "MCP Go"},
					},
				}),
			},
		},
	}, true)

	assert.False(t, result.NeedsInput())
	require.Len(t, result.Content, 1)
	assert.Equal(t, "hello MCP Go", result.Content[0].(mcp.TextContent).Text)
}

func TestMultiRoundTrip_LegacyClientIsBridged(t *testing.T) {
	srv := newMRTRServer(t)

	// A client predating the multi round-trip pattern never sees the input
	// request: the server issues the elicitation/create it understands and
	// re-invokes the handler with the answer.
	session := newMRTRSession("legacy")
	session.response = &mcp.ElicitationResult{
		ElicitationResponse: mcp.ElicitationResponse{
			Action:  mcp.ElicitationResponseActionAccept,
			Content: map[string]any{"name": "Legacy"},
		},
	}

	result := callToolInEra(t, srv, session, mcp.CallToolParams{Name: "confirmThenGreet"}, false)

	assert.False(t, result.NeedsInput(), "a legacy client must receive a final result")
	assert.Equal(t, 1, session.calls, "the server should have elicited on the handler's behalf")
	require.Len(t, result.Content, 1)
	assert.Equal(t, "hello Legacy", result.Content[0].(mcp.TextContent).Text)
}

func TestMultiRoundTrip_ServerInitiatedRequestsRejectedOnModern(t *testing.T) {
	srv := NewMCPServer("mrtr-test", "1.0.0", WithElicitation())
	session := newMRTRSession("modern")

	ctx := srv.WithContext(t.Context(), session)
	ctx = WithRequestProtocolInfo(ctx, &RequestProtocolInfo{
		Modern:          true,
		ProtocolVersion: mcp.ProtocolVersion20260728,
	})

	_, err := srv.RequestElicitation(ctx, mcp.ElicitationRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrServerInitiatedRequestUnsupported)
	assert.Equal(t, 0, session.calls, "nothing should have been sent to the client")
}

func TestMultiRoundTrip_LoadSheddingIsRejectedForLegacyClients(t *testing.T) {
	srv := NewMCPServer("mrtr-test", "1.0.0", WithToolCapabilities(true))
	// A handler that sheds load by asking the client to retry with no input.
	srv.AddTool(mcp.NewTool("busy"), func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return NewInputRequestBuilder("retry-later").ToolResult(), nil
	})

	session := newMRTRSession("legacy")
	ctx := srv.WithContext(t.Context(), session)
	ctx = WithRequestProtocolInfo(ctx, &RequestProtocolInfo{})

	// A legacy client has no way to act on a load-shedding signal, so it is
	// surfaced as an error rather than an empty result. The sentinel lets a
	// caller map it onto transport-level backpressure.
	_, reqErr := srv.handleToolCall(ctx, 1, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "busy"},
	})
	require.NotNil(t, reqErr)
	assert.ErrorIs(t, reqErr, ErrLoadShedding)
	assert.Contains(t, reqErr.Error(), "multi round-trip")
}

func TestMultiRoundTrip_InputResponseAccessors(t *testing.T) {
	responses := mcp.InputResponses{
		"elicit": mcp.NewElicitationInputResponse(mcp.ElicitationResult{
			ElicitationResponse: mcp.ElicitationResponse{Action: mcp.ElicitationResponseActionDecline},
		}),
		"sample": mcp.NewSamplingInputResponse(mcp.CreateMessageResult{Model: "test-model"}),
		"roots": mcp.NewRootsInputResponse(mcp.ListRootsResult{
			Roots: []mcp.Root{{URI: "file:///tmp", Name: "tmp"}},
		}),
	}

	elicitation := ElicitationResponse(responses, "elicit")
	require.NotNil(t, elicitation)
	assert.Equal(t, mcp.ElicitationResponseActionDecline, elicitation.Action)

	sampling := SamplingResponse(responses, "sample")
	require.NotNil(t, sampling)
	assert.Equal(t, "test-model", sampling.Model)

	roots := RootsResponse(responses, "roots")
	require.NotNil(t, roots)
	require.Len(t, roots.Roots, 1)
	assert.Equal(t, "file:///tmp", roots.Roots[0].URI)

	assert.Nil(t, ElicitationResponse(responses, "absent"))
}

// mrtrSession answers server-initiated elicitation requests, the way a client
// using a protocol version before 2026-07-28 would.
type mrtrSession struct {
	clientInfoStore
	sessionID string
	notify    chan mcp.JSONRPCNotification
	response  *mcp.ElicitationResult
	calls     int
}

func newMRTRSession(id string) *mrtrSession {
	return &mrtrSession{
		sessionID: id,
		notify:    make(chan mcp.JSONRPCNotification, 16),
	}
}

func (s *mrtrSession) SessionID() string { return s.sessionID }
func (s *mrtrSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return s.notify
}
func (s *mrtrSession) Initialize()       {}
func (s *mrtrSession) Initialized() bool { return true }

func (s *mrtrSession) RequestElicitation(
	_ context.Context,
	_ mcp.ElicitationRequest,
) (*mcp.ElicitationResult, error) {
	s.calls++
	if s.response == nil {
		return nil, fmt.Errorf("no elicitation response configured")
	}
	return s.response, nil
}

var (
	_ ClientSession          = (*mrtrSession)(nil)
	_ SessionWithElicitation = (*mrtrSession)(nil)
	_ SessionWithClientInfo  = (*mrtrSession)(nil)
)

// nilResultSession answers every server-initiated request with neither a
// result nor an error.
type nilResultSession struct {
	mrtrSession
}

func (s *nilResultSession) RequestElicitation(
	_ context.Context,
	_ mcp.ElicitationRequest,
) (*mcp.ElicitationResult, error) {
	s.calls++
	return nil, nil
}

func (s *nilResultSession) RequestSampling(
	_ context.Context,
	_ mcp.CreateMessageRequest,
) (*mcp.CreateMessageResult, error) {
	s.calls++
	return nil, nil
}

func (s *nilResultSession) ListRoots(
	_ context.Context,
	_ mcp.ListRootsRequest,
) (*mcp.ListRootsResult, error) {
	s.calls++
	return nil, nil
}

var (
	_ SessionWithSampling = (*nilResultSession)(nil)
	_ SessionWithRoots    = (*nilResultSession)(nil)
)

func TestMultiRoundTrip_NilSessionResultIsReportedNotPanicked(t *testing.T) {
	// A session that answers with neither a result nor an error must produce
	// an error. These run on their own goroutines, so a panic would take the
	// process down rather than failing the request.
	tests := []struct {
		name    string
		request mcp.InputRequest
		wantErr string
	}{
		{
			name: "elicitation",
			request: mcp.NewElicitationInputRequest(mcp.ElicitationParams{
				Mode:    mcp.ElicitationModeForm,
				Message: "name?",
				RequestedSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{"name": map[string]any{"type": "string"}},
				},
			}),
			wantErr: "session returned no elicitation result",
		},
		{
			name: "sampling",
			request: mcp.NewSamplingInputRequest(mcp.CreateMessageParams{
				Messages:  []mcp.SamplingMessage{{Role: mcp.RoleUser, Content: mcp.NewTextContent("hi")}},
				MaxTokens: 16,
			}),
			wantErr: "session returned no sampling result",
		},
		{
			name:    "roots",
			request: mcp.NewRootsInputRequest(),
			wantErr: "session returned no roots result",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewMCPServer("mrtr-test", "1.0.0", WithElicitation())
			session := &nilResultSession{mrtrSession: *newMRTRSession("legacy")}

			ctx := srv.WithContext(t.Context(), session)
			ctx = WithRequestProtocolInfo(ctx, &RequestProtocolInfo{})

			require.NotPanics(t, func() {
				_, err := srv.fulfillInputRequests(ctx, mcp.InputRequests{"who": tt.request})
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			})
		})
	}
}

func TestMultiRoundTrip_URLElicitationIsGatedOnModernClients(t *testing.T) {
	// RequestURLElicitation sends the same elicitation/create request, so it
	// must not be a way around the SEP-2322 restriction.
	srv := NewMCPServer("mrtr-test", "1.0.0", WithElicitation())
	session := newMRTRSession("modern")

	ctx := srv.WithContext(t.Context(), session)
	ctx = WithRequestProtocolInfo(ctx, &RequestProtocolInfo{
		Modern:          true,
		ProtocolVersion: mcp.ProtocolVersion20260728,
	})

	_, err := srv.RequestURLElicitation(ctx, session, "id-1", "https://example.com/auth", "authorize")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrServerInitiatedRequestUnsupported)
	assert.Equal(t, 0, session.calls, "nothing should have been sent to the client")
}
