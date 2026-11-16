package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// serverRequestKind is a server request a tool handler issues and waits on.
type serverRequestKind struct {
	name   string
	method mcp.MCPMethod
	// ask issues the request and returns text derived from the client's answer.
	ask func(ctx context.Context, s *MCPServer) (string, error)
	// answer is the client's result for the call tagged tag, and want the
	// tool result that answer produces.
	answer func(tag string) any
	want   func(tag string) string
}

var serverRequestKinds = []serverRequestKind{
	{
		name:   "roots",
		method: mcp.MethodListRoots,
		ask: func(ctx context.Context, s *MCPServer) (string, error) {
			result, err := s.RequestRoots(ctx, mcp.ListRootsRequest{})
			if err != nil {
				return "", err
			}
			if len(result.Roots) != 1 {
				return "", fmt.Errorf("expected one root, got %d", len(result.Roots))
			}
			return result.Roots[0].URI, nil
		},
		answer: func(tag string) any {
			return map[string]any{"roots": []map[string]any{{"uri": "file:///" + tag, "name": tag}}}
		},
		want: func(tag string) string { return "file:///" + tag },
	},
	{
		name:   "sampling",
		method: mcp.MethodSamplingCreateMessage,
		ask: func(ctx context.Context, s *MCPServer) (string, error) {
			result, err := s.RequestSampling(ctx, mcp.CreateMessageRequest{
				CreateMessageParams: mcp.CreateMessageParams{
					Messages:  []mcp.SamplingMessage{{Role: mcp.RoleUser, Content: mcp.NewTextContent("hi")}},
					MaxTokens: 10,
				},
			})
			if err != nil {
				return "", err
			}
			return "model=" + result.Model, nil
		},
		answer: func(tag string) any {
			return map[string]any{
				"role": "assistant", "model": tag,
				"content": map[string]any{"type": "text", "text": "ok"},
			}
		},
		want: func(tag string) string { return "model=" + tag },
	},
	{
		// Elicitation has taken the POST stream since #933 and is routed
		// the same way.
		name:   "elicitation",
		method: mcp.MethodElicitationCreate,
		ask: func(ctx context.Context, s *MCPServer) (string, error) {
			result, err := s.RequestElicitation(ctx, mcp.ElicitationRequest{
				Params: mcp.ElicitationParams{
					Message:         "Which?",
					RequestedSchema: map[string]any{"type": "object"},
				},
			})
			if err != nil {
				return "", err
			}
			content, _ := result.Content.(map[string]any)
			return fmt.Sprintf("choice=%v", content["choice"]), nil
		},
		answer: func(tag string) any {
			return map[string]any{"action": "accept", "content": map[string]any{"choice": tag}}
		},
		want: func(tag string) string { return "choice=" + tag },
	},
}

// sessionMode is a way for two POSTs to end up on one session ID.
type sessionMode struct {
	name string
	opts []StreamableHTTPOption
	// sessionID returns a session ID the server accepts, a new one on each
	// call.
	sessionID func(t *testing.T, url string) string
}

var sessionModes = []sessionMode{
	{
		// The session is registered when it is initialized.
		name:      "stateful",
		opts:      []StreamableHTTPOption{WithStateful(true)},
		sessionID: initializeSession,
	},
	{
		// No session is ever registered: the server accepts whatever ID
		// the client sends.
		name:      "stateless",
		opts:      []StreamableHTTPOption{WithStateLess(true)},
		sessionID: func(*testing.T, string) string { return "client-chosen-" + uuid.NewString() },
	},
	{
		// The default manager accepts an ID it did not issue, such as one
		// from another instance, so here too nothing is registered.
		name:      "default manager with a session initialized elsewhere",
		sessionID: func(*testing.T, string) string { return new(StatelessGeneratingSessionIdManager).Generate() },
	},
}

// overlapFixture serves one tool, "ask", whose handler reports that it has
// started, waits to be released, issues the kind's server request, and
// returns what the client answered.
type overlapFixture struct {
	kind serverRequestKind
	url  string
	// sessionID is the session every POST is sent on, and otherSessionID a
	// second session the server accepts.
	sessionID      string
	otherSessionID string
	entered        chan struct{}
	release        chan struct{}
}

