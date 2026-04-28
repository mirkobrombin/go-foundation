package metrics_test

import (
	"sync"
	"testing"
	"time"

	"github.com/mirkobrombin/go-foundation/pkg/metrics"
)

func TestCounterTracksValues(t *testing.T) {
	counter := metrics.NewCounter()
	counter.Inc()
	counter.Add(4)
	if counter.Value() != 5 {
		t.Fatalf("Value() = %d, want %d", counter.Value(), 5)
	}
}

func TestCounterIsConcurrencySafe(t *testing.T) {
	counter := metrics.NewCounter()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter.Inc()
		}()
	}
	wg.Wait()
	if counter.Value() != 32 {
		t.Fatalf("Value() = %d, want %d", counter.Value(), 32)
	}
}

func TestGauge(t *testing.T) {
	g := metrics.NewGauge()
	g.Set(42)
	if g.Value() != 42 {
		t.Errorf("Value() = %f, want 42", g.Value())
	}
	g.Inc()
	if g.Value() != 43 {
		t.Errorf("after Inc() = %f, want 43", g.Value())
	}
	g.Dec()
	if g.Value() != 42 {
		t.Errorf("after Dec() = %f, want 42", g.Value())
	}
	g.Add(10)
	if g.Value() != 52 {
		t.Errorf("after Add(10) = %f, want 52", g.Value())
	}
	g.Sub(20)
	if g.Value() != 32 {
		t.Errorf("after Sub(20) = %f, want 32", g.Value())
	}
}

func TestHistogram(t *testing.T) {
	h := metrics.NewHistogram([]float64{1, 5, 10})
	h.Observe(0.5)
	h.Observe(3)
	h.Observe(7)
	h.Observe(15)
}

func TestTimer(t *testing.T) {
	timer := metrics.NewTimer()
	tm := timer.Start()
	time.Sleep(time.Millisecond)
	d := tm.Stop()
	if d < time.Millisecond {
		t.Errorf("Stop() = %v, want >= 1ms", d)
	}
}

func TestGaugeConcurrencySafe(t *testing.T) {
	g := metrics.NewGauge()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.Inc()
		}()
	}
	wg.Wait()
	if g.Value() != 100 {
		t.Errorf("Value() = %f, want 100", g.Value())
	}
}
