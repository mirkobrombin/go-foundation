package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// --- OTLP JSON Exporter ---

// OTLPExporter sends spans and metrics to an OTLP-compatible endpoint over HTTP/JSON.
type OTLPExporter struct {
	endpoint string
	client   *http.Client
	batchMu  sync.Mutex
	spans    []otlpSpan
	metrics  []otlpMetric
	maxBatch int
	flushMs  time.Duration
	stopCh   chan struct{}
}

type otlpSpan struct {
	TraceID   string                 `json:"traceId"`
	SpanID    string                 `json:"spanId"`
	Name      string                 `json:"name"`
	StartTime string                `json:"startTimeUnixNano"`
	EndTime   string                 `json:"endTimeUnixNano"`
	Attrs     map[string]any         `json:"attributes,omitempty"`
}

type otlpMetric struct {
	Name  string  `json:"name"`
	Kind  string  `json:"kind"` // COUNTER, GAUGE, HISTOGRAM
	Value float64 `json:"value"`
}

// OTLPOption configures an OTLPExporter.
type OTLPOption func(*OTLPExporter)

// WithOTLPEndpoint sets the OTLP receiver endpoint URL.
func WithOTLPEndpoint(url string) OTLPOption {
	return func(e *OTLPExporter) { e.endpoint = url }
}

// WithOTLPBatchSize sets the maximum batch size before flushing.
func WithOTLPBatchSize(n int) OTLPOption {
	return func(e *OTLPExporter) { e.maxBatch = n }
}

// NewOTLPExporter creates a new OTLPExporter with the given options.
func NewOTLPExporter(opts ...OTLPOption) *OTLPExporter {
	e := &OTLPExporter{
		endpoint: "http://localhost:4318/v1/traces",
		client:   &http.Client{Timeout: 5 * time.Second},
		maxBatch: 100,
		flushMs:  5000 * time.Millisecond,
		stopCh:   make(chan struct{}),
	}
	for _, opt := range opts {
		opt(e)
	}
	go e.loop()
	return e
}

func (e *OTLPExporter) ExportSpan(span otlpSpan) {
	e.batchMu.Lock()
	e.spans = append(e.spans, span)
	shouldFlush := len(e.spans) >= e.maxBatch
	e.batchMu.Unlock()
	if shouldFlush {
		e.Flush(context.Background())
	}
}

func (e *OTLPExporter) ExportMetric(m otlpMetric) {
	e.batchMu.Lock()
	e.metrics = append(e.metrics, m)
	shouldFlush := len(e.metrics) >= e.maxBatch
	e.batchMu.Unlock()
	if shouldFlush {
		e.Flush(context.Background())
	}
}

