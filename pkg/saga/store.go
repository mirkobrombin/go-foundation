package saga

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// StepStatus represents the status of a saga step.
type StepStatus string

const (
	// StatusPending indicates the step has not yet executed.
	StatusPending StepStatus = "pending"
	// StatusCompleted indicates the step executed successfully.
	StatusCompleted StepStatus = "completed"
	// StatusCompensated indicates the step was rolled back.
	StatusCompensated StepStatus = "compensated"
	// StatusFailed indicates the step failed.
	StatusFailed StepStatus = "failed"
	// StatusDead indicates the saga exceeded max retries and is dead-lettered.
	StatusDead StepStatus = "dead"
)

// SagaState holds the persisted state of a saga.
type SagaState struct {
	ID             string      `json:"id"`
	Steps          []StepState `json:"steps"`
	Status         StepStatus  `json:"status"`
	Error          string      `json:"error,omitempty"`
	IdempotencyKey string      `json:"idempotency_key,omitempty"`
	RetryCount     int         `json:"retry_count,omitempty"`
	MaxRetries     int         `json:"max_retries,omitempty"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

// StepState holds the persisted state of a single saga step.
type StepState struct {
	Name      string     `json:"name"`
	Status    StepStatus `json:"status"`
	StepIndex int        `json:"step_index"`
}

// SagaStore is the interface for saga state persistence.
type SagaStore interface {
	Save(state *SagaState) error
	Load(id string) (*SagaState, error)
	Delete(id string) error
	ListIncomplete() ([]*SagaState, error)
}

// MemoryStore is an in-memory saga store for testing and single-process use.
type MemoryStore struct {
	mu    sync.RWMutex
	sagas map[string]*SagaState
}

// NewMemoryStore creates a new in-memory saga store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{sagas: make(map[string]*SagaState)}
}

func (m *MemoryStore) Save(state *SagaState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *state
	cp.UpdatedAt = time.Now()
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now()
	}
	m.sagas[state.ID] = &cp
	return nil
}

func (m *MemoryStore) Load(id string) (*SagaState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sagas[id]
	if !ok {
		return nil, fmt.Errorf("saga %q not found", id)
	}
	cp := *s
	return &cp, nil
}

func (m *MemoryStore) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sagas, id)
	return nil
}

func (m *MemoryStore) ListIncomplete() ([]*SagaState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*SagaState
	for _, s := range m.sagas {
		if s.Status == StatusPending || s.Status == StatusFailed {
			cp := *s
			result = append(result, &cp)
		}
	}
	return result, nil
}

// IdempotencyRecorder tracks processed idempotency keys to prevent double execution.
type IdempotencyRecorder struct {
	mu   sync.RWMutex
	seen map[string]*SagaState
}

// NewIdempotencyRecorder creates a new IdempotencyRecorder.
func NewIdempotencyRecorder() *IdempotencyRecorder {
	return &IdempotencyRecorder{seen: make(map[string]*SagaState)}
}

func (r *IdempotencyRecorder) Record(key string, state *SagaState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.seen[key]; ok {
		return &IdempotencyError{Key: key, ExistingState: existing}
	}
	r.seen[key] = state
	return nil
}

func (r *IdempotencyRecorder) Get(key string) (*SagaState, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.seen[key]
	if !ok {
		return nil, false
	}
	cp := *s
	return &cp, true
}

// IdempotencyError is returned when an idempotency key has already been processed.
type IdempotencyError struct {
	Key           string
	ExistingState *SagaState
}

func (e *IdempotencyError) Error() string {
	return fmt.Sprintf("idempotency key %q already processed", e.Key)
}

func (e *IdempotencyError) Is(target error) bool {
	_, ok := target.(*IdempotencyError)
	return ok
}

// DeadLetterQueue holds failed saga states that exceeded max retries.
type DeadLetterQueue struct {
	mu    sync.RWMutex
	items []*DLQEntry
}

// DLQEntry holds a dead-lettered saga state and the reason it was moved.
type DLQEntry struct {
	State     *SagaState
	Reason    string
	DeadSince time.Time
}

// NewDeadLetterQueue creates a new DeadLetterQueue.
func NewDeadLetterQueue() *DeadLetterQueue {
	return &DeadLetterQueue{items: make([]*DLQEntry, 0)}
}

func (dlq *DeadLetterQueue) Enqueue(state *SagaState, reason string) {
	dlq.mu.Lock()
	defer dlq.mu.Unlock()
	dlq.items = append(dlq.items, &DLQEntry{
		State:     state,
		Reason:    reason,
		DeadSince: time.Now(),
	})
}

func (dlq *DeadLetterQueue) List() []*DLQEntry {
	dlq.mu.RLock()
	defer dlq.mu.RUnlock()
	result := make([]*DLQEntry, len(dlq.items))
	copy(result, dlq.items)
	return result
}

func (dlq *DeadLetterQueue) Len() int {
	dlq.mu.RLock()
	defer dlq.mu.RUnlock()
	return len(dlq.items)
}

func (dlq *DeadLetterQueue) Remove(id string) {
	dlq.mu.Lock()
	defer dlq.mu.Unlock()
	for i, entry := range dlq.items {
		if entry.State.ID == id {
			dlq.items = append(dlq.items[:i], dlq.items[i+1:]...)
			return
		}
	}
}

// ProcessWithDLQ retries dead sagas up to maxRetries, then moves to dead state.
func ProcessWithDLQ(store SagaStore, dlq *DeadLetterQueue, maxRetries int) func(id string, runFunc func(ctx context.Context) error) func(ctx context.Context) error {
	return func(id string, runFunc func(ctx context.Context) error) func(ctx context.Context) error {
		return func(ctx context.Context) error {
			state, err := store.Load(id)
			if err != nil {
				return err
			}
			if state.RetryCount >= maxRetries {
				state.Status = StatusDead
				state.Error = errors.New("exceeded max retries").Error()
				_ = store.Save(state)
				dlq.Enqueue(state, "max retries exceeded")
				return fmt.Errorf("saga %q dead: max retries exceeded", id)
			}
			state.RetryCount++
			if err := runFunc(ctx); err != nil {
				state.Status = StatusFailed
				state.Error = err.Error()
				_ = store.Save(state)
				return err
			}
			state.Status = StatusCompleted
			_ = store.Save(state)
			return nil
		}
	}
}

// FileStore persists saga state to disk.
type FileStore struct {
	dir string
	mu  sync.RWMutex
}

// NewStore creates a FileStore that writes state JSON files to dir.
func NewStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("saga store: cannot create dir %s: %w", dir, err)
	}
	return &FileStore{dir: dir}, nil
}

func (s *FileStore) Save(state *SagaState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state.UpdatedAt = time.Now()
	if state.CreatedAt.IsZero() {
		state.CreatedAt = time.Now()
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("saga store: marshal: %w", err)
	}

	path := filepath.Join(s.dir, state.ID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("saga store: write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("saga store: rename: %w", err)
	}
	return nil
}

func (s *FileStore) Load(id string) (*SagaState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.dir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("saga store: read %s: %w", id, err)
	}

	var state SagaState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("saga store: unmarshal %s: %w", id, err)
	}
	return &state, nil
}

func (s *FileStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, id+".json")
	return os.Remove(path)
}

func (s *FileStore) ListIncomplete() ([]*SagaState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("saga store: readdir: %w", err)
	}

	var result []*SagaState
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			continue
		}
		var state SagaState
		if err := json.Unmarshal(data, &state); err != nil {
			continue
		}
		if state.Status == StatusPending || state.Status == StatusFailed {
			result = append(result, &state)
		}
	}
	return result, nil
}

// RecoverableWorkflow is a saga that persists state for crash recovery.
type RecoverableWorkflow struct {
	*Workflow
	id    string
	store SagaStore
	state *SagaState
}

// NewRecoverable creates a saga that persists state using the given store.
func NewRecoverable(id string, store SagaStore) *RecoverableWorkflow {
	return &RecoverableWorkflow{
		Workflow: New(),
		id:       id,
		store:    store,
		state: &SagaState{
			ID:     id,
			Status: StatusPending,
		},
	}
}

func (rw *RecoverableWorkflow) Add(name string, do, compensate func(ctx context.Context) error) {
	idx := len(rw.state.Steps)
	rw.state.Steps = append(rw.state.Steps, StepState{
		Name:      name,
		Status:    StatusPending,
		StepIndex: idx,
	})
	rw.Workflow.Add(name, do, compensate)
}

func (rw *RecoverableWorkflow) Run(ctx context.Context) error {
	rw.state.Status = StatusPending
	_ = rw.store.Save(rw.state)

	for i, item := range rw.steps {
		if ctx.Err() != nil {
			rw.state.Status = StatusFailed
			rw.state.Error = ctx.Err().Error()
			_ = rw.store.Save(rw.state)
			return rw.rollback(ctx, ctx.Err())
		}

		var err error
		switch v := item.(type) {
		case Step:
			err = rw.runStepTracking(ctx, v, i)
		case Group:
			err = rw.runGroupTracking(ctx, v, i)
		}

		if err != nil {
			rw.state.Status = StatusFailed
			rw.state.Error = err.Error()
			_ = rw.store.Save(rw.state)
			return rw.rollback(ctx, err)
		}
	}

	rw.state.Status = StatusCompleted
	_ = rw.store.Save(rw.state)
	return nil
}

func (rw *RecoverableWorkflow) runStepTracking(ctx context.Context, step Step, stepIndex int) error {
	if err := step.Do(ctx); err != nil {
		return fmt.Errorf("step '%s' failed: %w", step.Name, err)
	}

	rw.mu.Lock()
	if step.Compensate != nil {
		rw.stack = append(rw.stack, step)
	}
	rw.mu.Unlock()

	if stepIndex < len(rw.state.Steps) {
		rw.state.Steps[stepIndex].Status = StatusCompleted
		_ = rw.store.Save(rw.state)
	}
	return nil
}

func (rw *RecoverableWorkflow) runGroupTracking(ctx context.Context, group Group, stepIndex int) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(group))

	for _, step := range group {
		wg.Add(1)
		go func(s Step) {
			defer wg.Done()
			if err := rw.runStepTracking(ctx, s, stepIndex); err != nil {
				errChan <- err
			}
		}(step)
	}

	wg.Wait()
	close(errChan)

	if len(errChan) > 0 {
		var errs []error
		for e := range errChan {
			errs = append(errs, e)
		}
		return fmt.Errorf("group failed: %w", joinErrors(errs))
	}
	return nil
}

func (rw *RecoverableWorkflow) rollback(ctx context.Context, triggerErr error) error {
	rollbackCtx := context.WithoutCancel(ctx)
	var errs []error
	errs = append(errs, triggerErr)

	rw.mu.Lock()
	defer rw.mu.Unlock()

	for i := len(rw.stack) - 1; i >= 0; i-- {
		step := rw.stack[i]
		if err := step.Compensate(rollbackCtx); err != nil {
			errs = append(errs, fmt.Errorf("rollback failed for '%s': %w", step.Name, err))
		} else {
			for j := range rw.state.Steps {
				if rw.state.Steps[j].Name == step.Name {
					rw.state.Steps[j].Status = StatusCompensated
					break
				}
			}
		}
		_ = rw.store.Save(rw.state)
	}

	return joinErrors(errs)
}

func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%v", errs)
}