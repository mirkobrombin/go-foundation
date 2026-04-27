package tracing

import (
	"context"
)

type Span interface {
	End()
	SetAttributes(kv ...Attribute)
	RecordError(err error)
}

type Attribute struct {
	Key   string
	Value any
}

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

var Noop Tracer = noopTracer{}

func StartSpan(ctx context.Context, tracer Tracer, name string) (context.Context, Span) {
	if tracer == nil {
		tracer = Noop
	}
	return tracer.StartSpan(ctx, name)
}
