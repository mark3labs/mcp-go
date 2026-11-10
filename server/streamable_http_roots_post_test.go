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

// scopedRequestClient bounds each exchange, so a server request that never
// reaches the POST stream fails the test instead of hanging it.
var scopedRequestClient = &http.Client{Timeout: 10 * time.Second}

// postToolCall sends a tools/call for name on sessionID and returns the open
// SSE response, failing if the server did not upgrade it to a stream.
func postToolCall(t *testing.T, url, sessionID, name string) (*http.Response, *bufio.Reader) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 7, "method": "tools/call",
		"params": map[string]any{"name": name},
	})
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set(HeaderKeySessionID, sessionID)
	resp, err := scopedRequestClient.Do(req)
	if err != nil {
		t.Fatalf("tools/call request failed: %v", err)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		resp.Body.Close()
		t.Fatalf("expected SSE response, got Content-Type %q", ct)
	}
	return resp, bufio.NewReader(resp.Body)
}

// answerServerRequest posts result as the response to the server request id.
func answerServerRequest(t *testing.T, url, sessionID string, id json.RawMessage, result any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set(HeaderKeySessionID, sessionID)
	resp, err := scopedRequestClient.Do(req)
	if err != nil {
		t.Fatalf("server request response failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 for the response, got %d", resp.StatusCode)
	}
}

// readToolResultText reads the final tools/call response for request 7.
func readToolResultText(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	var final struct {
		ID     int64 `json:"id"`
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	readSSEData(t, reader, &final)
	if final.ID != 7 || len(final.Result.Content) == 0 {
		t.Fatalf("unexpected final response: %+v", final)
	}
	return final.Result.Content[0].Text
}

// A roots/list issued while a POST request is being handled must be delivered
// on that POST's SSE stream, before the final response, as elicitation is
// (#932). A client that holds no standalone GET stream otherwise never
// receives it, and the tool call waits out its timeout.
func TestStreamableHTTPServer_ListRootsOnOriginatingPOSTStream(t *testing.T) {
	mcpServer := NewMCPServer("test-server", "1.0.0", WithRoots())
	mcpServer.AddTool(
		mcp.NewTool("workspace"),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			result, err := mcpServer.RequestRoots(ctx, mcp.ListRootsRequest{})
			if err != nil {
				return nil, err
			}
			var uris []string
			for _, r := range result.Roots {
				uris = append(uris, r.URI)
			}
			return mcp.NewToolResultText(strings.Join(uris, ",")), nil
		},
	)
	testServer := httptest.NewServer(NewStreamableHTTPServer(mcpServer))
	defer testServer.Close()
	sessionID := initializeSession(t, testServer.URL)

	resp, reader := postToolCall(t, testServer.URL, sessionID, "workspace")
	defer resp.Body.Close()

	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	readSSEData(t, reader, &request)
	if request.Method != string(mcp.MethodListRoots) {
		t.Fatalf("expected first SSE event to be %s, got %q", mcp.MethodListRoots, request.Method)
	}
	answerServerRequest(t, testServer.URL, sessionID, request.ID,
		map[string]any{"roots": []map[string]any{{"uri": "file:///w", "name": "w"}}})

	if got := readToolResultText(t, reader); got != "file:///w" {
		t.Fatalf("unexpected tool result %q", got)
	}
}

// A sampling request issued while a POST request is being handled goes on
// that POST's SSE stream too.
func TestStreamableHTTPServer_SamplingOnOriginatingPOSTStream(t *testing.T) {
	mcpServer := NewMCPServer("test-server", "1.0.0")
	mcpServer.EnableSampling()
	mcpServer.AddTool(
		mcp.NewTool("summarize"),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			result, err := mcpServer.RequestSampling(ctx, mcp.CreateMessageRequest{
				CreateMessageParams: mcp.CreateMessageParams{
					Messages:  []mcp.SamplingMessage{{Role: mcp.RoleUser, Content: mcp.NewTextContent("hi")}},
					MaxTokens: 10,
				},
			})
			if err != nil {
				return nil, err
			}
			return mcp.NewToolResultText("model=" + result.Model), nil
		},
	)
	testServer := httptest.NewServer(NewStreamableHTTPServer(mcpServer))
	defer testServer.Close()
	sessionID := initializeSession(t, testServer.URL)

	resp, reader := postToolCall(t, testServer.URL, sessionID, "summarize")
	defer resp.Body.Close()

	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	readSSEData(t, reader, &request)
	if request.Method != string(mcp.MethodSamplingCreateMessage) {
		t.Fatalf("expected first SSE event to be %s, got %q", mcp.MethodSamplingCreateMessage, request.Method)
	}
	answerServerRequest(t, testServer.URL, sessionID, request.ID, map[string]any{
		"role": "assistant", "model": "m1",
		"content": map[string]any{"type": "text", "text": "ok"},
	})

	if got := readToolResultText(t, reader); got != "model=m1" {
		t.Fatalf("unexpected tool result %q", got)
	}
}

// Without a request-scoped stream in the context, roots/list and sampling
// still go to the standalone GET stream path.
func TestStreamableHttpSession_ListRootsAndSamplingFallBackToSessionChannel(t *testing.T) {
	session := newStreamableHttpSession("s1", nil, nil, nil, nil, new(atomic.Int64))
	go func() {
		item := <-session.rootsRequestChan
		item.response <- samplingResponseItem{
			requestID: item.requestID,
			result:    json.RawMessage(`{"roots":[{"uri":"file:///fallback","name":"f"}]}`),
		}
	}()
	go func() {
		item := <-session.samplingRequestChan
		item.response <- samplingResponseItem{
			requestID: item.requestID,
			result:    json.RawMessage(`{"role":"assistant","model":"fallback","content":{"type":"text","text":"ok"}}`),
		}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	roots, err := session.ListRoots(ctx, mcp.ListRootsRequest{})
	if err != nil {
		t.Fatalf("ListRoots failed: %v", err)
	}
	if len(roots.Roots) != 1 || roots.Roots[0].URI != "file:///fallback" {
		t.Fatalf("unexpected roots: %+v", roots.Roots)
	}
	sample, err := session.RequestSampling(ctx, mcp.CreateMessageRequest{
		CreateMessageParams: mcp.CreateMessageParams{
			Messages:  []mcp.SamplingMessage{{Role: mcp.RoleUser, Content: mcp.NewTextContent("hi")}},
			MaxTokens: 10,
		},
	})
	if err != nil {
		t.Fatalf("RequestSampling failed: %v", err)
	}
	if sample.Model != "fallback" {
		t.Fatalf("expected model fallback, got %q", sample.Model)
	}
}
