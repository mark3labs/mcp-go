package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPrompt(t *testing.T) {
	tests := []struct {
		name     string
		prompt   Prompt
		expected Prompt
	}{
		{
			name:   "basic prompt",
			prompt: NewPrompt("test-prompt"),
			expected: Prompt{
				Name: "test-prompt",
			},
		},
		{
			name: "prompt with description",
			prompt: NewPrompt("test-prompt",
				WithPromptDescription("A test prompt")),
			expected: Prompt{
				Name:        "test-prompt",
				Description: "A test prompt",
			},
		},
		{
			name: "prompt with single argument",
			prompt: NewPrompt("test-prompt",
				WithPromptDescription("Test prompt with arg"),
				WithArgument("query",
					ArgumentDescription("Search query"),
					RequiredArgument())),
			expected: Prompt{
				Name:        "test-prompt",
				Description: "Test prompt with arg",
				Arguments: []PromptArgument{
					{
						Name:        "query",
						Description: "Search query",
						Required:    true,
					},
				},
			},
		},
		{
			name: "prompt with multiple arguments",
			prompt: NewPrompt("search-prompt",
				WithPromptDescription("Search with filters"),
				WithArgument("query",
					ArgumentDescription("Search query"),
					RequiredArgument()),
				WithArgument("limit",
					ArgumentDescription("Max results")),
				WithArgument("offset",
					ArgumentDescription("Starting position"))),
			expected: Prompt{
				Name:        "search-prompt",
				Description: "Search with filters",
				Arguments: []PromptArgument{
					{
						Name:        "query",
						Description: "Search query",
						Required:    true,
					},
					{
						Name:        "limit",
						Description: "Max results",
						Required:    false,
					},
					{
						Name:        "offset",
						Description: "Starting position",
						Required:    false,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected.Name, tt.prompt.Name)
			assert.Equal(t, tt.expected.Description, tt.prompt.Description)
			assert.Equal(t, tt.expected.Arguments, tt.prompt.Arguments)
		})
	}
}

func TestPromptGetName(t *testing.T) {
	prompt := NewPrompt("my-prompt",
		WithPromptDescription("Test prompt"))

	assert.Equal(t, "my-prompt", prompt.GetName())
}

func TestPromptJSONMarshaling(t *testing.T) {
	tests := []struct {
		name   string
		prompt Prompt
	}{
		{
			name: "simple prompt",
			prompt: NewPrompt("simple",
				WithPromptDescription("A simple prompt")),
		},
		{
			name: "prompt with required argument",
			prompt: NewPrompt("with-arg",
				WithPromptDescription("Prompt with argument"),
				WithArgument("name",
					ArgumentDescription("User name"),
					RequiredArgument())),
		},
		{
			name: "prompt with optional arguments",
			prompt: NewPrompt("optional-args",
				WithArgument("field1", ArgumentDescription("First field")),
				WithArgument("field2", ArgumentDescription("Second field"))),
		},
		{
			name: "complex prompt",
			prompt: NewPrompt("complex",
				WithPromptDescription("Complex prompt template"),
				WithArgument("query", ArgumentDescription("Search query"), RequiredArgument()),
				WithArgument("limit", ArgumentDescription("Result limit")),
				WithArgument("sort", ArgumentDescription("Sort order"), RequiredArgument())),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to JSON
			data, err := json.Marshal(tt.prompt)
			require.NoError(t, err)

			// Unmarshal back
			var unmarshaled Prompt
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)

			// Compare
			assert.Equal(t, tt.prompt.Name, unmarshaled.Name)
			assert.Equal(t, tt.prompt.Description, unmarshaled.Description)
			assert.Equal(t, tt.prompt.Arguments, unmarshaled.Arguments)
		})
	}
}

func TestWithArgument(t *testing.T) {
	t.Run("single argument", func(t *testing.T) {
		prompt := NewPrompt("test")
		opt := WithArgument("arg1", ArgumentDescription("First arg"))
		opt(&prompt)

		require.Len(t, prompt.Arguments, 1)
		assert.Equal(t, "arg1", prompt.Arguments[0].Name)
		assert.Equal(t, "First arg", prompt.Arguments[0].Description)
		assert.False(t, prompt.Arguments[0].Required)
	})

	t.Run("multiple arguments", func(t *testing.T) {
		prompt := NewPrompt("test")
		opt1 := WithArgument("arg1", RequiredArgument())
		opt2 := WithArgument("arg2", ArgumentDescription("Second"))

		opt1(&prompt)
		opt2(&prompt)

		require.Len(t, prompt.Arguments, 2)
		assert.Equal(t, "arg1", prompt.Arguments[0].Name)
		assert.True(t, prompt.Arguments[0].Required)
		assert.Equal(t, "arg2", prompt.Arguments[1].Name)
		assert.Equal(t, "Second", prompt.Arguments[1].Description)
	})

	t.Run("argument with no options", func(t *testing.T) {
		prompt := NewPrompt("test")
		opt := WithArgument("simple")
		opt(&prompt)

		require.Len(t, prompt.Arguments, 1)
		assert.Equal(t, "simple", prompt.Arguments[0].Name)
		assert.Empty(t, prompt.Arguments[0].Description)
		assert.False(t, prompt.Arguments[0].Required)
	})
}

func TestArgumentDescription(t *testing.T) {
	arg := PromptArgument{}
	opt := ArgumentDescription("Test description")
	opt(&arg)

	assert.Equal(t, "Test description", arg.Description)
}

func TestRequiredArgument(t *testing.T) {
	arg := PromptArgument{}
	opt := RequiredArgument()
	opt(&arg)

	assert.True(t, arg.Required)
}

func TestWithPromptDescription(t *testing.T) {
	prompt := Prompt{}
	opt := WithPromptDescription("Test prompt description")
	opt(&prompt)

	assert.Equal(t, "Test prompt description", prompt.Description)
}

func TestPromptMessageCreation(t *testing.T) {
	tests := []struct {
		name    string
		role    Role
		content Content
	}{
		{
			name:    "user text message",
			role:    RoleUser,
			content: NewTextContent("Hello"),
		},
		{
			name:    "assistant text message",
			role:    RoleAssistant,
			content: NewTextContent("Hi there"),
		},
		{
			name:    "user image message",
			role:    RoleUser,
			content: NewImageContent("base64data", "image/png"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := NewPromptMessage(tt.role, tt.content)

			assert.Equal(t, tt.role, msg.Role)
			assert.Equal(t, tt.content, msg.Content)
		})
	}
}

func TestPromptJSONStructure(t *testing.T) {
	prompt := NewPrompt("test-prompt",
		WithPromptDescription("Test description"),
		WithArgument("arg1",
			ArgumentDescription("First argument"),
			RequiredArgument()),
		WithArgument("arg2",
			ArgumentDescription("Second argument")))

	data, err := json.Marshal(prompt)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	// Verify structure
	assert.Equal(t, "test-prompt", result["name"])
	assert.Equal(t, "Test description", result["description"])

	args, ok := result["arguments"].([]any)
	require.True(t, ok)
	require.Len(t, args, 2)

	arg1, ok := args[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "arg1", arg1["name"])
	assert.Equal(t, "First argument", arg1["description"])
	assert.Equal(t, true, arg1["required"])

	arg2, ok := args[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "arg2", arg2["name"])
	assert.Equal(t, "Second argument", arg2["description"])
	// Optional arguments may not have "required" field or it's false
}
