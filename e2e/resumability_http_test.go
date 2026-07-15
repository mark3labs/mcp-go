package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const sessionHeader = "Mcp-Session-Id"

const waitTimeout = 15 * time.Second

// ---------------------------------------------------------------------------
// Store instrumentation
// ---------------------------------------------------------------------------

type storeCall struct {
	sessionID string
	streamID  string
	message   json.RawMessage
	eventID   string
}

// recordingStore wraps the in-memory store and tracks every recorded event,
// so tests can wait for messages produced while no client is connected
// without polling or timing assumptions.
type recordingStore struct {
	inner  server.EventStore
	mu     sync.Mutex
	calls  []storeCall
	stored chan struct{}
}

func newRecordingStore() *recordingStore {
	return &recordingStore{
		inner:  server.NewInMemoryEventStore(),
		stored: make(chan struct{}, 4096),
	}
}

func (r *recordingStore) StoreEvent(ctx context.Context, sessionID, streamID string, message json.RawMessage) (string, error) {
	id, err := r.inner.StoreEvent(ctx, sessionID, streamID, message)
	msg := make(json.RawMessage, len(message))
	copy(msg, message)
	r.mu.Lock()
	r.calls = append(r.calls, storeCall{sessionID: sessionID, streamID: streamID, message: msg, eventID: id})
	r.mu.Unlock()
	r.stored <- struct{}{}
	return id, err
}

func (r *recordingStore) ReplayEventsAfter(ctx context.Context, sessionID, lastEventID string, send func(eventID string, message json.RawMessage) error) (string, error) {
	return r.inner.ReplayEventsAfter(ctx, sessionID, lastEventID, send)
}

func (r *recordingStore) PurgeSession(ctx context.Context, sessionID string) error {
	return r.inner.PurgeSession(ctx, sessionID)
}

// waitTotalStored blocks until at least n events have been recorded in total.
func (r *recordingStore) waitTotalStored(t *testing.T, n int) {
	t.Helper()
	deadline := time.After(waitTimeout)
	for {
		r.mu.Lock()
		count := len(r.calls)
		r.mu.Unlock()
		if count >= n {
			return
		}
		select {
		case <-r.stored:
		case <-deadline:
			t.Fatalf("timed out waiting for %d events to be recorded, have %d", n, count)
		}
	}
}

func (r *recordingStore) snapshot() []storeCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]storeCall, len(r.calls))
	copy(out, r.calls)
	return out
}

// ---------------------------------------------------------------------------
// SSE wire helpers
// ---------------------------------------------------------------------------

type sseEvent struct {
	id   string
	data json.RawMessage
}

// readSSEEvent reads the next SSE event from br. It accepts both "field:value"
// and "field: value" forms and returns io.EOF once the stream ends.
func readSSEEvent(br *bufio.Reader) (sseEvent, error) {
	var ev sseEvent
	seen := false
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return sseEvent{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if seen {
				return ev, nil
			}
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "id":
			ev.id = value
			seen = true
		case "data":
			ev.data = json.RawMessage(value)
			seen = true
		case "event":
			seen = true
		}
	}
}

// requireEvent reads one SSE event, failing the test on error or timeout.
func requireEvent(t *testing.T, br *bufio.Reader) sseEvent {
	t.Helper()
	type result struct {
		ev  sseEvent
		err error
	}
	ch := make(chan result, 1)
	go func() {
		ev, err := readSSEEvent(br)
		ch <- result{ev, err}
	}()
	select {
	case r := <-ch:
		require.NoError(t, r.err, "reading SSE event")
		return r.ev
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for an SSE event")
		return sseEvent{}
	}
}

// requireEOF asserts that the stream ends without any further events.
func requireEOF(t *testing.T, br *bufio.Reader) {
	t.Helper()
	type result struct {
		ev  sseEvent
		err error
	}
	ch := make(chan result, 1)
	go func() {
		ev, err := readSSEEvent(br)
		ch <- result{ev, err}
	}()
	select {
	case r := <-ch:
		require.ErrorIs(t, r.err, io.EOF, "expected end of stream, got event %q / error %v", string(r.ev.data), r.err)
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for the stream to end")
	}
}

