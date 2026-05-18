package otel_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/mark3labs/mcp-go/metrics"
	otelmcp "github.com/mark3labs/mcp-go/otel"
)

func newTestMeter(t *testing.T) (otelmetric.Meter, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(t.Context()) })
	return mp.Meter("test"), reader
}

func collect(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	return rm
}

func findCounter(rm metricdata.ResourceMetrics, name string) *metricdata.Sum[int64] {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				if d, ok := m.Data.(metricdata.Sum[int64]); ok {
					return &d
				}
			}
		}
	}
	return nil
}

func findHistogram(rm metricdata.ResourceMetrics, name string) *metricdata.Histogram[float64] {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				if d, ok := m.Data.(metricdata.Histogram[float64]); ok {
					return &d
				}
			}
		}
	}
	return nil
}

func TestNewMeter_NilFallsBackToNoop(t *testing.T) {
	m := otelmcp.NewMeter(nil)
	require.NotNil(t, m, "NewMeter(nil) must return a non-nil noop meter")
	m.Counter("x", "", "").Add(t.Context(), 1)
	m.Histogram("y", "", "").Record(t.Context(), 1.0)
}

func TestNewMeter_RecordsCounterAndHistogram(t *testing.T) {
	otelMeter, reader := newTestMeter(t)
	m := otelmcp.NewMeter(otelMeter)

	c := m.Counter("svc.calls", "calls", "{call}")
	h := m.Histogram("svc.duration", "duration", "s")

	ctx := t.Context()
	c.Add(ctx, 1, metrics.String("method", "tools/list"))
	c.Add(ctx, 1, metrics.String("method", "tools/list"))
	h.Record(ctx, 0.125, metrics.String("method", "tools/list"))

	rm := collect(t, reader)
	calls := findCounter(rm, "svc.calls")
	require.NotNil(t, calls, "missing svc.calls counter")
	require.Len(t, calls.DataPoints, 1, "expected one data point keyed by method=tools/list")
	require.Equal(t, int64(2), calls.DataPoints[0].Value)
	gotMethod, _ := calls.DataPoints[0].Attributes.Value(attribute.Key("method"))
	require.Equal(t, "tools/list", gotMethod.AsString())

	hist := findHistogram(rm, "svc.duration")
	require.NotNil(t, hist, "missing svc.duration histogram")
	require.Len(t, hist.DataPoints, 1)
	require.Equal(t, uint64(1), hist.DataPoints[0].Count)
}
