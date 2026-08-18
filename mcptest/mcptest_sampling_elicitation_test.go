package mcptest_test

import (
	"context"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/mcptest"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServerWithSamplingHandler verifies that a tool which calls server.RequestSampling
// can be tested end-to-end using mcptest, without any external LLM.
func TestServerWithSamplingHandler(t *testing.T) {
	ctx := t.Context()

	const wantReply = "42"

	// A sampling handler that returns a fixed answer regardless of the question.
	samplingHandler := &fixedSamplingHandler{reply: wantReply}

	srv := mcptest.NewUnstartedServer(t)
	defer srv.Close()

	// A tool that delegates its response to the LLM via sampling.
	srv.AddTool(
		mcp.NewTool("ask_llm",
			mcp.WithDescription("Ask the LLM a question and return its response."),
			mcp.WithString("question",
				mcp.Required(),
				mcp.Description("The question to ask."),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			question, err := req.RequireString("question")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			samplingReq := mcp.CreateMessageRequest{
				CreateMessageParams: mcp.CreateMessageParams{
					Messages: []mcp.SamplingMessage{
						{
							Role:    mcp.RoleUser,
							Content: mcp.NewTextContent(question),
						},
					},
					MaxTokens: 64,
				},
			}

			mcpServer := server.ServerFromContext(ctx)
			result, err := mcpServer.RequestSampling(ctx, samplingReq)
			if err != nil {
				return mcp.NewToolResultError("sampling failed: " + err.Error()), nil
			}

			text, ok := result.Content.(mcp.TextContent)
			if !ok {
				return mcp.NewToolResultError("unexpected content type from sampling"), nil
			}
			return mcp.NewToolResultText(text.Text), nil
		},
	)

	srv.SetSamplingHandler(samplingHandler)

	if err := srv.Start(ctx); err != nil {
		t.Fatal("Start:", err)
	}

	var callReq mcp.CallToolRequest
	callReq.Params.Name = "ask_llm"
	callReq.Params.Arguments = map[string]any{"question": "What is 6*7?"}

	result, err := srv.Client().CallTool(ctx, callReq)
	if err != nil {
		t.Fatal("CallTool:", err)
	}

	got, err := resultToString(result)
	if err != nil {
		t.Fatal(err)
	}

	if got != wantReply {
		t.Errorf("got %q, want %q", got, wantReply)
	}
	if got := samplingHandler.calls(); got != 1 {
		t.Errorf("expected sampling handler called once, got %d", got)
	}
}

// TestServerWithElicitationHandler verifies that a tool which calls
// server.RequestElicitation can be tested end-to-end using mcptest.
func TestServerWithElicitationHandler(t *testing.T) {
	ctx := t.Context()

	// An elicitation handler that always accepts with a canned response.
	elicitationHandler := &fixedElicitationHandler{
		response: map[string]any{
			"confirmed": true,
		},
	}

	srv := mcptest.NewUnstartedServer(t)
	defer srv.Close()

	// The server must declare elicitation capability so the spec allows it to
	// issue elicitation/create requests.
	srv.AddServerOptions(server.WithElicitation())

	// A tool that asks the user to confirm an action before proceeding.
	srv.AddTool(
		mcp.NewTool("confirm_action",
			mcp.WithDescription("Ask the user to confirm before proceeding."),
			mcp.WithString("action",
				mcp.Required(),
				mcp.Description("Description of the action to confirm."),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			action, err := req.RequireString("action")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			elicitReq := mcp.ElicitationRequest{
				Params: mcp.ElicitationParams{
					Message: "Please confirm: " + action,
					RequestedSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"confirmed": map[string]any{
								"type": "boolean",
							},
						},
						"required": []string{"confirmed"},
					},
				},
			}

			mcpServer := server.ServerFromContext(ctx)
			elicitResult, err := mcpServer.RequestElicitation(ctx, elicitReq)
			if err != nil {
				return mcp.NewToolResultError("elicitation failed: " + err.Error()), nil
			}

			switch elicitResult.Action {
			case mcp.ElicitationResponseActionAccept:
				return mcp.NewToolResultText("confirmed"), nil
			case mcp.ElicitationResponseActionDecline:
				return mcp.NewToolResultText("declined"), nil
			default:
				return mcp.NewToolResultText("cancelled"), nil
			}
		},
	)

	srv.SetElicitationHandler(elicitationHandler)

	if err := srv.Start(ctx); err != nil {
		t.Fatal("Start:", err)
	}

	var callReq mcp.CallToolRequest
	callReq.Params.Name = "confirm_action"
	callReq.Params.Arguments = map[string]any{"action": "delete all records"}

	result, err := srv.Client().CallTool(ctx, callReq)
	if err != nil {
		t.Fatal("CallTool:", err)
	}

	got, err := resultToString(result)
	if err != nil {
		t.Fatal(err)
	}

	if got != "confirmed" {
		t.Errorf("got %q, want %q", got, "confirmed")
	}
	if got := elicitationHandler.calls(); got != 1 {
		t.Errorf("expected elicitation handler called once, got %d", got)
	}
}

// TestServerWithRootsHandler verifies that a tool which calls server.RequestRoots
// can be tested end-to-end using mcptest.
func TestServerWithRootsHandler(t *testing.T) {
	ctx := t.Context()
	rootsHandler := &fixedRootsHandler{
		roots: []mcp.Root{{URI: "file:///workspace", Name: "workspace"}},
	}

	srv := mcptest.NewUnstartedServer(t)
	defer srv.Close()
	srv.AddServerOptions(server.WithRoots())
	srv.AddTool(
		mcp.NewTool("list_workspace_roots"),
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			mcpServer := server.ServerFromContext(ctx)
			result, err := mcpServer.RequestRoots(ctx, mcp.ListRootsRequest{})
			if err != nil {
				return mcp.NewToolResultError("roots failed: " + err.Error()), nil
			}
			return mcp.NewToolResultText(result.Roots[0].URI), nil
		},
	)
	srv.SetRootsHandler(rootsHandler)

	require.NoError(t, srv.Start(ctx))
	result, err := srv.Client().CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "list_workspace_roots"},
	})
	require.NoError(t, err)

	got, err := resultToString(result)
	require.NoError(t, err)
	assert.Equal(t, "file:///workspace", got)
	assert.Equal(t, 1, rootsHandler.calls())
}