// notificationData extracts params.data from a notifications/message payload.
func notificationData(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var n struct {
		Method string `json:"method"`
		Params struct {
			Data string `json:"data"`
		} `json:"params"`
	}
	require.NoError(t, json.Unmarshal(raw, &n))
	require.Equal(t, "notifications/message", n.Method)
	return n.Params.Data
}

// toolResponse parses a JSON-RPC tools/call response payload.
func toolResponse(t *testing.T, raw json.RawMessage) (int, string) {
	t.Helper()
	var rpc struct {
		ID     int `json:"id"`
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &rpc))
	require.NotEmpty(t, rpc.Result.Content, "expected tool result content in %s", string(raw))
	return rpc.ID, rpc.Result.Content[0].Text
}

// ---------------------------------------------------------------------------
// Server and HTTP helpers
// ---------------------------------------------------------------------------

// startServer builds an MCP server with two tools: "emitter", which sends a
// notifications/message per command received on the returned channel until
// told to return, and "quiet", which returns immediately without streaming.
func startServer(t *testing.T, store server.EventStore) (*server.MCPServer, *httptest.Server, chan string) {
	t.Helper()
	mcpServer := server.NewMCPServer("resumability-test-server", "1.0.0")

	cmds := make(chan string, 32)
	closeCmds := sync.OnceFunc(func() { close(cmds) })

	mcpServer.AddTool(mcp.NewTool("emitter"), func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		for cmd := range cmds {
			if cmd == "return" {
				break
			}
			if err := mcpServer.SendNotificationToClient(ctx, "notifications/message", map[string]any{"data": cmd}); err != nil {
				return nil, fmt.Errorf("send notification: %w", err)
			}
		}
		return mcp.NewToolResultText("done"), nil
	})
	mcpServer.AddTool(mcp.NewTool("quiet"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("quiet-done"), nil
	})

	var opts []server.StreamableHTTPOption
	if store != nil {
		opts = append(opts, server.WithEventStore(store))
	}
	ts := server.NewTestStreamableHTTPServer(mcpServer, opts...)
	t.Cleanup(ts.Close)
	// Unblock any emitter call still waiting for commands, so shutting the
	// test server down cannot hang on an in-flight request.
	t.Cleanup(closeCmds)
	return mcpServer, ts, cmds
}

func initSession(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	initBody := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q,"clientInfo":{"name":"resume-test","version":"1.0.0"},"capabilities":{}}}`,
		mcp.LATEST_PROTOCOL_VERSION,
	)
	resp, err := http.Post(ts.URL, "application/json", strings.NewReader(initBody))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	sessionID := resp.Header.Get(sessionHeader)
	require.NotEmpty(t, sessionID, "initialize response must carry a session ID")

	req, err := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sessionHeader, sessionID)
	resp2, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusAccepted, resp2.StatusCode)
	return sessionID
}

// openGET opens a listening (or, with lastEventID, resuming) GET stream.
func openGET(t *testing.T, ts *httptest.Server, sessionID, lastEventID string) (*http.Response, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL, nil)
	require.NoError(t, err)
	req.Header.Set(sessionHeader, sessionID)
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp, cancel
}

// startToolCall POSTs a tools/call for the emitter tool and returns a channel
// yielding the response once its headers arrive (i.e. once the response
// upgrades to SSE or completes).
func startToolCall(t *testing.T, ts *httptest.Server, sessionID string, requestID int) (<-chan postResult, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"emitter","arguments":{}}}`, requestID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sessionHeader, sessionID)

	ch := make(chan postResult, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		ch <- postResult{resp: resp, err: err}
	}()
	return ch, cancel
}

// postResult carries the outcome of an asynchronous POST: the response once
// its headers arrived, or the request error.
type postResult struct {
	resp *http.Response
	err  error
}

func waitResponse(t *testing.T, ch <-chan postResult) *http.Response {
	t.Helper()
	select {
	case r := <-ch:
		require.NoError(t, r.err, "tools/call request failed")
		t.Cleanup(func() { _ = r.resp.Body.Close() })
		return r.resp
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for HTTP response headers")
		return nil
	}
}

