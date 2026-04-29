package telemetry

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

// --- M6: OTLP, Prometheus, Traceparent, Middleware tests ---

func TestOTLPExporter_ExportFlush(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	e := NewOTLPExporter(WithOTLPBatchSize(100))
	defer e.Close()
	e.endpoint = server.URL + "/v1/traces"

	e.ExportSpan(otlpSpan{
		TraceID: "0af7651916cd43dd8448eb211c80319c",
		SpanID:  "b7ad6b7169203331",
		Name:    "test-span",
	})

	time.Sleep(100 * time.Millisecond)

	e.ExportMetric(otlpMetric{
		Name:  "http_requests",
		Kind:  "COUNTER",
		Value: 42,
	})

	err := e.Flush(context.Background())
	if err != nil {
		t.Errorf("Flush: %v", err)
	}
}

func TestPrometheusExporter_CounterGauge(t *testing.T) {
	p := NewPrometheusExporter()
	p.IncCounter("http_requests_total", 5)
	p.IncCounter("http_requests_total", 3)
	p.SetGauge("cpu_usage", 75.5)

	var buf bytes.Buffer
	p.WriteText(&buf)

	output := buf.String()
	if !strings.Contains(output, "http_requests_total 8") {
		t.Errorf("expected counter in output, got: %s", output)
	}
	if !strings.Contains(output, "cpu_usage 75.5") {
		t.Errorf("expected gauge in output, got: %s", output)
	}
}

func TestPrometheusExporter_Histogram(t *testing.T) {
	p := NewPrometheusExporter()
	buckets := []float64{0.1, 0.5, 1.0}
	p.ObserveHistogram("request_duration", 0.05, buckets)
	p.ObserveHistogram("request_duration", 0.25, buckets)
	p.ObserveHistogram("request_duration", 0.75, buckets)
	p.ObserveHistogram("request_duration", 1.5, buckets)

	var buf bytes.Buffer
	p.WriteText(&buf)

	output := buf.String()
	if !strings.Contains(output, "request_duration_bucket") {
		t.Errorf("expected histogram buckets in output, got: %s", output)
	}
	if !strings.Contains(output, "request_duration_sum") {
		t.Errorf("expected histogram sum in output, got: %s", output)
	}
}

func TestTraceparent(t *testing.T) {
	tc, err := ParseTraceparent("00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	if err != nil {
		t.Fatalf("ParseTraceparent: %v", err)
	}
	if tc.TraceID != "0af7651916cd43dd8448eb211c80319c" {
		t.Errorf("TraceID = %q, want full trace ID", tc.TraceID)
	}
	if tc.ParentID != "b7ad6b7169203331" {
		t.Errorf("ParentID = %q", tc.ParentID)
	}
	if tc.TraceFlags != "01" {
		t.Errorf("TraceFlags = %q", tc.TraceFlags)
	}

	encoded := tc.Encode()
	if encoded != "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01" {
		t.Errorf("Encode = %q", encoded)
	}
}

func TestTraceparent_Invalid(t *testing.T) {
	_, err := ParseTraceparent("invalid")
	if err == nil {
		t.Error("expected error for invalid traceparent")
	}

	_, err = ParseTraceparent("01-abc-def-01")
	if err == nil {
		t.Error("expected error for unsupported version")
	}
}

func TestTelemetryMiddleware_WrapHTTP(t *testing.T) {
	meter := NewSimpleMeter()
	provider := NewProvider(WithMeter(meter))

	mw := NewTelemetryMiddleware(provider)

	handler := mw.WrapHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}

	if v := meter.GetCounter("http_requests_total"); v == 0 {
		t.Error("expected counter to be incremented")
	}
}