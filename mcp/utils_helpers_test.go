package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helper functions with 0% coverage

func TestNewProgressNotification(t *testing.T) {
	token := ProgressToken("test-token")
	progress := 50.0
	total := 100.0
	message := "Processing..."

	result := NewProgressNotification(token, progress, &total, &message)

	assert.Equal(t, "notifications/progress", result.Method)
	assert.Equal(t, token, result.Params.ProgressToken)
	assert.Equal(t, progress, result.Params.Progress)
	assert.Equal(t, total, result.Params.Total)
	assert.Equal(t, message, result.Params.Message)
}

func TestNewProgressNotification_WithNils(t *testing.T) {
	token := ProgressToken("test-token")
	progress := 50.0

	result := NewProgressNotification(token, progress, nil, nil)

	assert.Equal(t, "notifications/progress", result.Method)
	assert.Equal(t, token, result.Params.ProgressToken)
	assert.Equal(t, progress, result.Params.Progress)
	assert.Equal(t, 0.0, result.Params.Total)
	assert.Equal(t, "", result.Params.Message)
}

func TestNewLoggingMessageNotification(t *testing.T) {
	level := LoggingLevelInfo
	logger := "test-logger"
	data := map[string]any{"key": "value"}

	result := NewLoggingMessageNotification(level, logger, data)

	assert.Equal(t, "notifications/message", result.Method)
	assert.Equal(t, level, result.Params.Level)
	assert.Equal(t, logger, result.Params.Logger)
	assert.Equal(t, data, result.Params.Data)
}

func TestNewToolResultImage(t *testing.T) {
	text := "Image result"
	imageData := "base64imagedata"
	mimeType := "image/png"

	result := NewToolResultImage(text, imageData, mimeType)

	require.Len(t, result.Content, 2)

	textContent, ok := result.Content[0].(TextContent)
	require.True(t, ok)
	assert.Equal(t, text, textContent.Text)

	imageContent, ok := result.Content[1].(ImageContent)
	require.True(t, ok)
	assert.Equal(t, imageData, imageContent.Data)
	assert.Equal(t, mimeType, imageContent.MIMEType)
}

func TestNewToolResultAudio(t *testing.T) {
	text := "Audio result"
	audioData := "base64audiodata"
	mimeType := "audio/mp3"

	result := NewToolResultAudio(text, audioData, mimeType)

	require.Len(t, result.Content, 2)

	textContent, ok := result.Content[0].(TextContent)
	require.True(t, ok)
	assert.Equal(t, text, textContent.Text)

	audioContent, ok := result.Content[1].(AudioContent)
	require.True(t, ok)
	assert.Equal(t, audioData, audioContent.Data)
	assert.Equal(t, mimeType, audioContent.MIMEType)
}

func TestNewToolResultResource(t *testing.T) {
	text := "Resource result"
	resource := TextResourceContents{
		URI:      "file:///test.txt",
		MIMEType: "text/plain",
		Text:     "content",
	}

	result := NewToolResultResource(text, resource)

	require.Len(t, result.Content, 2)

	textContent, ok := result.Content[0].(TextContent)
	require.True(t, ok)
	assert.Equal(t, text, textContent.Text)

	embeddedResource, ok := result.Content[1].(EmbeddedResource)
	require.True(t, ok)
	assert.Equal(t, resource, embeddedResource.Resource)
}

func TestNewToolResultErrorf(t *testing.T) {
	result := NewToolResultErrorf("error code: %d, message: %s", 404, "not found")

	assert.True(t, result.IsError)
	require.Len(t, result.Content, 1)

	textContent, ok := result.Content[0].(TextContent)
	require.True(t, ok)
	assert.Equal(t, "error code: 404, message: not found", textContent.Text)
}

func TestNewListResourcesResult(t *testing.T) {
	resources := []Resource{
		{URI: "file:///test1.txt", Name: "test1.txt"},
		{URI: "file:///test2.txt", Name: "test2.txt"},
	}
	cursor := Cursor("next-page")

	result := NewListResourcesResult(resources, cursor)

	assert.Equal(t, resources, result.Resources)
	assert.Equal(t, cursor, result.NextCursor)
}

func TestNewListResourceTemplatesResult(t *testing.T) {
	templates := []ResourceTemplate{
		{Name: "template1"},
		{Name: "template2"},
	}
	cursor := Cursor("next-page")

	result := NewListResourceTemplatesResult(templates, cursor)

	assert.Equal(t, templates, result.ResourceTemplates)
	assert.Equal(t, cursor, result.NextCursor)
}

func TestNewReadResourceResult(t *testing.T) {
	text := "file content"

	result := NewReadResourceResult(text)

	require.Len(t, result.Contents, 1)
	textContents, ok := result.Contents[0].(TextResourceContents)
	require.True(t, ok)
	assert.Equal(t, text, textContents.Text)
}

func TestNewListPromptsResult(t *testing.T) {
	prompts := []Prompt{
		{Name: "prompt1"},
		{Name: "prompt2"},
	}
	cursor := Cursor("next-page")

	result := NewListPromptsResult(prompts, cursor)

	assert.Equal(t, prompts, result.Prompts)
	assert.Equal(t, cursor, result.NextCursor)
}

func TestNewGetPromptResult(t *testing.T) {
	description := "Test prompt"
	messages := []PromptMessage{
		{Role: RoleUser, Content: TextContent{Text: "Hello"}},
	}

	result := NewGetPromptResult(description, messages)

	assert.Equal(t, description, result.Description)
	assert.Equal(t, messages, result.Messages)
}

func TestNewListToolsResult(t *testing.T) {
	tools := []Tool{
		{Name: "tool1"},
		{Name: "tool2"},
	}
	cursor := Cursor("next-page")

	result := NewListToolsResult(tools, cursor)

	assert.Equal(t, tools, result.Tools)
	assert.Equal(t, cursor, result.NextCursor)
}

func TestNewInitializeResult(t *testing.T) {
	version := "1.0"
	capabilities := ServerCapabilities{}
	serverInfo := Implementation{Name: "test-server", Version: "1.0"}
	instructions := "Use this server carefully"

	result := NewInitializeResult(version, capabilities, serverInfo, instructions)

	assert.Equal(t, version, result.ProtocolVersion)
	assert.Equal(t, capabilities, result.Capabilities)
	assert.Equal(t, serverInfo, result.ServerInfo)
	assert.Equal(t, instructions, result.Instructions)
}
