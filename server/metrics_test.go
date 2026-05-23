package server

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/metrics"
)

// recordingMeter captures every Counter.Add and Histogram.Record so tests
// can assert attribute and value behaviour without an OTEL backend.
type recordingMeter struct {
	mu         sync.Mutex
	counters   map[string]*recordingCounter
	histograms map[string]*recordingHistogram
}

func newRecordingMeter() *recordingMeter {
	return &recordingMeter{
		counters:   map[string]*recordingCounter{},
		histograms: map[string]*recordingHistogram{},
	}
}

func (m *recordingMeter) Counter(name, _, _ string) metrics.Counter {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.counters[name]; ok {
		return c
	}
	c := &recordingCounter{name: name}
	m.counters[name] = c
	return c
}

func (m *recordingMeter) Histogram(name, _, _ string) metrics.Histogram {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.histograms[name]; ok {
		return h
	}
	h := &recordingHistogram{name: name}
	m.histograms[name] = h
	return h
}

type counterCall struct {
	n     int64
	attrs map[string]string
}

type recordingCounter struct {
	mu    sync.Mutex
	name  string
	calls []counterCall
}

func (c *recordingCounter) Add(_ context.Context, n int64, attrs ...metrics.Attribute) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, counterCall{n: n, attrs: flatten(attrs)})
}

type histogramObservation struct {
	value float64
	attrs map[string]string
}

type recordingHistogram struct {
	mu           sync.Mutex
	name         string
	observations []histogramObservation
}

func (h *recordingHistogram) Record(_ context.Context, v float64, attrs ...metrics.Attribute) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.observations = append(h.observations, histogramObservation{value: v, attrs: flatten(attrs)})
}

func flatten(attrs []metrics.Attribute) map[string]string {
	out := map[string]string{}
	for _, a := range attrs {
		out[a.Key] = a.Value
	}
	return out
}

func TestWithMeter_NilFallsBackToNoop(t *testing.T) {
	s := NewMCPServer("meter-srv", "1.0", WithMeter(nil))
	if s.meter == nil {
		t.Fatalf("WithMeter(nil) must install a non-nil noop meter")
	}
	// Counters/Histograms must be wired so the request hook does not panic.
	end := s.startMessageMetric(t.Context(), "tools/list", "")
	end(nil)
}

func TestWithMeter_RecordsRequestOnDispatch(t *testing.T) {
	m := newRecordingMeter()
	s := NewMCPServer("meter-srv", "1.0", WithMeter(m))

	end := s.startMessageMetric(t.Context(), "tools/list", "2025-11-25")
	end(nil)

	c := m.counters[metricRequestCalls]
	if c == nil || len(c.calls) != 1 {
		t.Fatalf("expected 1 mcp.request.calls observation; got %+v", c)
	}
	got := c.calls[0]
	if got.n != 1 {
		t.Fatalf("counter delta must be 1; got %d", got.n)
	}
	if got.attrs[attrKeyMethod] != "tools/list" {
		t.Fatalf("missing method attr: %+v", got.attrs)
	}
	if got.attrs[attrKeyProtocolVersion] != "2025-11-25" {
		t.Fatalf("missing protocol attr: %+v", got.attrs)
	}
	if got.attrs[attrKeyOutcome] != metricOutcomeOK {
		t.Fatalf("want outcome=ok, got %s", got.attrs[attrKeyOutcome])
	}

	h := m.histograms[metricRequestDuration]
	if h == nil || len(h.observations) != 1 {
		t.Fatalf("expected 1 mcp.request.duration observation; got %+v", h)
	}
	if h.observations[0].value < 0 {
		t.Fatalf("duration must be non-negative; got %v", h.observations[0].value)
	}
}

func TestWithMeter_RecordsRequestErrorOutcome(t *testing.T) {
	m := newRecordingMeter()
	s := NewMCPServer("meter-srv", "1.0", WithMeter(m))

	end := s.startMessageMetric(t.Context(), "tools/call", "")
	end(mcp.NewJSONRPCError(mcp.NewRequestId(1), mcp.INVALID_PARAMS, "bad", nil))

	c := m.counters[metricRequestCalls]
	if c == nil || len(c.calls) != 1 {
		t.Fatalf("expected 1 counter observation")
	}
	if got := c.calls[0].attrs[attrKeyOutcome]; got != metricOutcomeError {
		t.Fatalf("want outcome=error, got %s", got)
	}
}

func TestWithMeter_ToolMiddlewareCovers3Outcomes(t *testing.T) {
	m := newRecordingMeter()
	s := NewMCPServer("meter-srv", "1.0", WithMeter(m))

	// Pull out the registered tool middleware and exercise it.
	s.toolMiddlewareMu.RLock()
	mws := append([]ToolHandlerMiddleware(nil), s.toolHandlerMiddlewares...)
	s.toolMiddlewareMu.RUnlock()
	if len(mws) == 0 {
		t.Fatalf("expected at least one tool middleware after WithMeter")
	}

	cases := []struct {
		name        string
		handler     ToolHandlerFunc
		wantOutcome string
	}{
		{"ok",
			func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{}, nil
			},
			metricOutcomeOK,
		},
		{"error",
			func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return nil, errors.New("boom")
			},
			metricOutcomeError,
		},
		{"error_result",
			func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{IsError: true}, nil
			},
			metricOutcomeErrorResult,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := tc.handler
			for i := len(mws) - 1; i >= 0; i-- {
				wrapped = mws[i](wrapped)
			}
			_, _ = wrapped(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "list_pods"}})
		})
	}

	c := m.counters[metricToolCalls]
	if c == nil || len(c.calls) != 3 {
		t.Fatalf("expected 3 tool.call observations; got %+v", c)
	}
	for i, want := range []string{metricOutcomeOK, metricOutcomeError, metricOutcomeErrorResult} {
		if got := c.calls[i].attrs[attrKeyOutcome]; got != want {
			t.Fatalf("call[%d] want outcome=%s, got %s", i, want, got)
		}
		if got := c.calls[i].attrs[attrKeyToolName]; got != "list_pods" {
			t.Fatalf("call[%d] want tool=list_pods, got %s", i, got)
		}
	}
}
