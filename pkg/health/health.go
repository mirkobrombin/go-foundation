package health

import (
	"context"
	"sync"
	"time"
)

// Status represents the health status of a component.
type Status int

const (
	StatusHealthy   Status = iota
	StatusDegraded
	StatusUnhealthy
)

// String returns the textual representation of the status.
func (s Status) String() string {
	switch s {
	case StatusHealthy:
		return "healthy"
	case StatusDegraded:
		return "degraded"
	case StatusUnhealthy:
		return "unhealthy"
	default:
		return "unknown"
	}
}

// Report contains the result of a health check.
type Report struct {
	Status   Status
	Duration time.Duration
	Details  map[string]any
}

// Checker performs a health check and returns a Report.
type Checker interface {
	Check(ctx context.Context) Report
}

// Registry manages a set of named health Checkers.
type Registry struct {
	mu     sync.RWMutex
	checks map[string]Checker
}

// NewRegistry creates a new health check Registry.
func NewRegistry() *Registry {
	return &Registry{checks: make(map[string]Checker)}
}

// Register adds a named checker to the registry.
func (r *Registry) Register(name string, c Checker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checks[name] = c
}

func (r *Registry) CheckAll(ctx context.Context) map[string]Report {
	r.mu.RLock()
	names := make([]string, 0, len(r.checks))
	for n := range r.checks {
		names = append(names, n)
	}
	r.mu.RUnlock()

	results := make(map[string]Report, len(names))
	for _, name := range names {
		r.mu.RLock()
		checker := r.checks[name]
		r.mu.RUnlock()

		start := time.Now()
		report := checker.Check(ctx)
		report.Duration = time.Since(start)
		results[name] = report
	}
	return results
}
