package telemetry

import (
	"context"
	"testing"
)

func TestProvider_NoopDefault(t *testing.T) {
	p := NewProvider()
	if p.Tracer == nil {
		t.Error("Tracer should not be nil (noop)")
	}
	if p.Meter == nil {
		t.Error("Meter should not be nil (noop)")
	}

	span, ctx := p.Tracer.Start(context.Background(), "test")
	span.SetAttributes(Attribute{Key: "k", Value: "v"})
	span.End()

	p.Meter.Counter("c").Add(ctx, 1)
	p.Meter.Histogram("h").Record(ctx, 3.14)
	p.Meter.Gauge("g").Set(ctx, 42)
}

func TestSimpleTracer(t *testing.T) {
	tracer := NewSimpleTracer()
	span, _ := tracer.Start(context.Background(), "operation")
	span.End()
}

func TestSimpleMeter(t *testing.T) {
	meter := NewSimpleMeter()
	ctx := context.Background()

	counter := meter.Counter("requests")
	counter.Add(ctx, 5)
	counter.Add(ctx, 3)

	if v := meter.GetCounter("requests"); v != 8 {
		t.Errorf("counter: got %d, want 8", v)
	}

	gauge := meter.Gauge("cpu")
	gauge.Set(ctx, 75.5)
	if v := meter.GetGauge("cpu"); v != 75.5 {
		t.Errorf("gauge: got %f, want 75.5", v)
	}
}

func TestTimed(t *testing.T) {
	meter := NewSimpleMeter()
	h := meter.Histogram("duration")
	ctx := context.Background()

	called := false
	dur := Timed(ctx, h, func() {
		called = true
	})

	if !called {
		t.Error("Timed should call fn")
	}
	if dur <= 0 {
		t.Error("Timed should return positive duration")
	}
}

func TestProvider_WithTracer(t *testing.T) {
	custom := NewSimpleTracer()
	p := NewProvider(WithTracer(custom))
	if p.Tracer != custom {
		t.Error("WithTracer should set custom tracer")
	}
}

func TestProvider_Shutdown(t *testing.T) {
	called := false
	p := NewProvider()
	p.shutdown = append(p.shutdown, func() { called = true })
	p.Shutdown()
	if !called {
		t.Error("Shutdown should call cleanup functions")
	}
}