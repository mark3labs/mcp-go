package metrics

import (
	"testing"
)

func TestNoopMeter_IsNonNilAndDoesNotPanic(t *testing.T) {
	m := NoopMeter()
	if m == nil {
		t.Fatalf("NoopMeter() returned nil")
	}
	c := m.Counter("test", "", "")
	h := m.Histogram("test", "", "")
	c.Add(t.Context(), 1, String("k", "v"))
	h.Record(t.Context(), 1.5, String("k", "v"))
}

func TestString_BuildsAttribute(t *testing.T) {
	a := String("mcp.method", "tools/list")
	if a.Key != "mcp.method" || a.Value != "tools/list" {
		t.Fatalf("unexpected attr: %+v", a)
	}
}
