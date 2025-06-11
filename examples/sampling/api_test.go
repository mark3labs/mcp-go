package main

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// TestSamplingAPI tests the sampling API components
func TestSamplingAPI(t *testing.T) {
	t.Run("Server Sampling Capability", func(t *testing.T) {
		// Test server with sampling capability
		mcpServer := server.NewMCPServer(
			"test-server",
			"1.0.0",
			server.WithSampling(),
		)

		// Verify the server was created successfully
		if mcpServer == nil {
			t.Error("Server creation failed")
		}

		t.Log("✅ Server sampling capability test passed")
	})

	t.Run("Client Sampling Handler", func(t *testing.T) {
		// Test client with sampling handler
		handler := client.SamplingHandlerFunc(func(ctx context.Context, req *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
			return &mcp.CreateMessageResult{
				Role:    mcp.RoleAssistant,
				Content: mcp.NewTextContent("Test response"),
				Model:   "test-model",
			}, nil
		})

		// This should not panic and should properly set up the handler
		option := client.WithSamplingHandler(handler)
		if option == nil {
			t.Error("Sampling handler option creation failed")
		}

		t.Log("✅ Client sampling handler test passed")
	})

	t.Run("Sampling Types", func(t *testing.T) {
		// Test sampling message creation
		message := mcp.NewSamplingMessage(mcp.RoleUser, mcp.NewTextContent("Test message"))
		if message.Role != mcp.RoleUser {
			t.Error("Sampling message role not set correctly")
		}

		textContent, ok := mcp.AsTextContent(message.Content)
		if !ok || textContent.Text != "Test message" {
			t.Error("Sampling message content not set correctly")
		}

		// Test request creation
		request := mcp.NewCreateMessageRequest([]mcp.SamplingMessage{message})
		if len(request.Messages) != 1 {
			t.Error("Create message request not created correctly")
		}

		// Test result creation
		result := mcp.NewCreateMessageResult(mcp.RoleAssistant, mcp.NewTextContent("Response"), "test-model")
		if result.Role != mcp.RoleAssistant || result.Model != "test-model" {
			t.Error("Create message result not created correctly")
		}

		t.Log("✅ Sampling types test passed")
	})

	t.Run("Sampling Options", func(t *testing.T) {
		// Test sampling options
		params := mcp.CreateMessageParams{}

		// Apply options
		server.WithTemperature(0.5)(&params)
		if params.Temperature == nil || *params.Temperature != 0.5 {
			t.Error("Temperature option not applied correctly")
		}

		server.WithMaxTokens(100)(&params)
		if params.MaxTokens == nil || *params.MaxTokens != 100 {
			t.Error("MaxTokens option not applied correctly")
		}

		server.WithModel("gpt-4")(&params)
		if params.ModelPreferences == nil || len(params.ModelPreferences.Hints) != 1 || params.ModelPreferences.Hints[0].Name != "gpt-4" {
			t.Error("Model option not applied correctly")
		}

		systemPrompt := "You are a helpful assistant"
		server.WithSystemPrompt(systemPrompt)(&params)
		if params.SystemPrompt == nil || *params.SystemPrompt != systemPrompt {
			t.Error("SystemPrompt option not applied correctly")
		}

		server.WithContext(mcp.ContextThisServer)(&params)
		if params.IncludeContext == nil || *params.IncludeContext != mcp.ContextThisServer {
			t.Error("Context option not applied correctly")
		}

		server.WithStopSequences("END", "STOP")(&params)
		if len(params.StopSequences) != 2 || params.StopSequences[0] != "END" || params.StopSequences[1] != "STOP" {
			t.Error("StopSequences option not applied correctly")
		}

		t.Log("✅ Sampling options test passed")
	})

	t.Run("Sampling Context Retrieval", func(t *testing.T) {
		// Test that sampling context can be retrieved (even if nil)
		ctx := context.Background()
		samplingCtx := server.SamplingContextFromContext(ctx)

		// This should be nil since we haven't set up a proper context
		if samplingCtx != nil {
			t.Error("Sampling context should be nil without proper setup")
		}

		t.Log("✅ Sampling context retrieval test passed")
	})

	t.Run("Sampling Error Types", func(t *testing.T) {
		// Test sampling error creation
		err := client.NewSamplingError(client.ErrCodeSamplingNotSupported, "test error")
		if err.Code != client.ErrCodeSamplingNotSupported || err.Message != "test error" {
			t.Error("Sampling error not created correctly")
		}

		if err.Error() != "test error" {
			t.Error("Sampling error Error() method not working correctly")
		}

		t.Log("✅ Sampling error types test passed")
	})

	t.Log("🎉 All sampling API tests passed!")
}