func newOverlapFixture(t *testing.T, kind serverRequestKind, mode sessionMode) *overlapFixture {
	t.Helper()
	f := &overlapFixture{
		kind:    kind,
		entered: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	mcpServer := NewMCPServer("test-server", "1.0.0", WithRoots())
	mcpServer.EnableSampling()
	mcpServer.AddTool(
		mcp.NewTool("ask"),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			f.entered <- struct{}{}
			select {
			case <-f.release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			text, err := kind.ask(ctx, mcpServer)
			if err != nil {
				return nil, err
			}
			return mcp.NewToolResultText(text), nil
		},
	)
	testServer := httptest.NewServer(NewStreamableHTTPServer(mcpServer, mode.opts...))
	t.Cleanup(testServer.Close)
	f.url = testServer.URL
	f.sessionID = mode.sessionID(t, testServer.URL)
	f.otherSessionID = mode.sessionID(t, testServer.URL)
	return f
}

// askCall is one tools/call POST for "ask" and its SSE response.
type askCall struct {
	id     int
	resp   *http.Response
	reader *bufio.Reader
}

// post sends a tools/call for "ask" with the JSON-RPC id and returns once the
// server has started its SSE response. It does not use t, so it can run
// outside the test goroutine.
func (f *overlapFixture) post(id int) (*askCall, error) {
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "tools/call",
		"params": map[string]any{"name": "ask"},
	})
	req, _ := http.NewRequest(http.MethodPost, f.url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set(HeaderKeySessionID, f.sessionID)
	resp, err := scopedRequestClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tools/call %d failed: %w", id, err)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("tools/call %d: expected an SSE response, got status %d, Content-Type %q, body %q",
			id, resp.StatusCode, ct, raw)
	}
	return &askCall{id: id, resp: resp, reader: bufio.NewReader(resp.Body)}, nil
}

// serverRequest reads the server request the call's handler issued on the
// call's own stream, and returns its ID.
func (f *overlapFixture) serverRequest(t *testing.T, call *askCall) json.RawMessage {
	t.Helper()
	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	readSSEData(t, call.reader, &request)
	require.Equal(t, string(f.kind.method), request.Method, "call %d: first event on its POST stream", call.id)
	return request.ID
}

// answer posts the client's response to a server request on the fixture's
// session and returns the HTTP status. The result is the kind's answer for
// tag.
func (f *overlapFixture) answer(t *testing.T, requestID json.RawMessage, tag string) int {
	t.Helper()
	return f.answerOn(t, f.sessionID, requestID, tag)
}

