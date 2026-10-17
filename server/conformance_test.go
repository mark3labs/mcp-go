package server

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rogpeppe/go-internal/txtar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Conformance tests check the JSON that goes out on the wire, rather than the
// Go values behind it. That is the level at which interoperability with the
// other MCP SDKs is decided: two implementations can agree on every Go type
// and still disagree about whether an optional field is omitted, null, or an
// empty object.
//
// Each test is a txtar archive under testdata/conformance. The "client"
// section holds the JSON-RPC messages to feed the server, one per line or as
// consecutive JSON values; the "server" section holds the responses expected
// back, and is regenerated with -update.

var updateConformance = flag.Bool("update", false, "update conformance test data")

type conformanceTest struct {
	name    string
	path    string
	archive *txtar.Archive
	client  []json.RawMessage
	server  []json.RawMessage
}

func TestConformance(t *testing.T) {
	dir := filepath.Join("testdata", "conformance")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err, "conformance testdata is missing")

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txtar") {
			continue
		}
		test := loadConformanceTest(t, filepath.Join(dir, entry.Name()))
		t.Run(test.name, func(t *testing.T) {
			runConformanceTest(t, test)
		})
	}
}

func loadConformanceTest(t *testing.T, path string) conformanceTest {
	t.Helper()

	archive, err := txtar.ParseFile(path)
	require.NoError(t, err)

	test := conformanceTest{
		name:    strings.TrimSuffix(filepath.Base(path), ".txtar"),
		path:    path,
		archive: archive,
	}
	for _, file := range archive.Files {
		switch file.Name {
		case "client":
			test.client = splitJSONValues(t, file.Data)
		case "server":
			test.server = splitJSONValues(t, file.Data)
		default:
			t.Fatalf("%s: unknown section %q", path, file.Name)
		}
	}
	require.NotEmpty(t, test.client, "%s: no client messages", path)
	return test
}

// splitJSONValues decodes a section into the sequence of JSON values it holds,
// so that messages may be written either one per line or pretty-printed.
func splitJSONValues(t *testing.T, data []byte) []json.RawMessage {
	t.Helper()

	decoder := json.NewDecoder(strings.NewReader(string(data)))
	var values []json.RawMessage
	for {
		var value json.RawMessage
		err := decoder.Decode(&value)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decoding conformance section: %v", err)
		}
		values = append(values, value)
	}
	return values
}

func runConformanceTest(t *testing.T, test conformanceTest) {
	t.Helper()

	srv := newConformanceServer()
	session := newConformanceSession()
	ctx := srv.WithContext(t.Context(), session)

	var got []json.RawMessage
	for _, message := range test.client {
		response := srv.HandleMessage(ctx, message)
		if response == nil {
			// Notifications produce no reply.
			continue
		}
		encoded, err := json.Marshal(response)
		require.NoError(t, err)
		got = append(got, normalizeJSON(t, encoded))
	}

	if *updateConformance {
		updateConformanceArchive(t, test, got)
		return
	}

	require.Len(t, got, len(test.server),
		"%s: got %d responses, want %d", test.path, len(got), len(test.server))

	for i := range got {
		want := normalizeJSON(t, test.server[i])
		assert.Equal(t, string(want), string(got[i]),
			"%s: response %d does not match the recorded wire format", test.path, i)
	}
}

// normalizeJSON re-encodes a JSON value with sorted keys and stable
// indentation, so that comparisons ignore formatting and field order.
func normalizeJSON(t *testing.T, data []byte) []byte {
	t.Helper()

	var value any
	require.NoError(t, json.Unmarshal(data, &value))

	normalized, err := json.MarshalIndent(value, "", "\t")
	require.NoError(t, err)
	return normalized
}

func updateConformanceArchive(t *testing.T, test conformanceTest, got []json.RawMessage) {
	t.Helper()

	var section strings.Builder
	for i, message := range got {
		if i > 0 {
			section.WriteString("\n")
		}
		section.Write(message)
		section.WriteString("\n")
	}

	replaced := false
	for i := range test.archive.Files {
		if test.archive.Files[i].Name == "server" {
			test.archive.Files[i].Data = []byte(section.String())
			replaced = true
		}
	}
	if !replaced {
		test.archive.Files = append(test.archive.Files, txtar.File{
			Name: "server",
			Data: []byte(section.String()),
		})
	}

	require.NoError(t, os.WriteFile(test.path, txtar.Format(test.archive), 0o644))
	t.Logf("updated %s", test.path)
}

// newConformanceServer builds the fixed server every conformance archive is
// written against. Changing it invalidates the recorded responses.
func newConformanceServer() *MCPServer {
	srv := NewMCPServer("testServer", "v1.0.0",
		WithInstructions("a server for conformance testing"),
		WithToolCapabilities(true),
		WithResourceCapabilities(true, true),
		WithPromptCapabilities(true),
		WithElicitation(),
	)

	srv.AddTool(
		mcp.NewTool("greet",
			mcp.WithDescription("greets the caller"),
			mcp.WithString("name", mcp.Description("who to greet"), mcp.Required()),
		),
		func(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("hello " + request.GetString("name", "")), nil
		},
	)

	srv.AddTool(
		mcp.NewTool("confirmThenGreet", mcp.WithDescription("greets after confirming a name")),
		func(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if answer := ElicitationResponse(request.Params.InputResponses, "who"); answer != nil {
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
		},
	)

	return srv
}

// conformanceSession is a fixed session for conformance runs.
type conformanceSession struct {
	clientInfoStore
	notify chan mcp.JSONRPCNotification
}

func newConformanceSession() *conformanceSession {
	return &conformanceSession{notify: make(chan mcp.JSONRPCNotification, 64)}
}

func (s *conformanceSession) SessionID() string { return "conformance" }
func (s *conformanceSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return s.notify
}
func (s *conformanceSession) Initialize()       {}
func (s *conformanceSession) Initialized() bool { return true }

var _ ClientSession = (*conformanceSession)(nil)
