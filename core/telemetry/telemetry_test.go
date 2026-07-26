package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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
	histogram := meter.Histogram("latency")
	histogram.Record(ctx, 2)
	histogram.Record(ctx, 6)
	snapshot := meter.GetHistogram("latency")
	if snapshot.Count != 2 || snapshot.Sum != 8 || snapshot.Min != 2 || snapshot.Max != 6 {
		t.Fatalf("histogram = %+v", snapshot)
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

func TestOTLPExporter_ExportFlush(t *testing.T) {
	type request struct {
		path string
		body map[string]any
	}
	requests := make(chan request, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode OTLP request: %v", err)
		}
		requests <- request{path: r.URL.Path, body: body}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	e := NewOTLPExporter(WithOTLPEndpoint(server.URL), WithOTLPBatchSize(100))
	defer e.Close()
	start := time.Now().Add(-time.Second)
	if err := e.ExportSpan(OTLPSpan{
		TraceID:   "0af7651916cd43dd8448eb211c80319c",
		SpanID:    "b7ad6b7169203331",
		Name:      "test-span",
		StartTime: start,
		EndTime:   time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.ExportMetric(OTLPMetric{
		Name:  "http_requests",
		Kind:  OTLPMetricCounter,
		Value: 42,
	}); err != nil {
		t.Fatal(err)
	}

	err := e.Flush(context.Background())
	if err != nil {
		t.Errorf("Flush: %v", err)
	}
	seen := make(map[string]map[string]any)
	for range 2 {
		req := <-requests
		seen[req.path] = req.body
	}
	if seen["/v1/traces"]["resourceSpans"] == nil {
		t.Fatal("trace request did not contain resourceSpans")
	}
	if seen["/v1/metrics"]["resourceMetrics"] == nil {
		t.Fatal("metric request did not contain resourceMetrics")
	}
}

func TestOTLPExporterCloseIsIdempotentAndFlushes(t *testing.T) {
	requests := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exporter := NewOTLPExporter(WithOTLPEndpoint(server.URL))
	if err := exporter.ExportMetric(OTLPMetric{Name: "requests", Kind: OTLPMetricCounter, Value: 1}); err != nil {
		t.Fatal(err)
	}
	if err := exporter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := exporter.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-requests:
	case <-time.After(time.Second):
		t.Fatal("Close() did not flush pending telemetry")
	}
}

func TestOTLPExporterRestoresFailedBatch(t *testing.T) {
	exporter := NewOTLPExporter(WithOTLPEndpoint("://invalid"))
	start := time.Now().Add(-time.Second)
	if err := exporter.ExportSpan(OTLPSpan{
		TraceID:   "0af7651916cd43dd8448eb211c80319c",
		SpanID:    "b7ad6b7169203331",
		Name:      "span",
		StartTime: start,
		EndTime:   time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := exporter.Flush(context.Background()); err == nil {
		t.Fatal("Flush() succeeded with invalid endpoint")
	}
	exporter.batchMu.Lock()
	count := len(exporter.spans)
	exporter.batchMu.Unlock()
	if count != 1 {
		t.Fatalf("retained spans = %d, want 1", count)
	}
	_ = exporter.Close()
}

func TestOTLPExporterBoundsQueueAndRejectsUseAfterClose(t *testing.T) {
	exporter := NewOTLPExporter(
		WithOTLPBatchSize(10),
		WithOTLPQueueSize(1),
	)
	if err := exporter.ExportMetric(OTLPMetric{
		Name: "load",
		Kind: OTLPMetricGauge,
	}); err != nil {
		t.Fatal(err)
	}
	if err := exporter.ExportMetric(OTLPMetric{
		Name: "second",
		Kind: OTLPMetricGauge,
	}); !errors.Is(err, ErrOTLPQueueFull) {
		t.Fatalf("ExportMetric() error = %v, want ErrOTLPQueueFull", err)
	}
	exporter.endpoint = "://invalid"
	if err := exporter.Close(); err == nil {
		t.Fatal("Close() succeeded with an invalid endpoint")
	}
	if err := exporter.ExportMetric(OTLPMetric{
		Name: "closed",
		Kind: OTLPMetricGauge,
	}); !errors.Is(err, ErrOTLPExporterClosed) {
		t.Fatalf("ExportMetric() after Close() = %v", err)
	}
}

func TestOTLPExporterValidatesPublicInputs(t *testing.T) {
	exporter := NewOTLPExporter()
	defer exporter.Close()
	if err := exporter.ExportSpan(OTLPSpan{Name: "invalid"}); err == nil {
		t.Fatal("ExportSpan() accepted missing IDs and timestamps")
	}
	if err := exporter.ExportMetric(OTLPMetric{Name: "invalid"}); err == nil {
		t.Fatal("ExportMetric() accepted an invalid kind")
	}
}

func TestOTLPExporterDoesNotRetryPartialSuccessBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"partialSuccess":{"rejectedDataPoints":"1","errorMessage":"invalid metric"}}`))
	}))
	defer server.Close()
	exporter := NewOTLPExporter(WithOTLPEndpoint(server.URL))
	defer exporter.Close()
	if err := exporter.ExportMetric(OTLPMetric{
		Name: "invalid",
		Kind: OTLPMetricGauge,
	}); err != nil {
		t.Fatal(err)
	}

	if err := exporter.Flush(context.Background()); err == nil {
		t.Fatal("Flush() ignored an OTLP partial success")
	}
	exporter.batchMu.Lock()
	queued := len(exporter.metrics)
	exporter.batchMu.Unlock()
	if queued != 0 {
		t.Fatalf("partial-success batch was queued for retry: %d metrics", queued)
	}
}

func TestOTLPExporterCoalescesAutomaticFlushDuringOutage(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		w.WriteHeader(http.StatusServiceUnavailable)
	}))

	exporter := NewOTLPExporter(
		WithOTLPEndpoint(server.URL),
		WithOTLPBatchSize(1),
	)
	var wait sync.WaitGroup
	wait.Add(10)
	for index := 0; index < 10; index++ {
		go func(value float64) {
			defer wait.Done()
			_ = exporter.ExportMetric(OTLPMetric{
				Name:  "load",
				Kind:  OTLPMetricGauge,
				Value: value,
			})
		}(float64(index))
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("automatic flush did not start")
	}
	wait.Wait()
	close(release)
	deadline := time.Now().Add(time.Second)
	for {
		exporter.batchMu.Lock()
		inFlight := exporter.inFlight
		exporter.batchMu.Unlock()
		if inFlight == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("automatic outage flush requests = %d, want 1", got)
	}

	exporter.batchMu.Lock()
	exporter.spans = nil
	exporter.metrics = nil
	exporter.batchMu.Unlock()
	if err := exporter.Close(); err != nil {
		t.Fatal(err)
	}
	server.Close()
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

	invalid := []string{
		"00-00000000000000000000000000000000-b7ad6b7169203331-01",
		"00-0af7651916cd43dd8448eb211c80319c-0000000000000000-01",
		"00-0AF7651916CD43DD8448EB211C80319C-b7ad6b7169203331-01",
		"00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-zz",
	}
	for _, header := range invalid {
		if _, err := ParseTraceparent(header); err == nil {
			t.Fatalf("ParseTraceparent() accepted %q", header)
		}
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
	if got := w.Header().Get("traceparent"); got == "" {
		t.Fatal("traceparent response header was not written")
	}
}

func TestTelemetryURLRedactsSecrets(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodGet,
		"https://user:password@example.com/path?token=secret#fragment",
		nil,
	)
	got := telemetryURL(request)
	if got != "https://example.com/path" {
		t.Fatalf("telemetryURL() = %q, want safe URL", got)
	}
}

func TestTelemetryResponseWriterPreservesFirstStatusAndUnwraps(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := &telemetryResponseWriter{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}
	writer.WriteHeader(http.StatusCreated)
	writer.WriteHeader(http.StatusInternalServerError)
	if writer.statusCode != http.StatusCreated {
		t.Fatalf("recorded status = %d, want 201", writer.statusCode)
	}
	if recorder.Code != http.StatusCreated {
		t.Fatalf("response status = %d, want 201", recorder.Code)
	}
	if writer.Unwrap() != recorder {
		t.Fatal("Unwrap() did not return the underlying writer")
	}
	if err := http.NewResponseController(writer).Flush(); err != nil {
		t.Fatalf("ResponseController.Flush() error = %v", err)
	}
}
