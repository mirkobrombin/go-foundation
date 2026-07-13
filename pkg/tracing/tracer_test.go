package tracing

import (
	"context"
	"errors"
	"testing"
)

type testTracer struct {
	name string
}

func (t *testTracer) StartSpan(ctx context.Context, name string) (context.Context, Span) {
	t.name = name
	return context.WithValue(ctx, "span", name), &testSpan{}
}

type testSpan struct {
	ended bool
	err   error
}

func (s *testSpan) End()                          { s.ended = true }
func (s *testSpan) SetAttributes(kv ...Attribute) {}
func (s *testSpan) RecordError(err error)         { s.err = err }

func TestStartSpanUsesNoopForNilTracer(t *testing.T) {
	ctx, span := StartSpan(context.Background(), nil, "op")
	if ctx == nil || span == nil {
		t.Fatal("StartSpan() returned nil values")
	}
	span.SetAttributes(Attribute{Key: "k", Value: "v"})
	span.RecordError(errors.New("x"))
	span.End()
}

func TestStartSpanUsesTracer(t *testing.T) {
	tracer := &testTracer{}

	ctx, span := StartSpan(context.Background(), tracer, "op")

	if tracer.name != "op" {
		t.Fatalf("name = %q, want op", tracer.name)
	}
	if ctx.Value("span") != "op" {
		t.Fatalf("ctx value = %v, want op", ctx.Value("span"))
	}
	span.End()
}