// answerOn is answer on the given session.
func (f *overlapFixture) answerOn(t *testing.T, sessionID string, requestID json.RawMessage, tag string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": requestID, "result": f.kind.answer(tag)})
	req, _ := http.NewRequest(http.MethodPost, f.url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set(HeaderKeySessionID, sessionID)
	resp, err := scopedRequestClient.Do(req)
	require.NoError(t, err, "answering server request %s", requestID)
	resp.Body.Close()
	return resp.StatusCode
}

// finish reads the call's tool result, checks that it carries the answer
// tagged tag, and reads the stream to its end, which the server reaches
// only after the POST handler has returned.
func (f *overlapFixture) finish(t *testing.T, call *askCall, tag string) {
	t.Helper()
	var final struct {
		ID     int `json:"id"`
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	readSSEData(t, call.reader, &final)
	require.Equal(t, call.id, final.ID, "final response: %+v", final)
	require.False(t, final.Result.IsError, "call %d: tool error: %+v", call.id, final.Result)
	require.Len(t, final.Result.Content, 1, "call %d", call.id)
	require.Equal(t, f.kind.want(tag), final.Result.Content[0].Text,
		"call %d: its tool result must carry the answer to its own request", call.id)
	_, err := io.Copy(io.Discard, call.reader)
	require.NoError(t, err, "call %d: reading its stream to the end", call.id)
	call.resp.Body.Close()
}

// forEachOverlapCase runs test for every server request kind in every
// session mode.
func forEachOverlapCase(t *testing.T, test func(t *testing.T, f *overlapFixture)) {
	for _, kind := range serverRequestKinds {
		for _, mode := range sessionModes {
			t.Run(kind.name+"/"+mode.name, func(t *testing.T) {
				test(t, newOverlapFixture(t, kind, mode))
			})
		}
	}
}

// Two POSTs on one session ID overlap: both tool handlers are running before
// either issues its server request, so neither POST can borrow the other's
// state. The client's response to each request must reach the handler that
// issued it.
func TestStreamableHTTPServer_OverlappingPOSTsRouteServerRequestResponses(t *testing.T) {
	forEachOverlapCase(t, func(t *testing.T, f *overlapFixture) {
		type posted struct {
			call *askCall
			err  error
		}
		// A POST returns only once its handler has issued the server
		// request, which needs the gate opened, so both are sent from
		// goroutines.
		results := map[int]chan posted{1: make(chan posted, 1), 2: make(chan posted, 1)}
		for id, result := range results {
			go func() {
				call, err := f.post(id)
				result <- posted{call, err}
			}()
		}
		for range results {
			select {
			case <-f.entered:
			case <-time.After(10 * time.Second):
				t.Fatal("the tool handlers did not both start")
			}
		}
		close(f.release)

		calls := make(map[int]*askCall, len(results))
		for id, result := range results {
			p := <-result
			require.NoError(t, p.err)
			defer p.call.resp.Body.Close()
			calls[id] = p.call
		}
		first, second := calls[1], calls[2]
		firstRequest, secondRequest := f.serverRequest(t, first), f.serverRequest(t, second)

		// Both requests are pending at once. Answer them in the reverse
		// order, each with its own result.
		require.Equal(t, http.StatusAccepted, f.answer(t, secondRequest, "second"),
			"response to the second POST's %s", f.kind.method)
		require.Equal(t, http.StatusAccepted, f.answer(t, firstRequest, "first"),
			"response to the first POST's %s", f.kind.method)
		f.finish(t, first, "first")
		f.finish(t, second, "second")
	})
}

// One POST finishes while its peer on the same session ID still waits for
// the client's response. The peer's response must still be routed: the first
// POST must not take the routing for the session away with it.
func TestStreamableHTTPServer_POSTFinishingBeforePeerKeepsPeerRoutable(t *testing.T) {
	forEachOverlapCase(t, func(t *testing.T, f *overlapFixture) {
		// The interleaving is driven from here, so the handlers need no gate.
		close(f.release)

		// The first POST's request is on the wire before the peer starts.
		first, err := f.post(1)
		require.NoError(t, err)
		defer first.resp.Body.Close()
		firstRequest := f.serverRequest(t, first)

		peer, err := f.post(2)
		require.NoError(t, err)
		defer peer.resp.Body.Close()
		peerRequest := f.serverRequest(t, peer)

		// The first POST completes, and its handler returns, with the
		// peer's request still pending.
		require.Equal(t, http.StatusAccepted, f.answer(t, firstRequest, "first"),
			"response to the first POST's %s", f.kind.method)
		f.finish(t, first, "first")

		require.Equal(t, http.StatusAccepted, f.answer(t, peerRequest, "peer"),
			"response to the peer's %s after the first POST finished", f.kind.method)
		f.finish(t, peer, "peer")

		// Both requests are settled, so nothing is left to route to.
		for _, settled := range []json.RawMessage{firstRequest, peerRequest} {
			require.NotEqual(t, http.StatusAccepted, f.answer(t, settled, "late"),
				"a second response to settled request %s must be refused", settled)
		}
	})
}

// Pending requests of every session are routed from one table, so a response
// must name the session its request was sent to: one posted on another
// session is refused and leaves the request pending.
func TestStreamableHTTPServer_ServerRequestResponseConfinedToItsSession(t *testing.T) {
	forEachOverlapCase(t, func(t *testing.T, f *overlapFixture) {
		close(f.release)

		call, err := f.post(1)
		require.NoError(t, err)
		defer call.resp.Body.Close()
		request := f.serverRequest(t, call)

		require.NotEqual(t, http.StatusAccepted, f.answerOn(t, f.otherSessionID, request, "intruder"),
			"a response to %s posted on another session must be refused", f.kind.method)
		require.Equal(t, http.StatusAccepted, f.answer(t, request, "owner"),
			"response to %s on its own session", f.kind.method)
		f.finish(t, call, "owner")
	})
}
