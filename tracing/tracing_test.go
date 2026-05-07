package tracing

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNoopTracerStart(t *testing.T) {
	ctx, span := NoopTracer().Start(t.Context(), "anything", SpanKindServer, String("k", "v"))
	assert.NotNil(t, ctx)
	span.SetAttributes(String("k2", "v2"))
	span.RecordError(errors.New("ignored"))
	span.SetStatus(StatusError, "ignored")
	span.End()
}

func TestSpanFromContextWithoutSpanReturnsNoop(t *testing.T) {
	span := SpanFromContext(t.Context())
	assert.NotNil(t, span)
}

func TestContextWithSpanRoundTrip(t *testing.T) {
	want := &recordingSpan{}
	ctx := ContextWithSpan(t.Context(), want)
	got := SpanFromContext(ctx)
	assert.Same(t, want, got)
}

func TestNoopPropagatorIsIdentity(t *testing.T) {
	p := NoopPropagator()
	headers := http.Header{}
	p.Inject(t.Context(), headers)
	assert.Empty(t, headers)
	ctx := p.Extract(t.Context(), headers)
	assert.NotNil(t, ctx)
}

type recordingSpan struct {
	attrs   []Attribute
	err     error
	status  StatusCode
	statusD string
	ended   bool
}

func (r *recordingSpan) SetAttributes(attrs ...Attribute)       { r.attrs = append(r.attrs, attrs...) }
func (r *recordingSpan) RecordError(err error)                  { r.err = err }
func (r *recordingSpan) SetStatus(code StatusCode, desc string) { r.status = code; r.statusD = desc }
func (r *recordingSpan) End()                                   { r.ended = true }
