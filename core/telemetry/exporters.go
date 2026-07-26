package telemetry

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

// OTLPExporter sends spans and metrics to an OTLP-compatible endpoint over HTTP/JSON.
type OTLPExporter struct {
	endpoint string
	client   *http.Client
	batchMu  sync.Mutex
	exportWG sync.WaitGroup
	flushMu  sync.Mutex
	spans    []OTLPSpan
	metrics  []OTLPMetric
	inFlight int
	maxBatch int
	maxQueue int
	flushMs  time.Duration
	flushCh  chan struct{}
	stopCh   chan struct{}
	stopOnce sync.Once
	done     chan struct{}
	closeMu  sync.Mutex
	closeErr error
	closed   bool
}

// OTLPSpan is a span accepted by OTLPExporter.
type OTLPSpan struct {
	TraceID    string
	SpanID     string
	Name       string
	StartTime  time.Time
	EndTime    time.Time
	Attributes map[string]any
}

// OTLPMetricKind identifies the OTLP data-point representation.
type OTLPMetricKind string

const (
	// OTLPMetricCounter exports a monotonic cumulative sum.
	OTLPMetricCounter OTLPMetricKind = "counter"
	// OTLPMetricGauge exports the current value.
	OTLPMetricGauge OTLPMetricKind = "gauge"
	// OTLPMetricHistogram exports one histogram observation.
	OTLPMetricHistogram OTLPMetricKind = "histogram"
)

// OTLPMetric is a metric data point accepted by OTLPExporter.
type OTLPMetric struct {
	Name       string
	Kind       OTLPMetricKind
	Value      float64
	Time       time.Time
	Attributes map[string]any
}

var (
	// ErrOTLPExporterClosed is returned after the exporter has closed.
	ErrOTLPExporterClosed = errors.New("telemetry: OTLP exporter is closed")
	// ErrOTLPQueueFull is returned when the bounded queue has reached capacity.
	ErrOTLPQueueFull = errors.New("telemetry: OTLP queue is full")
)

// OTLPOption configures an OTLPExporter.
type OTLPOption func(*OTLPExporter)

// WithOTLPEndpoint sets the OTLP receiver base URL.
func WithOTLPEndpoint(url string) OTLPOption {
	return func(e *OTLPExporter) { e.endpoint = url }
}

// WithOTLPBatchSize sets the maximum batch size before flushing.
func WithOTLPBatchSize(n int) OTLPOption {
	return func(e *OTLPExporter) { e.maxBatch = n }
}

// WithOTLPQueueSize sets the maximum number of pending spans and metrics.
func WithOTLPQueueSize(n int) OTLPOption {
	return func(e *OTLPExporter) { e.maxQueue = n }
}

// NewOTLPExporter creates a new OTLPExporter with the given options.
func NewOTLPExporter(opts ...OTLPOption) *OTLPExporter {
	e := &OTLPExporter{
		endpoint: "http://localhost:4318",
		client:   &http.Client{Timeout: 5 * time.Second},
		maxBatch: 100,
		maxQueue: 10_000,
		flushMs:  5000 * time.Millisecond,
		flushCh:  make(chan struct{}, 1),
		stopCh:   make(chan struct{}),
		done:     make(chan struct{}),
	}
	for _, opt := range opts {
		opt(e)
	}
	if e.maxBatch <= 0 {
		panic("telemetry: OTLP batch size must be positive")
	}
	if e.maxQueue <= 0 {
		panic("telemetry: OTLP queue size must be positive")
	}
	go e.loop()
	return e
}

// ExportSpan queues a span and flushes when the batch threshold is reached.
func (e *OTLPExporter) ExportSpan(span OTLPSpan) error {
	if err := validateOTLPSpan(span); err != nil {
		return err
	}
	e.batchMu.Lock()
	if e.closed {
		e.batchMu.Unlock()
		return ErrOTLPExporterClosed
	}
	if len(e.spans)+len(e.metrics)+e.inFlight >= e.maxQueue {
		e.batchMu.Unlock()
		return ErrOTLPQueueFull
	}
	e.exportWG.Add(1)
	defer e.exportWG.Done()
	e.spans = append(e.spans, span)
	shouldFlush := len(e.spans) >= e.maxBatch
	e.batchMu.Unlock()
	if shouldFlush {
		e.requestFlush()
	}
	return nil
}

