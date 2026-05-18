package otel

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"

	"github.com/mark3labs/mcp-go/metrics"
)

// NewMeter wraps an OpenTelemetry metric.Meter as a metrics.Meter. A nil
// meter is treated as a no-op.
func NewMeter(m otelmetric.Meter) metrics.Meter {
	if m == nil {
		return metrics.NoopMeter()
	}
	return otelMeter{meter: m}
}

type otelMeter struct {
	meter otelmetric.Meter
}

func (o otelMeter) Counter(name, description, unit string) metrics.Counter {
	opts := []otelmetric.Int64CounterOption{}
	if description != "" {
		opts = append(opts, otelmetric.WithDescription(description))
	}
	if unit != "" {
		opts = append(opts, otelmetric.WithUnit(unit))
	}
	c, err := o.meter.Int64Counter(name, opts...)
	if err != nil {
		return metrics.NoopMeter().Counter(name, description, unit)
	}
	return otelCounter{c: c}
}

func (o otelMeter) Histogram(name, description, unit string) metrics.Histogram {
	opts := []otelmetric.Float64HistogramOption{}
	if description != "" {
		opts = append(opts, otelmetric.WithDescription(description))
	}
	if unit != "" {
		opts = append(opts, otelmetric.WithUnit(unit))
	}
	h, err := o.meter.Float64Histogram(name, opts...)
	if err != nil {
		return metrics.NoopMeter().Histogram(name, description, unit)
	}
	return otelHistogram{h: h}
}

type otelCounter struct{ c otelmetric.Int64Counter }

func (c otelCounter) Add(ctx context.Context, n int64, attrs ...metrics.Attribute) {
	c.c.Add(ctx, n, otelmetric.WithAttributes(toOTelMetricAttrs(attrs)...))
}

type otelHistogram struct{ h otelmetric.Float64Histogram }

func (h otelHistogram) Record(ctx context.Context, v float64, attrs ...metrics.Attribute) {
	h.h.Record(ctx, v, otelmetric.WithAttributes(toOTelMetricAttrs(attrs)...))
}

func toOTelMetricAttrs(attrs []metrics.Attribute) []attribute.KeyValue {
	out := make([]attribute.KeyValue, len(attrs))
	for i, a := range attrs {
		out[i] = attribute.String(a.Key, a.Value)
	}
	return out
}
