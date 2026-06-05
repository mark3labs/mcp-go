package otel_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"

	otelmcp "github.com/mark3labs/mcp-go/otel"
)

// captureExporter is a minimal sdklog.Exporter that holds onto every
// record it sees so tests can assert without standing up a transport.
type captureExporter struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (e *captureExporter) Export(_ context.Context, records []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range records {
		e.records = append(e.records, r.Clone())
	}
	return nil
}

func (e *captureExporter) Shutdown(context.Context) error   { return nil }
func (e *captureExporter) ForceFlush(context.Context) error { return nil }

func (e *captureExporter) snapshot() []sdklog.Record {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]sdklog.Record, len(e.records))
	copy(out, e.records)
	return out
}

func newTestLoggerProvider(t *testing.T) (log.LoggerProvider, *captureExporter) {
	t.Helper()
	exp := &captureExporter{}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exp)))
	// context.Background() is intentional: t.Context() is canceled before t.Cleanup runs.
	t.Cleanup(func() { _ = lp.Shutdown(context.Background()) }) //nolint:usetesting
	return lp, exp
}

func TestNewSlogLogger_NilProviderUsesGlobal(t *testing.T) {
	lp, exp := newTestLoggerProvider(t)

	prev := global.GetLoggerProvider()
	global.SetLoggerProvider(lp)
	t.Cleanup(func() { global.SetLoggerProvider(prev) })

	logger := otelmcp.NewSlogLogger(nil, "mcp")
	require.NotNil(t, logger)

	logger.LogAttrs(t.Context(), slog.LevelInfo, "mcp.global.probe")

	require.Len(t, exp.snapshot(), 1, "expected global provider to receive the record")
}

func TestNewSlogLogger_RoutesRecordsToProvider(t *testing.T) {
	lp, exp := newTestLoggerProvider(t)
	logger := otelmcp.NewSlogLogger(lp, "mcp")

	logger.LogAttrs(t.Context(), slog.LevelInfo, "mcp.request",
		slog.String("mcp.method", "tools/list"),
		slog.Float64("duration_s", 0.001),
	)

	records := exp.snapshot()
	require.Len(t, records, 1, "expected one record")
	require.Equal(t, "mcp.request", records[0].Body().AsString())

	var gotMethod string
	var gotDuration float64
	records[0].WalkAttributes(func(kv log.KeyValue) bool {
		switch kv.Key {
		case "mcp.method":
			gotMethod = kv.Value.AsString()
		case "duration_s":
			gotDuration = kv.Value.AsFloat64()
		}
		return true
	})
	require.Equal(t, "tools/list", gotMethod)
	require.InDelta(t, 0.001, gotDuration, 0.0)
}
