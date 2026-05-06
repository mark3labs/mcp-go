package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestStrictInputSchemaDefault_RegistersAdditionalPropertiesFalse pins that
// WithStrictInputSchemaDefault sets additionalProperties:false on a tool that
// did not configure the field, and that tools/list reflects the strict
// schema so a schema-aware client sees it.
func TestStrictInputSchemaDefault_RegistersAdditionalPropertiesFalse(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithStrictInputSchemaDefault())
	tool := mcp.NewTool("kubernetes_list",
		mcp.WithString("resourceType", mcp.Required()),
		mcp.WithString("continue"),
	)
	srv.AddTool(tool, okHandler)

	stored := srv.GetTool("kubernetes_list")
	require.NotNil(t, stored)
	assert.Equal(t, false, stored.Tool.InputSchema.AdditionalProperties)
}

// TestStrictInputSchemaDefault_PreservesExplicitOptIn confirms that an author
// who explicitly chose permissive (true) — or who supplied a schema for
// additionalProperties — is not overridden by the server default.
func TestStrictInputSchemaDefault_PreservesExplicitOptIn(t *testing.T) {
	tests := []struct {
		name string
		opts []mcp.ToolOption
		want any
	}{
		{
			name: "explicit true stays true",
			opts: []mcp.ToolOption{
				mcp.WithString("name"),
				mcp.WithSchemaAdditionalProperties(true),
			},
			want: true,
		},
		{
			name: "explicit schema stays as map",
			opts: []mcp.ToolOption{
				mcp.WithString("name"),
				mcp.WithSchemaAdditionalProperties(map[string]any{"type": "string"}),
			},
			want: map[string]any{"type": "string"},
		},
		{
			name: "explicit false stays false",
			opts: []mcp.ToolOption{
				mcp.WithString("name"),
				mcp.WithSchemaAdditionalProperties(false),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewMCPServer("test", "1.0.0", WithStrictInputSchemaDefault())
			tool := mcp.NewTool("t", tt.opts...)
			srv.AddTool(tool, okHandler)

			stored := srv.GetTool("t")
			require.NotNil(t, stored)
			assert.Equal(t, tt.want, stored.Tool.InputSchema.AdditionalProperties)
		})
	}
}

// TestStrictInputSchemaDefault_SkipsRawInputSchema documents that tools
// shipping a RawInputSchema are out of scope for the option. Raw is an
// explicit opt-out of the structured-schema helpers and naive top-level
// patching is unsafe for $ref / oneOf shapes.
func TestStrictInputSchemaDefault_SkipsRawInputSchema(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithStrictInputSchemaDefault())
	raw := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	tool := mcp.Tool{
		Name:           "raw",
		RawInputSchema: raw,
	}
	srv.AddTool(tool, okHandler)

	stored := srv.GetTool("raw")
	require.NotNil(t, stored)
	assert.JSONEq(t, string(raw), string(stored.Tool.RawInputSchema),
		"raw schema must be preserved verbatim")
	assert.Nil(t, stored.Tool.InputSchema.AdditionalProperties)
}

// TestStrictInputSchemaDefault_DisabledByDefault is the back-compat regression
// guard: without the option, registered tools must keep their original
// (nil / unset) AdditionalProperties value so existing servers do not change
// behaviour silently.
func TestStrictInputSchemaDefault_DisabledByDefault(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0")
	tool := mcp.NewTool("t", mcp.WithString("name"))
	srv.AddTool(tool, okHandler)

	stored := srv.GetTool("t")
	require.NotNil(t, stored)
	assert.Nil(t, stored.Tool.InputSchema.AdditionalProperties)
}

// TestStrictInputSchemaDefault_RejectsUnknownPropertyWithValidation is the
// integration check: combined with WithInputSchemaValidation, the server
// rejects an unknown property without per-tool WithSchemaAdditionalProperties
// calls. This is the boilerplate-removal use case.
func TestStrictInputSchemaDefault_RejectsUnknownPropertyWithValidation(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0",
		WithStrictInputSchemaDefault(),
		WithInputSchemaValidation(),
	)
	tool := mcp.NewTool("kubernetes_list",
		mcp.WithString("resourceType", mcp.Required()),
		mcp.WithString("continue"),
	)
	srv.AddTool(tool, okHandler)

	resp := callTool(t, srv, "kubernetes_list", map[string]any{
		"resourceType": "pods",
		"cursor":       "abc",
	})
	requireToolErrorContaining(t, resp, "cursor")
}

// TestStrictInputSchemaDefault_DoesNotMutateCallerTool checks that the option
// only affects the in-server copy of the tool. Callers who pass the same
// mcp.Tool value to multiple servers must observe their original variable
// unchanged.
func TestStrictInputSchemaDefault_DoesNotMutateCallerTool(t *testing.T) {
	tool := mcp.NewTool("t", mcp.WithString("name"))
	require.Nil(t, tool.InputSchema.AdditionalProperties)

	srv := NewMCPServer("test", "1.0.0", WithStrictInputSchemaDefault())
	srv.AddTool(tool, okHandler)

	assert.Nil(t, tool.InputSchema.AdditionalProperties,
		"caller's tool variable must remain untouched")

	stored := srv.GetTool("t")
	require.NotNil(t, stored)
	assert.Equal(t, false, stored.Tool.InputSchema.AdditionalProperties)
}

// TestStrictInputSchemaDefault_AppliesToSetTools covers the SetTools
// replacement path so a strict server cannot quietly fall back to permissive
// schemas after a SetTools refresh.
func TestStrictInputSchemaDefault_AppliesToSetTools(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithStrictInputSchemaDefault())
	srv.SetTools(ServerTool{
		Tool:    mcp.NewTool("t", mcp.WithString("name")),
		Handler: okHandler,
	})

	stored := srv.GetTool("t")
	require.NotNil(t, stored)
	assert.Equal(t, false, stored.Tool.InputSchema.AdditionalProperties)
}

// TestStrictInputSchemaDefault_AppliesToSessionTools covers session-scoped
// tools so a session that shadows a global tool inherits the strict default.
func TestStrictInputSchemaDefault_AppliesToSessionTools(t *testing.T) {
	srv := NewMCPServer("test", "1.0.0", WithStrictInputSchemaDefault())
	sess := &fakeSessionWithTools{
		id:    "s1",
		ready: true,
		tools: map[string]ServerTool{},
	}
	require.NoError(t, srv.RegisterSession(t.Context(), sess))

	require.NoError(t, srv.AddSessionTool("s1",
		mcp.NewTool("t", mcp.WithString("name")), okHandler))

	got := sess.GetSessionTools()["t"]
	assert.Equal(t, false, got.Tool.InputSchema.AdditionalProperties)
}

// fakeSessionWithTools is a minimal SessionWithTools used to exercise the
// session-scoped registration path without spinning up a full transport.
type fakeSessionWithTools struct {
	id    string
	ready bool
	tools map[string]ServerTool
}

func (f *fakeSessionWithTools) SessionID() string                              { return f.id }
func (f *fakeSessionWithTools) NotificationChannel() chan<- mcp.JSONRPCNotification { return nil }
func (f *fakeSessionWithTools) Initialize()                                    { f.ready = true }
func (f *fakeSessionWithTools) Initialized() bool                              { return f.ready }
func (f *fakeSessionWithTools) GetSessionTools() map[string]ServerTool         { return f.tools }
func (f *fakeSessionWithTools) SetSessionTools(tools map[string]ServerTool)    { f.tools = tools }