// fixedSamplingHandler is safe for concurrent use by the stdio worker pool.
type fixedSamplingHandler struct {
	callCounter
	reply string
}

func (h *fixedSamplingHandler) CreateMessage(_ context.Context, _ mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	h.recordCall()
	return &mcp.CreateMessageResult{
		SamplingMessage: mcp.SamplingMessage{
			Role:    mcp.RoleAssistant,
			Content: mcp.NewTextContent(h.reply),
		},
		Model:      "test-model",
		StopReason: "endTurn",
	}, nil
}

// fixedElicitationHandler is safe for concurrent use by the stdio worker pool.
type fixedElicitationHandler struct {
	callCounter
	response map[string]any
}

// fixedRootsHandler is safe for concurrent use by the stdio worker pool.
type fixedRootsHandler struct {
	callCounter
	roots []mcp.Root
}

func (h *fixedRootsHandler) ListRoots(_ context.Context, _ mcp.ListRootsRequest) (*mcp.ListRootsResult, error) {
	h.recordCall()
	return &mcp.ListRootsResult{Roots: h.roots}, nil
}

func (h *fixedElicitationHandler) Elicit(_ context.Context, _ mcp.ElicitationRequest) (*mcp.ElicitationResult, error) {
	h.recordCall()
	return &mcp.ElicitationResult{
		ElicitationResponse: mcp.ElicitationResponse{
			Action:  mcp.ElicitationResponseActionAccept,
			Content: h.response,
		},
	}, nil
}

type callCounter struct {
	mu    sync.Mutex
	count int
}

func (c *callCounter) recordCall() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count++
}

func (c *callCounter) calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}