// ExportMetric queues a metric and flushes when the batch threshold is reached.
func (e *OTLPExporter) ExportMetric(metric OTLPMetric) error {
	if err := validateOTLPMetric(metric); err != nil {
		return err
	}
	e.batchMu.Lock()
	if e.closed {
		e.batchMu.Unlock()
		return ErrOTLPExporterClosed
	}
	if len(e.spans)+len(e.metrics)+e.inFlight >= e.maxQueue {
		e.batchMu.Unlock()
		return ErrOTLPQueueFull
	}
	e.exportWG.Add(1)
	defer e.exportWG.Done()
	e.metrics = append(e.metrics, metric)
	shouldFlush := len(e.metrics) >= e.maxBatch
	e.batchMu.Unlock()
	if shouldFlush {
		e.requestFlush()
	}
	return nil
}

func (e *OTLPExporter) requestFlush() {
	select {
	case e.flushCh <- struct{}{}:
	default:
	}
}

// Flush sends all currently queued telemetry.
func (e *OTLPExporter) Flush(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	e.flushMu.Lock()
	defer e.flushMu.Unlock()
	e.batchMu.Lock()
	spans := e.spans
	metrics := e.metrics
	e.spans = nil
	e.metrics = nil
	batchSize := len(spans) + len(metrics)
	e.inFlight += batchSize
	e.batchMu.Unlock()

	if len(spans) == 0 && len(metrics) == 0 {
		return nil
	}
	spansSent := len(spans) == 0
	metricsSent := len(metrics) == 0
	defer func() {
		e.batchMu.Lock()
		if !spansSent {
			e.spans = append(spans, e.spans...)
		}
		if !metricsSent {
			e.metrics = append(metrics, e.metrics...)
		}
		e.inFlight -= batchSize
		e.batchMu.Unlock()
	}()

	if len(spans) > 0 {
		if err := e.send(ctx, "traces", otlpTracePayload(spans)); err != nil {
			var partial *otlpPartialSuccessError
			if errors.As(err, &partial) {
				spansSent = true
			}
			return err
		}
		spansSent = true
	}
	if len(metrics) > 0 {
		if err := e.send(ctx, "metrics", otlpMetricPayload(metrics)); err != nil {
			var partial *otlpPartialSuccessError
			if errors.As(err, &partial) {
				metricsSent = true
			}
			return err
		}
		metricsSent = true
	}
	return nil
}

type otlpPartialSuccessError struct {
	signal   string
	rejected string
	message  string
}

func (e *otlpPartialSuccessError) Error() string {
	return fmt.Sprintf("otlp %s partial success rejected %s: %s", e.signal, e.rejected, e.message)
}

