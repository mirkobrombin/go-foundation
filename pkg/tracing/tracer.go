package tracing

import (
	"context"
)

// Span represents a tracing span that can be ended and annotated.
type Span interface {
	End()
	SetAttributes(kv ...Attribute)
	RecordError(err error)
}

// Attribute is a key-value pair for span metadata.
type Attribute struct {
	Key   string
	Value any
}

// Tracer creates new spans.
type Tracer interface {
	StartSpan(ctx context.Context, name string) (context.Context, Span)
}

type noopSpan struct{}

func (noopSpan) End()                         {}
func (noopSpan) SetAttributes(kv ...Attribute) {}
func (noopSpan) RecordError(err error)        {}

type noopTracer struct{}

func (noopTracer) StartSpan(ctx context.Context, name string) (context.Context, Span) {
	return ctx, noopSpan{}
}

// Noop is a Tracer that creates noop spans.
var Noop Tracer = noopTracer{}

// StartSpan starts a span from the given tracer, falling back to Noop.
func StartSpan(ctx context.Context, tracer Tracer, name string) (context.Context, Span) {
	if tracer == nil {
		tracer = Noop
	}
	return tracer.StartSpan(ctx, name)
}
