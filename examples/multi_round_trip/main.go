// Command multi_round_trip demonstrates the multi round-trip request pattern
// introduced in MCP protocol version 2026-07-28 (SEP-2322).
//
// A tool that needs something from the user mid-call cannot send the client a
// request on a stateless protocol, so it returns the requests it needs
// answered instead. The client fulfils them and retries the original call with
// the answers attached, and an opaque request state echoed back so the handler
// can resume where it left off.
//
// The handler below is written once and serves clients of either protocol era:
// against a client predating this revision, mcp-go issues the
// elicitation/create that client understands and re-invokes the handler with
// the answer.
//
// Run it with:
//
//	go run ./examples/multi_round_trip
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// confirmKey identifies the input request across the two round trips. The
// server chooses it, and the client echoes it back as the key of its answer.
const confirmKey = "confirm"

func main() {
	ctx := context.Background()

	srv := newServer()

	// An in-process transport keeps the example to a single command; the same
	// code works over Streamable HTTP.
	c := client.NewClient(
		transport.NewInProcessTransport(srv),
		client.WithElicitationHandler(&autoApprover{}),
	)
	if err := c.Start(ctx); err != nil {
		log.Fatalf("starting client: %v", err)
	}
	defer c.Close()

	var initRequest mcp.InitializeRequest
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "mrtr-example", Version: "1.0.0"}

	initResult, err := c.Initialize(ctx, initRequest)
	if err != nil {
		log.Fatalf("connecting: %v", err)
	}
	fmt.Printf("connected using protocol version %s\n\n", initResult.ProtocolVersion)

	// The call below completes in one line of application code, but takes two
	// round trips: the server asks for confirmation, the client's elicitation
	// handler answers, and the client retries automatically.
	var call mcp.CallToolRequest
	call.Params.Name = "deploy"
	call.Params.Arguments = map[string]any{"environment": "production"}

	result, err := c.CallTool(ctx, call)
	if err != nil {
		log.Fatalf("calling deploy: %v", err)
	}

	for _, content := range result.Content {
		if text, ok := content.(mcp.TextContent); ok {
			fmt.Println(text.Text)
		}
	}
}

func newServer() *server.MCPServer {
	srv := server.NewMCPServer("deploy-server", "1.0.0",
		server.WithToolCapabilities(true),
		server.WithElicitation(),
	)

	srv.AddTool(
		mcp.NewTool("deploy",
			mcp.WithDescription("deploys the application, after confirming with the user"),
			mcp.WithString("environment",
				mcp.Description("the environment to deploy to"),
				mcp.Required(),
			),
		),
		deploy,
	)

	return srv
}

// deploy asks the user to confirm before doing anything irreversible.
func deploy(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	environment := request.GetString("environment", "staging")

	// On the retry, the client's answer is waiting under the key the first
	// call asked about.
	if answer := server.ElicitationResponse(request.Params.InputResponses, confirmKey); answer != nil {
		if answer.Action != mcp.ElicitationResponseActionAccept {
			return mcp.NewToolResultText("deployment cancelled"), nil
		}

		content, _ := answer.Content.(map[string]any)
		if approved, _ := content["approve"].(bool); !approved {
			return mcp.NewToolResultText("deployment declined"), nil
		}

		// The request state is whatever the first call handed out, echoed back
		// verbatim. Here it carries the environment, so the handler need not
		// re-derive it.
		target := strings.TrimPrefix(request.Params.RequestState, "env=")
		return mcp.NewToolResultText("deployed to " + target), nil
	}

	// First call: ask, and hand back the state needed to resume. The client
	// treats it as opaque, so it must not carry anything secret.
	return server.NewInputRequestBuilder("env="+environment).
		Elicit(confirmKey, mcp.ElicitationParams{
			Mode:    mcp.ElicitationModeForm,
			Message: fmt.Sprintf("Deploy to %s?", environment),
			RequestedSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"approve": map[string]any{
						"type":        "boolean",
						"description": "whether to proceed",
					},
				},
				"required": []string{"approve"},
			},
		}).
		ToolResult(), nil
}

// autoApprover stands in for a real client's user interface.
type autoApprover struct{}

func (*autoApprover) Elicit(
	_ context.Context,
	request mcp.ElicitationRequest,
) (*mcp.ElicitationResult, error) {
	fmt.Printf("server asks: %s\n", request.Params.Message)
	fmt.Println("client answers: yes")

	return &mcp.ElicitationResult{
		ElicitationResponse: mcp.ElicitationResponse{
			Action:  mcp.ElicitationResponseActionAccept,
			Content: map[string]any{"approve": true},
		},
	}, nil
}
