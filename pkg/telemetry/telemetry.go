package telemetry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mirkobrombin/go-foundation/pkg/options"
)

// Tracer creates and manages spans.
type Tracer interface {
	Start(ctx context.Context, name string, attrs ...Attribute) (Span, context.Context)
}

// Span represents an active span in a trace.
type Span interface {
	SetAttributes(attrs ...Attribute)
	End()
}

// Meter creates and records metrics.
type Meter interface {
	Counter(name string, attrs ...Attribute) Counter
	Histogram(name string, attrs ...Attribute) Histogram
	Gauge(name string, attrs ...Attribute) Gauge
}

// Counter records monotonically increasing values.
type Counter interface {
	Add(ctx context.Context, delta int64)
}

// Histogram records distribution of values.
type Histogram interface {
	Record(ctx context.Context, value float64)
}

// Gauge records current values.
type Gauge interface {
	Set(ctx context.Context, value float64)
}

// Attribute is a key-value pair for spans and metrics.
type Attribute struct {
	Key   string
	Value any
}

// Provider is the central telemetry hub.
type Provider struct {
	Tracer   Tracer
	Meter    Meter
	shutdown []func()
}

// Option configures a Provider.
type Option = options.Option[Provider]

// NewProvider creates a telemetry provider with noop defaults.
func NewProvider(opts ...Option) *Provider {
	p := &Provider{
		Tracer: noopTracerInst,
		Meter:  noopMeterInst,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// WithTracer sets the tracer implementation.
func WithTracer(t Tracer) Option {
	return func(p *Provider) { p.Tracer = t }
}

// WithMeter sets the meter implementation.
func WithMeter(m Meter) Option {
	return func(p *Provider) { p.Meter = m }
}

// Shutdown cleans up all providers.
func (p *Provider) Shutdown() {
	for _, f := range p.shutdown {
		f()
	}
}

// Timed measures and records the duration of fn as a metric.
func Timed(ctx context.Context, h Histogram, fn func()) time.Duration {
	start := time.Now()
	fn()
	dur := time.Since(start)
	h.Record(ctx, dur.Seconds())
	return dur
}

// Noop implementations

// Noop implementations

var noopTracerInst = &noopTracer{}
var noopMeterInst = &noopMeter{}

type noopTracer struct{}

func (n *noopTracer) Start(ctx context.Context, name string, attrs ...Attribute) (Span, context.Context) {
	return &noopSpan{}, ctx
}

type noopSpan struct{}

func (n *noopSpan) SetAttributes(attrs ...Attribute) {}
func (n *noopSpan) End()                             {}

type noopMeter struct{}

func (n *noopMeter) Counter(name string, attrs ...Attribute) Counter   { return &noopCounter{} }
func (n *noopMeter) Histogram(name string, attrs ...Attribute) Histogram { return &noopHistogram{} }
func (n *noopMeter) Gauge(name string, attrs ...Attribute) Gauge       { return &noopGauge{} }

type noopCounter struct{}

func (n *noopCounter) Add(ctx context.Context, delta int64) {}

type noopHistogram struct{}

func (n *noopHistogram) Record(ctx context.Context, value float64) {}

type noopGauge struct{}

func (n *noopGauge) Set(ctx context.Context, value float64) {}

// SimpleTracer is a basic tracer that logs span starts and ends to stderr.
type SimpleTracer struct {
	mu sync.Mutex
}

// NewSimpleTracer creates a SimpleTracer.
func NewSimpleTracer() *SimpleTracer { return &SimpleTracer{} }

func (s *SimpleTracer) Start(ctx context.Context, name string, attrs ...Attribute) (Span, context.Context) {
	span := &simpleSpan{name: name, start: time.Now(), attrs: attrs}
	return span, ctx
}

type simpleSpan struct {
	name  string
	start time.Time
	attrs []Attribute
}

func (s *simpleSpan) SetAttributes(attrs ...Attribute) {
	s.attrs = append(s.attrs, attrs...)
}

func (s *simpleSpan) End() {
	fmt.Printf("[TRACE] %s duration=%v attrs=%v\n", s.name, time.Since(s.start), s.attrs)
}

// SimpleMeter is a basic meter that stores counters in memory.
type SimpleMeter struct {
	mu        sync.Mutex
	counters  map[string]int64
	histogram map[string][]float64
	gauges    map[string]float64
}

// NewSimpleMeter creates a SimpleMeter.
func NewSimpleMeter() *SimpleMeter {
	return &SimpleMeter{
		counters:  make(map[string]int64),
		histogram: make(map[string][]float64),
		gauges:    make(map[string]float64),
	}
}

func (m *SimpleMeter) Counter(name string, attrs ...Attribute) Counter {
	return &simpleCounter{meter: m, name: name}
}

func (m *SimpleMeter) Histogram(name string, attrs ...Attribute) Histogram {
	return &simpleHistogram{meter: m, name: name}
}

func (m *SimpleMeter) Gauge(name string, attrs ...Attribute) Gauge {
	return &simpleGauge{meter: m, name: name}
}

// GetCounter returns the current counter value by name.
func (m *SimpleMeter) GetCounter(name string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counters[name]
}

// GetGauge returns the current gauge value by name.
func (m *SimpleMeter) GetGauge(name string) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gauges[name]
}

type simpleCounter struct {
	meter *SimpleMeter
	name  string
}

func (c *simpleCounter) Add(ctx context.Context, delta int64) {
	c.meter.mu.Lock()
	c.meter.counters[c.name] += delta
	c.meter.mu.Unlock()
}

type simpleHistogram struct {
	meter *SimpleMeter
	name  string
}

func (h *simpleHistogram) Record(ctx context.Context, value float64) {
	h.meter.mu.Lock()
	h.meter.histogram[h.name] = append(h.meter.histogram[h.name], value)
	h.meter.mu.Unlock()
}

type simpleGauge struct {
	meter *SimpleMeter
	name  string
}

func (g *simpleGauge) Set(ctx context.Context, value float64) {
	g.meter.mu.Lock()
	g.meter.gauges[g.name] = value
	g.meter.mu.Unlock()
}