func sendNotification(t *testing.T, s *server.MCPServer, sessionID, data string) {
	t.Helper()
	require.NoError(t, s.SendNotificationToSpecificClient(sessionID, "notifications/message", map[string]any{"data": data}))
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// Without an event store configured, behavior is unchanged: SSE events carry
// no id fields, on either the listening stream or an upgraded POST response.
func TestResumability_NoEventStoreNoIDs(t *testing.T) {
	mcpServer, ts, cmds := startServer(t, nil)

	// Listening stream, on its own session.
	sidA := initSession(t, ts)
	resp, _ := openGET(t, ts, sidA, "")
	br := bufio.NewReader(resp.Body)
	sendNotification(t, mcpServer, sidA, "n1")
	ev := requireEvent(t, br)
	assert.Empty(t, ev.id, "events must not carry ids without an event store")
	assert.Equal(t, "n1", notificationData(t, ev.data))

	// Upgraded POST response, on a separate session so notification routing
	// is unambiguous.
	sidB := initSession(t, ts)
	respCh, _ := startToolCall(t, ts, sidB, 7)
	cmds <- "p1"
	postResp := waitResponse(t, respCh)
	pbr := bufio.NewReader(postResp.Body)
	evP := requireEvent(t, pbr)
	assert.Empty(t, evP.id)
	assert.Equal(t, "p1", notificationData(t, evP.data))
	cmds <- "return"
	evR := requireEvent(t, pbr)
	assert.Empty(t, evR.id)
	id, text := toolResponse(t, evR.data)
	assert.Equal(t, 7, id)
	assert.Equal(t, "done", text)
}

// With a store, live events on the listening stream carry unique, non-empty
// ids and arrive in order.
func TestResumability_ListeningStreamEventIDs(t *testing.T) {
	store := newRecordingStore()
	mcpServer, ts, _ := startServer(t, store)
	sid := initSession(t, ts)

	resp, _ := openGET(t, ts, sid, "")
	br := bufio.NewReader(resp.Body)

	seen := map[string]bool{}
	for i, want := range []string{"n1", "n2", "n3"} {
		sendNotification(t, mcpServer, sid, want)
		ev := requireEvent(t, br)
		require.NotEmpty(t, ev.id, "event %d must carry an id", i+1)
		require.False(t, seen[ev.id], "event ids must be unique, got %q twice", ev.id)
		seen[ev.id] = true
		assert.Equal(t, want, notificationData(t, ev.data))
	}
}

// Notifications produced while the listening client is disconnected are
// recorded and replayed, in order and with their original ids, and the
// resumed connection then continues receiving live.
func TestResumability_ListeningStreamResume(t *testing.T) {
	store := newRecordingStore()
	mcpServer, ts, _ := startServer(t, store)
	sid := initSession(t, ts)

	resp1, cancel1 := openGET(t, ts, sid, "")
	br1 := bufio.NewReader(resp1.Body)
	sendNotification(t, mcpServer, sid, "n1")
	sendNotification(t, mcpServer, sid, "n2")
	_ = requireEvent(t, br1)
	ev2 := requireEvent(t, br1)
	require.NotEmpty(t, ev2.id)

	// Drop the connection, then produce messages while nobody is listening.
	cancel1()
	sendNotification(t, mcpServer, sid, "n3")
	sendNotification(t, mcpServer, sid, "n4")
	store.waitTotalStored(t, 4)

	// Resume after n2: exactly n3 and n4 come back, then delivery goes live.
	resp2, cancel2 := openGET(t, ts, sid, ev2.id)
	br2 := bufio.NewReader(resp2.Body)
	ev3 := requireEvent(t, br2)
	assert.Equal(t, "n3", notificationData(t, ev3.data))
	require.NotEmpty(t, ev3.id)
	ev4 := requireEvent(t, br2)
	assert.Equal(t, "n4", notificationData(t, ev4.data))
	require.NotEmpty(t, ev4.id)

	sendNotification(t, mcpServer, sid, "n5")
	ev5 := requireEvent(t, br2)
	assert.Equal(t, "n5", notificationData(t, ev5.data))

	// Resuming again replays with the ids the events were originally
	// assigned.
	cancel2()
	store.waitTotalStored(t, 5)
	resp3, _ := openGET(t, ts, sid, ev3.id)
	br3 := bufio.NewReader(resp3.Body)
	ev4again := requireEvent(t, br3)
	assert.Equal(t, "n4", notificationData(t, ev4again.data))
	assert.Equal(t, ev4.id, ev4again.id, "replayed events keep their original ids")
	ev5again := requireEvent(t, br3)
	assert.Equal(t, "n5", notificationData(t, ev5again.data))
	assert.Equal(t, ev5.id, ev5again.id)
}

// When a POST connection breaks while its request is executing, the
// notifications the handler goes on to emit and its response are recorded;
// resuming from the last event the client saw redelivers them and the stream
// closes once the response has been delivered.
func TestResumability_InterruptedRequestCaptured(t *testing.T) {
	store := newRecordingStore()
	_, ts, cmds := startServer(t, store)
	sid := initSession(t, ts)

	respCh, cancelPost := startToolCall(t, ts, sid, 42)
	cmds <- "p1"
	postResp := waitResponse(t, respCh)
	br := bufio.NewReader(postResp.Body)
	evP1 := requireEvent(t, br)
	require.NotEmpty(t, evP1.id)
	require.Equal(t, "p1", notificationData(t, evP1.data))

	// Kill the connection mid-request, then let the handler finish its work.
	cancelPost()
	cmds <- "p2"
	cmds <- "p3"
	cmds <- "return"
	store.waitTotalStored(t, 4) // p1, p2, p3, response

	resp2, _ := openGET(t, ts, sid, evP1.id)
	br2 := bufio.NewReader(resp2.Body)
	evP2 := requireEvent(t, br2)
	assert.Equal(t, "p2", notificationData(t, evP2.data))
	evP3 := requireEvent(t, br2)
	assert.Equal(t, "p3", notificationData(t, evP3.data))
	evR := requireEvent(t, br2)
	id, text := toolResponse(t, evR.data)
	assert.Equal(t, 42, id)
	assert.Equal(t, "done", text)

	// The request's response has been delivered; the resumed stream ends.
	requireEOF(t, br2)
}

// Replay never mixes streams: resuming from an event of one request stream
// redelivers only that stream's messages, and ids are unique across all of a
// session's streams.
func TestResumability_StreamIsolationAndUniqueIDs(t *testing.T) {
	store := newRecordingStore()
	_, ts, cmds := startServer(t, store)
	sid := initSession(t, ts)

	// Request A: one live notification, then interrupted; finishes detached.
	respChA, cancelA := startToolCall(t, ts, sid, 1)
	cmds <- "a1"
	respA := waitResponse(t, respChA)
	brA := bufio.NewReader(respA.Body)
	evA1 := requireEvent(t, brA)
	require.Equal(t, "a1", notificationData(t, evA1.data))
	cancelA()
	cmds <- "a2"
	cmds <- "return"
	store.waitTotalStored(t, 3) // a1, a2, response A

	// Request B: runs to completion normally after A is done.
	respChB, _ := startToolCall(t, ts, sid, 2)
	cmds <- "b1"
	respB := waitResponse(t, respChB)
	brB := bufio.NewReader(respB.Body)
	evB1 := requireEvent(t, brB)
	require.Equal(t, "b1", notificationData(t, evB1.data))
	cmds <- "return"
	evBR := requireEvent(t, brB)
	idB, _ := toolResponse(t, evBR.data)
	require.Equal(t, 2, idB)
	store.waitTotalStored(t, 5)

	// Resuming from a1 yields a2 and A's response, and nothing of B's.
	resp, _ := openGET(t, ts, sid, evA1.id)
	br := bufio.NewReader(resp.Body)
	evA2 := requireEvent(t, br)
	assert.Equal(t, "a2", notificationData(t, evA2.data))
	evAR := requireEvent(t, br)
	idA, textA := toolResponse(t, evAR.data)
	assert.Equal(t, 1, idA)
	assert.Equal(t, "done", textA)
	requireEOF(t, br)

	// Resuming from b1 yields only B's response.
	resp2, _ := openGET(t, ts, sid, evB1.id)
	br2 := bufio.NewReader(resp2.Body)
	evBR2 := requireEvent(t, br2)
	idB2, _ := toolResponse(t, evBR2.data)
	assert.Equal(t, 2, idB2)
	assert.Equal(t, evBR.id, evBR2.id, "replayed events keep their original ids")
	requireEOF(t, br2)

	// All ids observed in the session are distinct.
	ids := []string{evA1.id, evA2.id, evAR.id, evB1.id, evBR.id}
	seen := map[string]bool{}
	for _, id := range ids {
		require.NotEmpty(t, id)
		require.False(t, seen[id], "event id %q is not unique within the session", id)
		seen[id] = true
	}
}

// A Last-Event-ID the store has never issued is rejected with 400, whether
// or not the session has recorded events.
func TestResumability_UnknownLastEventID(t *testing.T) {
	store := newRecordingStore()
	mcpServer, ts, _ := startServer(t, store)
	sid := initSession(t, ts)

	resp, _ := openGET(t, ts, sid, "no-such-event")
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// Record something, then still reject an unknown id.
	respGET, _ := openGET(t, ts, sid, "")
	sendNotification(t, mcpServer, sid, "n1")
	_ = requireEvent(t, bufio.NewReader(respGET.Body))

	resp2, _ := openGET(t, ts, sid, "still-not-an-id")
	assert.Equal(t, http.StatusBadRequest, resp2.StatusCode)
}

// Terminating a session purges its recorded events: an event id that resumed
// fine while the session lived is rejected after DELETE.
func TestResumability_TerminatedSessionCannotResume(t *testing.T) {
	mcpServer, ts, _ := startServer(t, server.NewInMemoryEventStore())
	sid := initSession(t, ts)

	resp, cancelGET := openGET(t, ts, sid, "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	br := bufio.NewReader(resp.Body)

	sendNotification(t, mcpServer, sid, "n1")
	sendNotification(t, mcpServer, sid, "n2")
	ev := requireEvent(t, br)
	require.NotEmpty(t, ev.id)
	cancelGET()

	// While the session lives, ev.id resumes and redelivers n2.
	resumed, cancelResumed := openGET(t, ts, sid, ev.id)
	require.Equal(t, http.StatusOK, resumed.StatusCode)
	require.Equal(t, "n2", notificationData(t, requireEvent(t, bufio.NewReader(resumed.Body)).data))
	cancelResumed()

	req, err := http.NewRequest(http.MethodDelete, ts.URL, nil)
	require.NoError(t, err)
	req.Header.Set(sessionHeader, sid)
	delResp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer delResp.Body.Close()
	require.Equal(t, http.StatusOK, delResp.StatusCode)

	rejected, _ := openGET(t, ts, sid, ev.id)
	assert.Equal(t, http.StatusBadRequest, rejected.StatusCode)
}

// An event id recorded for one session cannot be used to resume from another
// session.
func TestResumability_CrossSessionRejected(t *testing.T) {
	store := newRecordingStore()
	mcpServer, ts, _ := startServer(t, store)

	sidA := initSession(t, ts)
	respA, _ := openGET(t, ts, sidA, "")
	brA := bufio.NewReader(respA.Body)
	sendNotification(t, mcpServer, sidA, "a1")
	evA := requireEvent(t, brA)
	require.NotEmpty(t, evA.id)

	sidB := initSession(t, ts)
	respB, _ := openGET(t, ts, sidB, "")
	brB := bufio.NewReader(respB.Body)
	sendNotification(t, mcpServer, sidB, "b1")
	_ = requireEvent(t, brB)

	resp, _ := openGET(t, ts, sidB, evA.id)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "another session's event id must not resume this session")
}

// The event store is genuinely pluggable: ids the store issues are used
// verbatim however they look, StoreEvent receives a consistent stream
// identifier and the exact delivered payload, replayed events are whatever
// the store sends, and a store error on resume maps to 400.
func TestResumability_CustomStore(t *testing.T) {
	fake := &fakeStore{
		replayEvents: []sseEvent{
			{id: "made-up/2", data: json.RawMessage(`{"jsonrpc":"2.0","method":"notifications/message","params":{"data":"fabricated-1"}}`)},
			{id: "made-up/3", data: json.RawMessage(`{"jsonrpc":"2.0","method":"notifications/message","params":{"data":"fabricated-2"}}`)},
		},
	}
	mcpServer, ts, _ := startServer(t, fake)
	sid := initSession(t, ts)

	resp, _ := openGET(t, ts, sid, "")
	br := bufio.NewReader(resp.Body)
	sendNotification(t, mcpServer, sid, "n1")
	ev1 := requireEvent(t, br)
	assert.Equal(t, "custom~1*id", ev1.id, "store-issued ids must be used verbatim")
	sendNotification(t, mcpServer, sid, "n2")
	ev2 := requireEvent(t, br)
	assert.Equal(t, "custom~2*id", ev2.id)

	calls := fake.snapshot()
	require.Len(t, calls, 2)
	assert.Equal(t, sid, calls[0].sessionID)
	assert.Equal(t, calls[0].streamID, calls[1].streamID, "one stream must keep one stream id")
	assert.NotEmpty(t, calls[0].streamID)
	assert.JSONEq(t, string(ev1.data), string(calls[0].message), "the recorded message must be the delivered message")

	// Replay comes straight from the store.
	resp2, _ := openGET(t, ts, sid, "custom~1*id")
	br2 := bufio.NewReader(resp2.Body)
	evR1 := requireEvent(t, br2)
	assert.Equal(t, "made-up/2", evR1.id)
	assert.Equal(t, "fabricated-1", notificationData(t, evR1.data))
	evR2 := requireEvent(t, br2)
	assert.Equal(t, "made-up/3", evR2.id)
	assert.Equal(t, "fabricated-2", notificationData(t, evR2.data))
	requireEOF(t, br2)

	// A store that cannot resolve the id means 400.
	resp3, _ := openGET(t, ts, sid, "unresolvable")
	assert.Equal(t, http.StatusBadRequest, resp3.StatusCode)
}

// Responses returned as plain JSON are not part of any stream and are not
// recorded.
func TestResumability_PlainJSONNotRecorded(t *testing.T) {
	store := newRecordingStore()
	_, ts, _ := startServer(t, store)
	sid := initSession(t, ts)

	req, err := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"quiet","arguments":{}}}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sessionHeader, sid)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	id, text := toolResponse(t, body)
	assert.Equal(t, 9, id)
	assert.Equal(t, "quiet-done", text)

	assert.Empty(t, store.snapshot(), "plain JSON responses must not be recorded")
}

