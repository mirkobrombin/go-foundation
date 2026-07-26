package resiliency

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// ErrCircuitOpen is returned by Execute when the circuit is in the Open state.
var ErrCircuitOpen = errors.New("circuit breaker: circuit is open")

// State represents the current state of a CircuitBreaker.
type State int

// Circuit breaker states.
const (
	// StateClosed allows all requests through.
	StateClosed State = iota
	// StateOpen rejects all requests until the timeout elapses.
	StateOpen
	// StateHalfOpen allows a single probe request to test recovery.
	StateHalfOpen
)

// CircuitBreaker protects a caller from repeated failures.
//
// It implements a state machine with Closed, Open, and Half-Open states.
//
// Example:
//
//	cb := resiliency.NewCircuitBreaker(3, time.Minute)
//	err := cb.Execute(func() error {
//		return doRiskyOperation()
//	})
type CircuitBreaker struct {
	mu            sync.RWMutex
	state         State
	failures      int
	threshold     int
	timeout       time.Duration
	lastFailure   time.Time
	onStateChange func(from, to State)
	probing       int32 // 1 when a probe is in flight (HalfOpen only)
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(threshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		threshold: threshold,
		timeout:   timeout,
		state:     StateClosed,
	}
}

// OnStateChange registers a callback for state transitions.
func (cb *CircuitBreaker) OnStateChange(fn func(from, to State)) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.onStateChange = fn
}

// Execute calls fn if the circuit allows it, recording the outcome to drive
// state transitions. It returns ErrCircuitOpen when the circuit is in the Open
// state, otherwise it returns the error (if any) produced by fn.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if !cb.allow() {
		return ErrCircuitOpen
	}

	err := fn()
	if err != nil {
		cb.onFailure()
		return err
	}

	cb.onSuccess()
	return nil
}

// allow returns true if the current state permits a call through.
func (cb *CircuitBreaker) allow() bool {
	cb.mu.Lock()
	var callback func(State, State)
	var from State
	allowed := false
	switch cb.state {
	case StateClosed:
		allowed = true
	case StateOpen:
		if time.Since(cb.lastFailure) > cb.timeout {
			atomic.StoreInt32(&cb.probing, 0)
			callback, from = cb.changeStateLocked(StateHalfOpen)
			allowed = atomic.CompareAndSwapInt32(&cb.probing, 0, 1)
		}
	case StateHalfOpen:
		allowed = atomic.CompareAndSwapInt32(&cb.probing, 0, 1)
	}
	cb.mu.Unlock()
	if callback != nil {
		callback(from, StateHalfOpen)
	}
	return allowed
}

// onSuccess records a successful call and transitions out of HalfOpen if needed.
func (cb *CircuitBreaker) onSuccess() {
	cb.mu.Lock()
	var callback func(State, State)
	var from State
	if cb.state == StateHalfOpen {
		atomic.StoreInt32(&cb.probing, 0)
		callback, from = cb.changeStateLocked(StateClosed)
	}
	cb.failures = 0
	cb.mu.Unlock()
	if callback != nil {
		callback(from, StateClosed)
	}
}

// onFailure records a failed call and trips the circuit when the threshold is reached.
func (cb *CircuitBreaker) onFailure() {
	cb.mu.Lock()
	var callback func(State, State)
	var from State
	to := cb.state
	cb.failures++
	cb.lastFailure = time.Now()

	if cb.state == StateClosed && cb.failures >= cb.threshold {
		to = StateOpen
		callback, from = cb.changeStateLocked(to)
	} else if cb.state == StateHalfOpen {
		atomic.StoreInt32(&cb.probing, 0)
		cb.failures = 0
		to = StateOpen
		callback, from = cb.changeStateLocked(to)
	}
	cb.mu.Unlock()
	if callback != nil {
		callback(from, to)
	}
}

func (cb *CircuitBreaker) changeStateLocked(to State) (func(State, State), State) {
	from := cb.state
	cb.state = to
	return cb.onStateChange, from
}

// State returns the current state of the circuit breaker.
func (cb *CircuitBreaker) State() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}
