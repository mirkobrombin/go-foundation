package metrics

import (
	"sort"
	"sync"
	"time"
)

// Counter tracks a monotonically increasing integer value.
type Counter interface {
	Inc()
	Add(int64)
	Value() int64
}

// Gauge tracks a float64 value that can go up and down.
type Gauge interface {
	Set(float64)
	Inc()
	Dec()
	Add(float64)
	Sub(float64)
	Value() float64
}

// Histogram records observed float64 values in buckets.
type Histogram interface {
	Observe(float64)
}

// Timer measures elapsed time for an operation.
type Timer interface {
	Start() *Timing
	ObserveDuration()
}

// Timing holds a start time for measuring duration.
type Timing struct {
	start time.Time
}

func (t *Timing) Stop() time.Duration {
	return time.Since(t.start)
}

// SimpleCounter is a mutex-protected Counter implementation.
type SimpleCounter struct {
	mu sync.Mutex
	v  int64
}

// NewCounter creates a new SimpleCounter.
func NewCounter() *SimpleCounter { return &SimpleCounter{} }
func (c *SimpleCounter) Inc()    { c.Add(1) }
func (c *SimpleCounter) Add(n int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.v += n
}
func (c *SimpleCounter) Value() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.v
}

// SimpleGauge is a mutex-protected Gauge implementation.
type SimpleGauge struct {
	mu sync.Mutex
	v  float64
}

// NewGauge creates a new SimpleGauge.
func NewGauge() *SimpleGauge { return &SimpleGauge{} }
func (g *SimpleGauge) Set(v float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.v = v
}
func (g *SimpleGauge) Inc()    { g.Add(1) }
func (g *SimpleGauge) Dec()    { g.Sub(1) }
func (g *SimpleGauge) Add(v float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.v += v
}
func (g *SimpleGauge) Sub(v float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.v -= v
}
func (g *SimpleGauge) Value() float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.v
}

// SimpleHistogram is a mutex-protected Histogram implementation.
type SimpleHistogram struct {
	mu      sync.Mutex
	buckets []float64
	counts  []int64
	total   int64
	sum     float64
}

// NewHistogram creates a new SimpleHistogram with the given bucket boundaries.
func NewHistogram(buckets []float64) *SimpleHistogram {
	sorted := append([]float64(nil), buckets...)
	sort.Float64s(sorted)
	return &SimpleHistogram{
		buckets: sorted,
		counts:  make([]int64, len(sorted)+1),
	}
}

func (h *SimpleHistogram) Observe(v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.total++
	h.sum += v
	for i, b := range h.buckets {
		if v <= b {
			h.counts[i]++
			return
		}
	}
	h.counts[len(h.counts)-1]++
}

// SimpleTimer is a basic Timer implementation.
type SimpleTimer struct{}

// NewTimer creates a new SimpleTimer.
func NewTimer() *SimpleTimer { return &SimpleTimer{} }

func (t *SimpleTimer) Start() *Timing {
	return &Timing{start: time.Now()}
}

func (t *SimpleTimer) ObserveDuration() {}