// A reconnect for a stream supersedes an earlier connection still attached to
// it: the old connection is closed and delivery continues on the new one.
func TestResumability_ReconnectSupersedes(t *testing.T) {
	store := newRecordingStore()
	mcpServer, ts, _ := startServer(t, store)
	sid := initSession(t, ts)

	resp1, _ := openGET(t, ts, sid, "")
	br1 := bufio.NewReader(resp1.Body)
	sendNotification(t, mcpServer, sid, "n1")
	ev1 := requireEvent(t, br1)
	require.NotEmpty(t, ev1.id)

	// Resume the same stream while the first connection is still open.
	resp2, _ := openGET(t, ts, sid, ev1.id)
	br2 := bufio.NewReader(resp2.Body)

	// The superseded connection ends...
	requireEOF(t, br1)

	// ...and new messages arrive on the replacement.
	sendNotification(t, mcpServer, sid, "n2")
	ev2 := requireEvent(t, br2)
	assert.Equal(t, "n2", notificationData(t, ev2.data))
}

// ---------------------------------------------------------------------------
// fakeStore
// ---------------------------------------------------------------------------

// fakeStore issues deliberately odd event ids and serves replay entirely from
// canned data, to verify the server treats stores as the source of truth.
type fakeStore struct {
	mu           sync.Mutex
	calls        []storeCall
	replayEvents []sseEvent
}

func (f *fakeStore) StoreEvent(_ context.Context, sessionID, streamID string, message json.RawMessage) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	msg := make(json.RawMessage, len(message))
	copy(msg, message)
	id := fmt.Sprintf("custom~%d*id", len(f.calls)+1)
	f.calls = append(f.calls, storeCall{sessionID: sessionID, streamID: streamID, message: msg, eventID: id})
	return id, nil
}

func (f *fakeStore) PurgeSession(context.Context, string) error { return nil }

func (f *fakeStore) ReplayEventsAfter(_ context.Context, _ string, lastEventID string, send func(eventID string, message json.RawMessage) error) (string, error) {
	if lastEventID != "custom~1*id" {
		return "", fmt.Errorf("%w: %q", server.ErrUnknownEventID, lastEventID)
	}
	f.mu.Lock()
	events := make([]sseEvent, len(f.replayEvents))
	copy(events, f.replayEvents)
	f.mu.Unlock()
	for _, ev := range events {
		if err := send(ev.id, ev.data); err != nil {
			return "", err
		}
	}
	return "stream-the-server-never-issued", nil
}

func (f *fakeStore) snapshot() []storeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]storeCall, len(f.calls))
	copy(out, f.calls)
	return out
}
