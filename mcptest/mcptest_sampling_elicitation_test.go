package mcptest_test

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/mcptest"
	"github.com/mark3labs/mcp-go/server"
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
	if samplingHandler.callCount != 1 {
		t.Errorf("expected sampling handler called once, got %d", samplingHandler.callCount)
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
	if elicitationHandler.callCount != 1 {
		t.Errorf("expected elicitation handler called once, got %d", elicitationHandler.callCount)
	}
}

// fixedSamplingHandler is a test double that always returns a preset text reply.
type fixedSamplingHandler struct {
	reply     string
	callCount int
}

func (h *fixedSamplingHandler) CreateMessage(_ context.Context, _ mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	h.callCount++
	return &mcp.CreateMessageResult{
		SamplingMessage: mcp.SamplingMessage{
			Role:    mcp.RoleAssistant,
			Content: mcp.NewTextContent(h.reply),
		},
		Model:      "test-model",
		StopReason: "endTurn",
	}, nil
}

// fixedElicitationHandler is a test double that always accepts with a preset content map.
type fixedElicitationHandler struct {
	response  map[string]any
	callCount int
}

func (h *fixedElicitationHandler) Elicit(_ context.Context, _ mcp.ElicitationRequest) (*mcp.ElicitationResult, error) {
	h.callCount++
	return &mcp.ElicitationResult{
		ElicitationResponse: mcp.ElicitationResponse{
			Action:  mcp.ElicitationResponseActionAccept,
			Content: h.response,
		},
	}, nil
}
