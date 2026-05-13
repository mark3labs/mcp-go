package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToolTitleMarshaling verifies that Tool.Title round-trips through JSON
// and is omitted when empty.
func TestToolTitleMarshaling(t *testing.T) {
	t.Run("omitted when empty", func(t *testing.T) {
		tool := NewTool("my_tool", WithDescription("desc"))
		data, err := json.Marshal(tool)
		require.NoError(t, err)
		assert.NotContains(t, string(data), `"title"`)
	})

	t.Run("included when set via option", func(t *testing.T) {
		tool := NewTool("my_tool",
			WithToolTitle("My Friendly Tool"),
			WithDescription("desc"),
		)
		assert.Equal(t, "My Friendly Tool", tool.Title)

		data, err := json.Marshal(tool)
		require.NoError(t, err)

		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))
		assert.Equal(t, "My Friendly Tool", m["title"])
	})

	t.Run("round-trips through unmarshal", func(t *testing.T) {
		raw := `{"name":"t","title":"Display Name","inputSchema":{"type":"object"},"annotations":{}}`
		var tool Tool
		require.NoError(t, json.Unmarshal([]byte(raw), &tool))
		assert.Equal(t, "Display Name", tool.Title)
	})
}

// TestPromptTitleMarshaling verifies Prompt.Title round-trips through JSON.
func TestPromptTitleMarshaling(t *testing.T) {
	t.Run("omitted when empty", func(t *testing.T) {
		p := NewPrompt("p")
		data, err := json.Marshal(p)
		require.NoError(t, err)
		assert.NotContains(t, string(data), `"title"`)
	})

	t.Run("included when set via option", func(t *testing.T) {
		p := NewPrompt("p",
			WithPromptTitle("Pretty Prompt"),
			WithPromptDescription("desc"),
		)
		assert.Equal(t, "Pretty Prompt", p.Title)

		data, err := json.Marshal(p)
		require.NoError(t, err)
		assert.Contains(t, string(data), `"title":"Pretty Prompt"`)

		var rt Prompt
		require.NoError(t, json.Unmarshal(data, &rt))
		assert.Equal(t, "Pretty Prompt", rt.Title)
	})
}

// TestPromptArgumentTitle verifies that PromptArgument.Title round-trips and
// can be set via ArgumentTitle.
func TestPromptArgumentTitle(t *testing.T) {
	p := NewPrompt("p",
		WithArgument("arg",
			ArgumentTitle("Argument Display"),
			ArgumentDescription("an arg"),
			RequiredArgument(),
		),
	)

	require.Len(t, p.Arguments, 1)
	assert.Equal(t, "Argument Display", p.Arguments[0].Title)

	data, err := json.Marshal(p)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"title":"Argument Display"`)

	var rt Prompt
	require.NoError(t, json.Unmarshal(data, &rt))
	require.Len(t, rt.Arguments, 1)
	assert.Equal(t, "Argument Display", rt.Arguments[0].Title)
}

// TestResourceTitleAndSize verifies the new Resource fields.
func TestResourceTitleAndSize(t *testing.T) {
	t.Run("title and size omitted when unset", func(t *testing.T) {
		r := NewResource("file:///x.txt", "x.txt")
		data, err := json.Marshal(r)
		require.NoError(t, err)
		s := string(data)
		assert.NotContains(t, s, `"title"`)
		assert.NotContains(t, s, `"size"`)
	})

	t.Run("title and size included when set", func(t *testing.T) {
		r := NewResource("file:///x.txt", "x.txt",
			WithResourceTitle("X File"),
			WithResourceSize(1024),
		)
		assert.Equal(t, "X File", r.Title)
		require.NotNil(t, r.Size)
		assert.Equal(t, int64(1024), *r.Size)

		data, err := json.Marshal(r)
		require.NoError(t, err)
		assert.Contains(t, string(data), `"title":"X File"`)
		assert.Contains(t, string(data), `"size":1024`)

		var rt Resource
		require.NoError(t, json.Unmarshal(data, &rt))
		assert.Equal(t, "X File", rt.Title)
		require.NotNil(t, rt.Size)
		assert.Equal(t, int64(1024), *rt.Size)
	})

	t.Run("explicit zero size is preserved", func(t *testing.T) {
		// Pointer semantics let us distinguish a known-zero-byte resource from
		// an unknown/unset size.
		r := NewResource("file:///empty.txt", "empty.txt",
			WithResourceSize(0),
		)
		require.NotNil(t, r.Size)
		assert.Equal(t, int64(0), *r.Size)

		data, err := json.Marshal(r)
		require.NoError(t, err)
		assert.Contains(t, string(data), `"size":0`)
	})

	t.Run("negative size is ignored", func(t *testing.T) {
		// Size is a byte count per the MCP schema; negative values are nonsensical
		// and silently dropped rather than serialised back to clients.
		r := NewResource("file:///x.txt", "x.txt",
			WithResourceSize(-1),
		)
		assert.Nil(t, r.Size)

		data, err := json.Marshal(r)
		require.NoError(t, err)
		assert.NotContains(t, string(data), `"size"`)
	})

	t.Run("negative size does not overwrite a previously set size", func(t *testing.T) {
		r := NewResource("file:///x.txt", "x.txt",
			WithResourceSize(100),
			WithResourceSize(-5),
		)
		require.NotNil(t, r.Size)
		assert.Equal(t, int64(100), *r.Size)
	})
}

// TestResourceTemplateTitle verifies that ResourceTemplate.Title round-trips.
func TestResourceTemplateTitle(t *testing.T) {
	rt := NewResourceTemplate("file:///{path}", "files",
		WithTemplateTitle("All Files"),
		WithTemplateDescription("desc"),
	)
	assert.Equal(t, "All Files", rt.Title)

	data, err := json.Marshal(rt)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"title":"All Files"`)

	var decoded ResourceTemplate
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "All Files", decoded.Title)
}

// TestIconTheme verifies the new Icon.Theme field and constants.
func TestIconTheme(t *testing.T) {
	t.Run("theme omitted when empty", func(t *testing.T) {
		icon := Icon{Src: "https://example.com/icon.png"}
		data, err := json.Marshal(icon)
		require.NoError(t, err)
		assert.NotContains(t, string(data), `"theme"`)
	})

	t.Run("light theme marshals and unmarshals", func(t *testing.T) {
		icon := Icon{
			Src:   "https://example.com/light.png",
			Theme: IconThemeLight,
		}
		data, err := json.Marshal(icon)
		require.NoError(t, err)
		assert.JSONEq(t,
			`{"src":"https://example.com/light.png","theme":"light"}`,
			string(data),
		)

		var decoded Icon
		require.NoError(t, json.Unmarshal(data, &decoded))
		assert.Equal(t, IconThemeLight, decoded.Theme)
	})

	t.Run("dark theme constant value", func(t *testing.T) {
		assert.Equal(t, IconTheme("dark"), IconThemeDark)
		assert.Equal(t, IconTheme("light"), IconThemeLight)
	})

	t.Run("unknown theme value unmarshals as-is", func(t *testing.T) {
		// Forward-compat: unknown theme strings should round-trip rather than error.
		var icon Icon
		err := json.Unmarshal(
			[]byte(`{"src":"x","theme":"sepia"}`),
			&icon,
		)
		require.NoError(t, err)
		assert.Equal(t, IconTheme("sepia"), icon.Theme)
	})
}
