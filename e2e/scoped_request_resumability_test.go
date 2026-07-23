package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// With an event store configured, a server request issued while a POST is
// being handled (here elicitation/create from the tool handler) is both
// recorded for resumability and delivered on that POST's own SSE stream with
// an id, before the final tool response. This is the intersection of #930
// (resumability) and the request-scoped delivery of #932.
func TestResumability_RequestScopedElicitationCaptured(t *testing.T) {
	store := newRecordingStore()

	mcpServer := server.NewMCPServer("scoped-resume-test-server", "1.0.0")
	mcpServer.AddTool(mcp.NewTool("confirm"), func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := mcpServer.RequestElicitation(ctx, mcp.ElicitationRequest{
			Params: mcp.ElicitationParams{
				Message:         "Proceed?",
				RequestedSchema: map[string]any{"type": "object"},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("request elicitation: %w", err)
		}
		return mcp.NewToolResultText("action=" + string(result.Action)), nil
	})

	ts := server.NewTestStreamableHTTPServer(mcpServer, server.WithEventStore(store))
	t.Cleanup(ts.Close)

	// The client answers elicitations, so it advertises the capability.
	initBody := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q,"clientInfo":{"name":"scoped-resume-test","version":"1.0.0"},"capabilities":{"elicitation":{}}}}`,
		mcp.LATEST_PROTOCOL_VERSION,
	)
	initResp, err := http.Post(ts.URL, "application/json", strings.NewReader(initBody))
	require.NoError(t, err)
	defer initResp.Body.Close()
	require.Equal(t, http.StatusOK, initResp.StatusCode)
	sid := initResp.Header.Get(sessionHeader)
	require.NotEmpty(t, sid)

	callReq, err := http.NewRequest(http.MethodPost, ts.URL,
		strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"confirm","arguments":{}}}`))
	require.NoError(t, err)
	callReq.Header.Set("Content-Type", "application/json")
	callReq.Header.Set(sessionHeader, sid)
	callResp, err := http.DefaultClient.Do(callReq)
	require.NoError(t, err)
	defer callResp.Body.Close()
	br := bufio.NewReader(callResp.Body)

	// The elicitation arrives on the POST stream, ahead of the tool result,
	// carrying an SSE id because the stream is recorded.
	elicEvent := requireEvent(t, br)
	require.NotEmpty(t, elicEvent.id, "request-scoped elicitation must be delivered with an SSE id")
	var elic struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	require.NoError(t, json.Unmarshal(elicEvent.data, &elic))
	require.Equal(t, string(mcp.MethodElicitationCreate), elic.Method)

	// Answer it on a separate POST, as a client would.
	answerReq, err := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(
		fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"action":"accept","content":{"confirmed":true}}}`, elic.ID)))
	require.NoError(t, err)
	answerReq.Header.Set("Content-Type", "application/json")
	answerReq.Header.Set(sessionHeader, sid)
	answerResp, err := http.DefaultClient.Do(answerReq)
	require.NoError(t, err)
	answerResp.Body.Close()
	require.Equal(t, http.StatusAccepted, answerResp.StatusCode)

	// The tool result then arrives on the same POST stream.
	resultEvent := requireEvent(t, br)
	id, text := toolResponse(t, resultEvent.data)
	assert.Equal(t, 7, id)
	assert.Equal(t, "action=accept", text)

	// The scoped request was recorded, under the POST's own request stream
	// (not the session id), with the same id that was written to the stream.
	var recorded storeCall
	found := false
	for _, c := range store.snapshot() {
		var m struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(c.message, &m); err == nil && m.Method == string(mcp.MethodElicitationCreate) {
			recorded = c
			found = true
			break
		}
	}
	require.True(t, found, "the request-scoped elicitation must be recorded in the event store")
	assert.Equal(t, sid, recorded.sessionID)
	assert.Equal(t, elicEvent.id, recorded.eventID, "the SSE id on the POST stream is the event id the store assigned")
	assert.NotEmpty(t, recorded.streamID)
	assert.NotEqual(t, sid, recorded.streamID, "recorded under the POST request's own resumable stream")
}