func (e *OTLPExporter) Flush(ctx context.Context) error {
	e.batchMu.Lock()
	spans := e.spans
	metrics := e.metrics
	e.spans = nil
	e.metrics = nil
	e.batchMu.Unlock()

	if len(spans) == 0 && len(metrics) == 0 {
		return nil
	}

	payload := map[string]any{
		"resourceSpans": []map[string]any{
			{
				"scopeSpans": []map[string]any{
					{"spans": spans},
				},
			},
		},
		"resourceMetrics": []map[string]any{
			{
				"scopeMetrics": []map[string]any{
					{"metrics": metrics},
				},
			},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("otlp marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("otlp request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("otlp send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("otlp status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (e *OTLPExporter) loop() {
	ticker := time.NewTicker(e.flushMs)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			e.Flush(context.Background())
		case <-e.stopCh:
			e.Flush(context.Background())
			return
		}
	}
}

func (e *OTLPExporter) Close() {
	close(e.stopCh)
}

// --- Prometheus Text Format Exporter ---

type PrometheusExporter struct {
	mu      sync.RWMutex
	counters map[string]int64
	gauges  map[string]float64
	histos  map[string]*promHistogram
}

type promHistogram struct {
	buckets map[float64]int64
	sum     float64
	count   int64
}

// NewPrometheusExporter creates a new PrometheusExporter.
func NewPrometheusExporter() *PrometheusExporter {
	return &PrometheusExporter{
		counters: make(map[string]int64),
		gauges:  make(map[string]float64),
		histos:  make(map[string]*promHistogram),
	}
}

func (p *PrometheusExporter) IncCounter(name string, delta int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.counters[name] += delta
}

func (p *PrometheusExporter) SetGauge(name string, value float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gauges[name] = value
}

func (p *PrometheusExporter) ObserveHistogram(name string, value float64, buckets []float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	h, ok := p.histos[name]
	if !ok {
		h = &promHistogram{buckets: make(map[float64]int64), sum: 0, count: 0}
		for _, b := range buckets {
			h.buckets[b] = 0
		}
		p.histos[name] = h
	}
	h.count++
	h.sum += value
	for _, b := range buckets {
		if value <= b {
			h.buckets[b]++
		}
	}
}

func (p *PrometheusExporter) WriteText(w io.Writer) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for name, val := range p.counters {
		fmt.Fprintf(w, "# TYPE %s counter\n%s %d\n", name, name, val)
	}
	for name, val := range p.gauges {
		fmt.Fprintf(w, "# TYPE %s gauge\n%s %g\n", name, name, val)
	}
	for name, h := range p.histos {
		fmt.Fprintf(w, "# TYPE %s histogram\n", name)
		for _, b := range sortedKeys(h.buckets) {
			fmt.Fprintf(w, "%s_bucket{le=\"%g\"} %d\n", name, b, h.buckets[b])
		}
		fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", name, h.count)
		fmt.Fprintf(w, "%s_sum %g\n", name, h.sum)
		fmt.Fprintf(w, "%s_count %d\n", name, h.count)
	}
}

func sortedKeys(m map[float64]int64) []float64 {
	keys := make([]float64, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortFloat64s(keys)
	return keys
}

func sortFloat64s(a []float64) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// --- W3C Trace Context Propagation ---

type TraceContext struct {
	TraceID    string
	ParentID   string
	TraceFlags string
}

// ParseTraceparent parses a W3C traceparent header.
func ParseTraceparent(header string) (*TraceContext, error) {
	parts := strings.Split(header, "-")
	if len(parts) != 4 {
		return nil, fmt.Errorf("invalid traceparent: expected 4 parts, got %d", len(parts))
	}
	if parts[0] != "00" {
		return nil, fmt.Errorf("unsupported traceparent version: %s", parts[0])
	}
	tc := &TraceContext{
		TraceID:    parts[1],
		ParentID:   parts[2],
		TraceFlags: parts[3],
	}
	if len(tc.TraceID) != 32 {
		return nil, fmt.Errorf("invalid trace ID length: %d", len(tc.TraceID))
	}
	if len(tc.ParentID) != 16 {
		return nil, fmt.Errorf("invalid parent ID length: %d", len(tc.ParentID))
	}
	return tc, nil
}

func (tc *TraceContext) Encode() string {
	return fmt.Sprintf("00-%s-%s-%s", tc.TraceID, tc.ParentID, tc.TraceFlags)
}

// --- Telemetry Middleware for srv ---

type TelemetryMiddleware struct {
	Provider *Provider
}

// NewTelemetryMiddleware creates a new TelemetryMiddleware wrapping the given provider.
func NewTelemetryMiddleware(provider *Provider) *TelemetryMiddleware {
	return &TelemetryMiddleware{Provider: provider}
}

// SrvMiddleware returns a middleware function compatible with the srv package.
// It accepts a HandlerFunc type to avoid circular import - use via adapter.
func (tm *TelemetryMiddleware) WrapHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		traceCtx, _ := ParseTraceparent(r.Header.Get("traceparent"))

		var span Span
		ctx := r.Context()
		if tm.Provider != nil && tm.Provider.Tracer != nil {
			attrs := []Attribute{
				{Key: "http.method", Value: r.Method},
				{Key: "http.url", Value: r.URL.String()},
			}
			if traceCtx != nil {
				attrs = append(attrs,
					Attribute{Key: "trace.parent_id", Value: traceCtx.ParentID},
				)
			}
			span, ctx = tm.Provider.Tracer.Start(ctx, r.Method+" "+r.URL.Path, attrs...)
		}

		rw := &telemetryResponseWriter{ResponseWriter: w, statusCode: 200}

		next.ServeHTTP(rw, r.WithContext(ctx))

		if span != nil {
			span.SetAttributes(
				Attribute{Key: "http.status_code", Value: rw.statusCode},
				Attribute{Key: "http.duration_ms", Value: time.Since(start).Milliseconds()},
			)
			span.End()
		}

		if tm.Provider != nil && tm.Provider.Meter != nil {
			counter := tm.Provider.Meter.Counter("http_requests_total",
				Attribute{Key: "method", Value: r.Method},
				Attribute{Key: "path", Value: r.URL.Path},
			)
			counter.Add(ctx, 1)

			hist := tm.Provider.Meter.Histogram("http_request_duration_ms",
				Attribute{Key: "method", Value: r.Method},
			)
			hist.Record(ctx, float64(time.Since(start).Milliseconds()))
		}

		if traceCtx != nil {
			w.Header().Set("traceparent", traceCtx.Encode())
		}
	})
}

type telemetryResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *telemetryResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}