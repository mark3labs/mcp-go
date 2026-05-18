// Package metrics defines the metric interfaces used by the mcp-go server.
// Concrete implementations live in adapter modules; an OpenTelemetry
// adapter ships at github.com/mark3labs/mcp-go/otel.
package metrics

import "context"

// Attribute is a string key/value pair attached to a metric observation.
type Attribute struct {
	Key, Value string
}

// String returns an Attribute with the given key and value.
func String(key, value string) Attribute {
	return Attribute{Key: key, Value: value}
}

// Meter creates instruments. Implementations are expected to deduplicate
// instruments by name, so calling Counter("x") twice returns equivalent
// recorders.
type Meter interface {
	Counter(name, description, unit string) Counter
	Histogram(name, description, unit string) Histogram
}

// Counter is a cumulative integer counter (e.g. number of requests).
type Counter interface {
	Add(ctx context.Context, n int64, attrs ...Attribute)
}

// Histogram records a distribution of values (e.g. request latencies).
type Histogram interface {
	Record(ctx context.Context, value float64, attrs ...Attribute)
}

// NoopMeter returns a Meter whose instruments record nothing.
func NoopMeter() Meter { return noopMeter{} }

type noopMeter struct{}

func (noopMeter) Counter(string, string, string) Counter     { return noopCounter{} }
func (noopMeter) Histogram(string, string, string) Histogram { return noopHistogram{} }

type noopCounter struct{}

func (noopCounter) Add(context.Context, int64, ...Attribute) {}

type noopHistogram struct{}

func (noopHistogram) Record(context.Context, float64, ...Attribute) {}
