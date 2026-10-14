package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// Regression test for #932: an elicitation issued while a POST request is
// being handled must be delivered on that POST's SSE stream, before the final
// response, instead of the standalone GET stream.
func TestStreamableHTTPServer_ElicitationOnOriginatingPOSTStream(t *testing.T) {
	mcpServer := NewMCPServer("test-server", "1.0.0")
	mcpServer.AddTool(
		mcp.NewTool("confirm"),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			result, err := mcpServer.RequestElicitation(ctx, mcp.ElicitationRequest{
				Params: mcp.ElicitationParams{
					Message:         "Proceed?",
					RequestedSchema: map[string]any{"type": "object"},
				},
			})
			if err != nil {
				return nil, err
			}
			return mcp.NewToolResultText("action=" + string(result.Action)), nil
		},
	)

	httpServer := NewStreamableHTTPServer(mcpServer)
	testServer := httptest.NewServer(httpServer)
	defer testServer.Close()

	sessionID := initializeSession(t, testServer.URL)

	callBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      7,
		"method":  "tools/call",
		"params":  map[string]any{"name": "confirm"},
	}
	bodyBytes, _ := json.Marshal(callBody)
	req, _ := http.NewRequest(http.MethodPost, testServer.URL, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set(HeaderKeySessionID, sessionID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("tools/call request failed: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected SSE response, got Content-Type %q", ct)
	}

	reader := bufio.NewReader(resp.Body)

	// First event: the elicitation request, delivered before the tool result.
	var elicitation struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			Message string `json:"message"`
		} `json:"params"`
	}
	readSSEData(t, reader, &elicitation)
	if elicitation.Method != string(mcp.MethodElicitationCreate) {
		t.Fatalf("expected first SSE event to be %s, got %q", mcp.MethodElicitationCreate, elicitation.Method)
	}
	if elicitation.Params.Message != "Proceed?" {
		t.Fatalf("unexpected elicitation message %q", elicitation.Params.Message)
	}

	// Answer the elicitation on a separate POST, as a client would.
	responseBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(elicitation.ID),
		"result": map[string]any{
			"action":  "accept",
			"content": map[string]any{"confirmed": true},
		},
	}
	respBytes, _ := json.Marshal(responseBody)
	answer, _ := http.NewRequest(http.MethodPost, testServer.URL, bytes.NewReader(respBytes))
	answer.Header.Set("Content-Type", "application/json")
	answer.Header.Set("Accept", "application/json, text/event-stream")
	answer.Header.Set(HeaderKeySessionID, sessionID)
	answerResp, err := http.DefaultClient.Do(answer)
	if err != nil {
		t.Fatalf("elicitation response request failed: %v", err)
	}
	answerResp.Body.Close()
	if answerResp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 for elicitation response, got %d", answerResp.StatusCode)
	}

	// Second event on the same POST stream: the final tool result.
	var final struct {
		ID     int64 `json:"id"`
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	readSSEData(t, reader, &final)
	if final.ID != 7 {
		t.Fatalf("expected final response for request 7, got %d", final.ID)
	}
	if len(final.Result.Content) == 0 || final.Result.Content[0].Text != "action=accept" {
		t.Fatalf("unexpected tool result: %+v", final.Result)
	}
}

// Without a request-scoped stream in the context, elicitation still goes to
// the standalone GET stream path.
func TestStreamableHttpSession_ElicitationFallsBackToSessionChannel(t *testing.T) {
	session := newStreamableHttpSession("s1", nil, nil, nil, nil, new(atomic.Int64))

	go func() {
		item := <-session.elicitationRequestChan
		item.response <- samplingResponseItem{
			requestID: item.requestID,
			result:    json.RawMessage(`{"action":"decline"}`),
		}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result, err := session.RequestElicitation(ctx, mcp.ElicitationRequest{
		Params: mcp.ElicitationParams{
			Message:         "fallback",
			RequestedSchema: map[string]any{"type": "object"},
		},
	})
	if err != nil {
		t.Fatalf("RequestElicitation failed: %v", err)
	}
	if result.Action != mcp.ElicitationResponseActionDecline {
		t.Fatalf("expected decline, got %q", result.Action)
	}
}

func initializeSession(t *testing.T, url string) string {
	t.Helper()
	initBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": mcp.LATEST_PROTOCOL_VERSION,
			"clientInfo":      map[string]any{"name": "test-client", "version": "1.0.0"},
			"capabilities":    map[string]any{"elicitation": map[string]any{}},
		},
	}
	bodyBytes, _ := json.Marshal(initBody)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	defer resp.Body.Close()
	sessionID := resp.Header.Get(HeaderKeySessionID)
	if sessionID == "" {
		t.Fatal("missing session id in initialize response")
	}
	return sessionID
}

func readSSEData(t *testing.T, reader *bufio.Reader, v any) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for SSE event")
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("reading SSE stream: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			if err := json.Unmarshal([]byte(data), v); err != nil {
				t.Fatalf("unmarshaling SSE data %q: %v", data, err)
			}
			return
		}
	}
}

// Two POST-scoped sessions created for the same session ID share the server
// counter, so concurrently issued server requests can never collide.
func TestStreamableHttpSession_RequestIDsUniqueAcrossInstances(t *testing.T) {
	counter := new(atomic.Int64)
	s1 := newStreamableHttpSession("shared", nil, nil, nil, nil, counter)
	s2 := newStreamableHttpSession("shared", nil, nil, nil, nil, counter)

	ids := make(chan int64, 2)
	for _, s := range []*streamableHttpSession{s1, s2} {
		go func(s *streamableHttpSession) {
			item := <-s.elicitationRequestChan
			ids <- item.requestID
			item.response <- samplingResponseItem{
				requestID: item.requestID,
				result:    json.RawMessage(`{"action":"decline"}`),
			}
		}(s)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	req := mcp.ElicitationRequest{
		Params: mcp.ElicitationParams{
			Message:         "collision",
			RequestedSchema: map[string]any{"type": "object"},
		},
	}
	if _, err := s1.RequestElicitation(ctx, req); err != nil {
		t.Fatalf("s1 elicitation failed: %v", err)
	}
	if _, err := s2.RequestElicitation(ctx, req); err != nil {
		t.Fatalf("s2 elicitation failed: %v", err)
	}

	a, b := <-ids, <-ids
	if a == b {
		t.Fatalf("request IDs collided across session instances: %d", a)
	}
}