func (e *OTLPExporter) send(ctx context.Context, signal string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("otlp marshal %s: %w", signal, err)
	}
	endpoint, err := otlpSignalEndpoint(e.endpoint, signal)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("otlp request %s: %w", signal, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("otlp send %s: %w", signal, err)
	}
	defer resp.Body.Close()
	const maxOTLPResponseSize = 4 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOTLPResponseSize+1))
	if err != nil {
		return fmt.Errorf("otlp read %s response: %w", signal, err)
	}
	if len(body) > maxOTLPResponseSize {
		return fmt.Errorf("otlp %s response exceeds size limit", signal)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("otlp %s status %d: %s", signal, resp.StatusCode, string(body))
	}
	if len(bytes.TrimSpace(body)) > 0 {
		var response struct {
			PartialSuccess struct {
				RejectedSpans      string `json:"rejectedSpans"`
				RejectedDataPoints string `json:"rejectedDataPoints"`
				ErrorMessage       string `json:"errorMessage"`
			} `json:"partialSuccess"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return fmt.Errorf("otlp decode %s response: %w", signal, err)
		}
		rejected := response.PartialSuccess.RejectedSpans
		if signal == "metrics" {
			rejected = response.PartialSuccess.RejectedDataPoints
		}
		if rejected != "" && rejected != "0" {
			return &otlpPartialSuccessError{
				signal:   signal,
				rejected: rejected,
				message:  response.PartialSuccess.ErrorMessage,
			}
		}
	}
	return nil
}

func otlpSignalEndpoint(base, signal string) (string, error) {
	endpoint, err := url.Parse(base)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return "", fmt.Errorf("otlp endpoint %q is invalid", base)
	}
	cleanPath := strings.TrimSuffix(endpoint.Path, "/")
	for _, suffix := range []string{"/v1/traces", "/v1/metrics"} {
		cleanPath = strings.TrimSuffix(cleanPath, suffix)
	}
	endpoint.Path = path.Join("/", cleanPath, "v1", signal)
	return endpoint.String(), nil
}

func validateOTLPSpan(span OTLPSpan) error {
	if len(span.TraceID) != 32 || !isLowerHex(span.TraceID) || allZero(span.TraceID) {
		return errors.New("telemetry: OTLP span requires a non-zero 32-character lowercase hexadecimal trace ID")
	}
	if len(span.SpanID) != 16 || !isLowerHex(span.SpanID) || allZero(span.SpanID) {
		return errors.New("telemetry: OTLP span requires a non-zero 16-character lowercase hexadecimal span ID")
	}
	if span.Name == "" {
		return errors.New("telemetry: OTLP span name cannot be empty")
	}
	if span.StartTime.IsZero() || span.EndTime.IsZero() || span.EndTime.Before(span.StartTime) {
		return errors.New("telemetry: OTLP span requires a valid start and end time")
	}
	return nil
}

func validateOTLPMetric(metric OTLPMetric) error {
	if metric.Name == "" {
		return errors.New("telemetry: OTLP metric name cannot be empty")
	}
	switch metric.Kind {
	case OTLPMetricCounter, OTLPMetricGauge, OTLPMetricHistogram:
	default:
		return fmt.Errorf("telemetry: unsupported OTLP metric kind %q", metric.Kind)
	}
	return nil
}

func otlpTracePayload(spans []OTLPSpan) any {
	items := make([]map[string]any, 0, len(spans))
	for _, span := range spans {
		items = append(items, map[string]any{
			"traceId":           span.TraceID,
			"spanId":            span.SpanID,
			"name":              span.Name,
			"kind":              1,
			"startTimeUnixNano": strconv.FormatInt(span.StartTime.UnixNano(), 10),
			"endTimeUnixNano":   strconv.FormatInt(span.EndTime.UnixNano(), 10),
			"attributes":        otlpAttributes(span.Attributes),
		})
	}
	return map[string]any{
		"resourceSpans": []any{
			map[string]any{
				"scopeSpans": []any{
					map[string]any{"spans": items},
				},
			},
		},
	}
}

func otlpMetricPayload(metrics []OTLPMetric) any {
	items := make([]map[string]any, 0, len(metrics))
	for _, metric := range metrics {
		timestamp := metric.Time
		if timestamp.IsZero() {
			timestamp = time.Now()
		}
		point := map[string]any{
			"timeUnixNano": strconv.FormatInt(timestamp.UnixNano(), 10),
			"asDouble":     metric.Value,
			"attributes":   otlpAttributes(metric.Attributes),
		}
		item := map[string]any{"name": metric.Name}
		switch metric.Kind {
		case OTLPMetricCounter:
			item["sum"] = map[string]any{
				"aggregationTemporality": 2,
				"isMonotonic":            true,
				"dataPoints":             []any{point},
			}
		case OTLPMetricGauge:
			item["gauge"] = map[string]any{"dataPoints": []any{point}}
		case OTLPMetricHistogram:
			delete(point, "asDouble")
			point["count"] = "1"
			point["sum"] = metric.Value
			point["min"] = metric.Value
			point["max"] = metric.Value
			point["bucketCounts"] = []string{"1"}
			point["explicitBounds"] = []float64{}
			item["histogram"] = map[string]any{
				"aggregationTemporality": 2,
				"dataPoints":             []any{point},
			}
		}
		items = append(items, item)
	}
	return map[string]any{
		"resourceMetrics": []any{
			map[string]any{
				"scopeMetrics": []any{
					map[string]any{"metrics": items},
				},
			},
		},
	}
}

func otlpAttributes(attributes map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(attributes))
	for key, value := range attributes {
		result = append(result, map[string]any{
			"key":   key,
			"value": otlpAnyValue(value),
		})
	}
	return result
}

func otlpAnyValue(value any) map[string]any {
	switch typed := value.(type) {
	case bool:
		return map[string]any{"boolValue": typed}
	case int:
		return map[string]any{"intValue": strconv.FormatInt(int64(typed), 10)}
	case int8:
		return map[string]any{"intValue": strconv.FormatInt(int64(typed), 10)}
	case int16:
		return map[string]any{"intValue": strconv.FormatInt(int64(typed), 10)}
	case int32:
		return map[string]any{"intValue": strconv.FormatInt(int64(typed), 10)}
	case int64:
		return map[string]any{"intValue": strconv.FormatInt(typed, 10)}
	case uint:
		return map[string]any{"intValue": strconv.FormatUint(uint64(typed), 10)}
	case uint8:
		return map[string]any{"intValue": strconv.FormatUint(uint64(typed), 10)}
	case uint16:
		return map[string]any{"intValue": strconv.FormatUint(uint64(typed), 10)}
	case uint32:
		return map[string]any{"intValue": strconv.FormatUint(uint64(typed), 10)}
	case uint64:
		return map[string]any{"intValue": strconv.FormatUint(typed, 10)}
	case float32:
		return map[string]any{"doubleValue": float64(typed)}
	case float64:
		return map[string]any{"doubleValue": typed}
	case string:
		return map[string]any{"stringValue": typed}
	default:
		return map[string]any{"stringValue": fmt.Sprint(typed)}
	}
}

func (e *OTLPExporter) loop() {
	ticker := time.NewTicker(e.flushMs)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = e.Flush(context.Background())
		case <-e.flushCh:
			if err := e.Flush(context.Background()); err != nil {
				select {
				case <-e.flushCh:
				default:
				}
			}
		case <-e.stopCh:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := e.Flush(ctx)
			cancel()
			e.closeMu.Lock()
			e.closeErr = err
			e.closeMu.Unlock()
			close(e.done)
			return
		}
	}
}

func (e *OTLPExporter) Close() error {
	e.stopOnce.Do(func() {
		e.batchMu.Lock()
		e.closed = true
		e.batchMu.Unlock()
		e.exportWG.Wait()
		close(e.stopCh)
	})
	<-e.done
	e.closeMu.Lock()
	defer e.closeMu.Unlock()
	return e.closeErr
}

// --- Prometheus Text Format Exporter ---

type PrometheusExporter struct {
	mu       sync.RWMutex
	counters map[string]int64
	gauges   map[string]float64
	histos   map[string]*promHistogram
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
		gauges:   make(map[string]float64),
		histos:   make(map[string]*promHistogram),
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
	if len(tc.TraceFlags) != 2 {
		return nil, fmt.Errorf("invalid trace flags length: %d", len(tc.TraceFlags))
	}
	if !isLowerHex(tc.TraceID) || !isLowerHex(tc.ParentID) || !isLowerHex(tc.TraceFlags) {
		return nil, fmt.Errorf("traceparent IDs and flags must use lowercase hexadecimal")
	}
	if allZero(tc.TraceID) || allZero(tc.ParentID) {
		return nil, fmt.Errorf("traceparent trace and parent IDs cannot be all zero")
	}
	return tc, nil
}

func isLowerHex(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

func allZero(value string) bool {
	return strings.Trim(value, "0") == ""
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
				{Key: "http.url", Value: telemetryURL(r)},
			}
			if traceCtx != nil {
				attrs = append(attrs,
					Attribute{Key: "trace.parent_id", Value: traceCtx.ParentID},
				)
			}
			span, ctx = tm.Provider.Tracer.Start(ctx, r.Method+" "+r.URL.Path, attrs...)
		}

		rw := &telemetryResponseWriter{ResponseWriter: w, statusCode: 200}

		if traceCtx != nil {
			w.Header().Set("traceparent", traceCtx.Encode())
		}
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

	})
}

func telemetryURL(r *http.Request) string {
	if r == nil || r.URL == nil {
		return ""
	}
	safe := *r.URL
	safe.User = nil
	safe.RawQuery = ""
	safe.ForceQuery = false
	safe.Fragment = ""
	return safe.String()
}

type telemetryResponseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (w *telemetryResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *telemetryResponseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *telemetryResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *telemetryResponseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *telemetryResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("telemetry: response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func (w *telemetryResponseWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}

func (w *telemetryResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if readerFrom, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return readerFrom.ReadFrom(reader)
	}
	return io.Copy(w.ResponseWriter, reader)
}
