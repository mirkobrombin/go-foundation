package saga

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// ErrSagaNotFound indicates that a persisted saga does not exist.
var ErrSagaNotFound = errors.New("saga: state not found")

// MemoryStore is an in-memory saga store for testing and single-process use.
type MemoryStore struct {
	mu       sync.RWMutex
	sagas    map[string]*SagaState
	capacity int
}

// NewMemoryStore creates a new in-memory saga store.
func NewMemoryStore(capacity ...int) *MemoryStore {
	limit := 10_000
	if len(capacity) > 0 && capacity[0] > 0 {
		limit = capacity[0]
	}
	return &MemoryStore{sagas: make(map[string]*SagaState), capacity: limit}
}

func (m *MemoryStore) Save(state *SagaState) error {
	if state == nil {
		return errors.New("saga memory store: state cannot be nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.sagas[state.ID]; !exists && len(m.sagas) >= m.capacity {
		return errors.New("saga memory store: capacity reached")
	}
	cp := cloneSagaState(state)
	cp.UpdatedAt = time.Now()
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now()
	}
	m.sagas[state.ID] = cp
	return nil
}

func (m *MemoryStore) Load(id string) (*SagaState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sagas[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrSagaNotFound, id)
	}
	return cloneSagaState(s), nil
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
			result = append(result, cloneSagaState(s))
		}
	}
	return result, nil
}

// IdempotencyRecorder tracks processed idempotency keys to prevent double execution.
type IdempotencyRecorder struct {
	mu       sync.RWMutex
	seen     map[string]*SagaState
	capacity int
}

// NewIdempotencyRecorder creates a new IdempotencyRecorder.
func NewIdempotencyRecorder(capacity ...int) *IdempotencyRecorder {
	limit := 10_000
	if len(capacity) > 0 && capacity[0] > 0 {
		limit = capacity[0]
	}
	return &IdempotencyRecorder{seen: make(map[string]*SagaState), capacity: limit}
}

func (r *IdempotencyRecorder) Record(key string, state *SagaState) error {
	if key == "" || state == nil {
		return errors.New("saga idempotency recorder: key and state are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.seen[key]; ok {
		return &IdempotencyError{Key: key, ExistingState: cloneSagaState(existing)}
	}
	if len(r.seen) >= r.capacity {
		return errors.New("saga idempotency recorder: capacity reached")
	}
	r.seen[key] = cloneSagaState(state)
	return nil
}

func (r *IdempotencyRecorder) Get(key string) (*SagaState, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.seen[key]
	if !ok {
		return nil, false
	}
	return cloneSagaState(s), true
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
	mu       sync.RWMutex
	items    []*DLQEntry
	capacity int
}

// DLQEntry holds a dead-lettered saga state and the reason it was moved.
type DLQEntry struct {
	State     *SagaState
	Reason    string
	DeadSince time.Time
}

// NewDeadLetterQueue creates a new DeadLetterQueue.
func NewDeadLetterQueue(capacity ...int) *DeadLetterQueue {
	limit := 10_000
	if len(capacity) > 0 && capacity[0] > 0 {
		limit = capacity[0]
	}
	return &DeadLetterQueue{items: make([]*DLQEntry, 0), capacity: limit}
}

func (dlq *DeadLetterQueue) Enqueue(state *SagaState, reason string) error {
	if state == nil {
		return errors.New("saga dead letter queue: state cannot be nil")
	}
	dlq.mu.Lock()
	defer dlq.mu.Unlock()
	if len(dlq.items) >= dlq.capacity {
		return errors.New("saga dead letter queue: capacity reached")
	}
	dlq.items = append(dlq.items, &DLQEntry{
		State:     cloneSagaState(state),
		Reason:    reason,
		DeadSince: time.Now(),
	})
	return nil
}

func (dlq *DeadLetterQueue) List() []*DLQEntry {
	dlq.mu.RLock()
	defer dlq.mu.RUnlock()
	result := make([]*DLQEntry, 0, len(dlq.items))
	for _, entry := range dlq.items {
		copyEntry := *entry
		copyEntry.State = cloneSagaState(entry.State)
		result = append(result, &copyEntry)
	}
	return result
}

func cloneSagaState(state *SagaState) *SagaState {
	if state == nil {
		return nil
	}
	clone := *state
	clone.Steps = append([]StepState(nil), state.Steps...)
	return &clone
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

type sagaRunLock struct {
	mu   sync.Mutex
	refs int
}

type sagaRunLocks struct {
	mu    sync.Mutex
	locks map[string]*sagaRunLock
}

func (l *sagaRunLocks) acquire(id string) func() {
	l.mu.Lock()
	if l.locks == nil {
		l.locks = make(map[string]*sagaRunLock)
	}
	lock := l.locks[id]
	if lock == nil {
		lock = &sagaRunLock{}
		l.locks[id] = lock
	}
	lock.refs++
	l.mu.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		l.mu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(l.locks, id)
		}
		l.mu.Unlock()
	}
}

// ProcessWithDLQ retries dead sagas up to maxRetries, then moves to dead state.
func ProcessWithDLQ(store SagaStore, dlq *DeadLetterQueue, maxRetries int) func(id string, runFunc func(ctx context.Context) error) func(ctx context.Context) error {
	var locks sagaRunLocks
	return func(id string, runFunc func(ctx context.Context) error) func(ctx context.Context) error {
		return func(ctx context.Context) error {
			release := locks.acquire(id)
			defer release()
			state, err := store.Load(id)
			if err != nil {
				return err
			}
			if state.Status == StatusCompleted {
				return nil
			}
			if state.Status == StatusDead {
				return fmt.Errorf("saga %q is dead", id)
			}
			if state.RetryCount >= maxRetries {
				state.Status = StatusDead
				state.Error = errors.New("exceeded max retries").Error()
				if err := store.Save(state); err != nil {
					return fmt.Errorf("saga %q persist dead state: %w", id, err)
				}
				if err := dlq.Enqueue(state, "max retries exceeded"); err != nil {
					return fmt.Errorf("saga %q enqueue dead state: %w", id, err)
				}
				return fmt.Errorf("saga %q dead: max retries exceeded", id)
			}
			state.RetryCount++
			if err := callSagaFunction(ctx, runFunc); err != nil {
				state.Status = StatusFailed
				state.Error = err.Error()
				if saveErr := store.Save(state); saveErr != nil {
					return errors.Join(err, fmt.Errorf("saga %q persist failure: %w", id, saveErr))
				}
				return err
			}
			state.Status = StatusCompleted
			if err := store.Save(state); err != nil {
				return fmt.Errorf("saga %q persist completion: %w", id, err)
			}
			return nil
		}
	}
}

func callSagaFunction(ctx context.Context, fn func(context.Context) error) (err error) {
	if fn == nil {
		return errors.New("saga: run function cannot be nil")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("saga: run function panic: %v", recovered)
		}
	}()
	return fn(ctx)
}

// FileStore persists saga state to disk.
type FileStore struct {
	dir string
	mu  sync.RWMutex
}

const maxSagaStateSize = 1 << 20

// NewStore creates a FileStore that writes state JSON files to dir.
func NewStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("saga store: cannot create dir %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, fmt.Errorf("saga store: cannot secure dir %s: %w", dir, err)
	}
	return &FileStore{dir: dir}, nil
}

func (s *FileStore) Save(state *SagaState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state == nil {
		return errors.New("saga store: state cannot be nil")
	}
	if err := validateStoreID(state.ID); err != nil {
		return err
	}

	state.UpdatedAt = time.Now()
	if state.CreatedAt.IsZero() {
		state.CreatedAt = time.Now()
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("saga store: marshal: %w", err)
	}
	if len(data) > maxSagaStateSize {
		return errors.New("saga store: state exceeds size limit")
	}

	path := filepath.Join(s.dir, state.ID+".json")
	tmp, err := os.CreateTemp(s.dir, ".saga-*.tmp")
	if err != nil {
		return fmt.Errorf("saga store: create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return fmt.Errorf("saga store: secure temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("saga store: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("saga store: close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("saga store: rename: %w", err)
	}
	return nil
}

func (s *FileStore) Load(id string) (*SagaState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := validateStoreID(id); err != nil {
		return nil, err
	}
	data, err := readStoreFile(s.dir, id+".json", maxSagaStateSize)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %q", ErrSagaNotFound, id)
		}
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

	if err := validateStoreID(id); err != nil {
		return err
	}
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	name := id + ".json"
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("saga store: refusing to delete symbolic link")
	}
	return root.Remove(name)
}

func (s *FileStore) ListIncomplete() ([]*SagaState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("saga store: readdir: %w", err)
	}

	var result []*SagaState
	var readErrors []error
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		data, err := readStoreFile(s.dir, entry.Name(), maxSagaStateSize)
		if err != nil {
			readErrors = append(readErrors, fmt.Errorf("saga store: read %s: %w", entry.Name(), err))
			continue
		}
		var state SagaState
		if err := json.Unmarshal(data, &state); err != nil {
			readErrors = append(readErrors, fmt.Errorf("saga store: unmarshal %s: %w", entry.Name(), err))
			continue
		}
		if state.Status == StatusPending || state.Status == StatusFailed {
			result = append(result, &state)
		}
	}
	return result, errors.Join(readErrors...)
}

func validateStoreID(id string) error {
	if id == "" || id == "." || id == ".." ||
		strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("saga store: invalid ID %q", id)
	}
	for _, character := range id {
		if character == 0 {
			return fmt.Errorf("saga store: invalid ID %q", id)
		}
	}
	return nil
}

func readStoreFile(dir, name string, limit int64) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("saga store: refusing to read symbolic link")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("saga store: state file exceeds size limit")
	}
	return data, nil
}

// RecoverableWorkflow is a saga that persists state for crash recovery.
type RecoverableWorkflow struct {
	*Workflow
	id      string
	store   SagaStore
	state   *SagaState
	stateMu sync.Mutex
	indexes [][]int
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
	rw.stateMu.Lock()
	idx := len(rw.state.Steps)
	rw.state.Steps = append(rw.state.Steps, StepState{
		Name:      name,
		Status:    StatusPending,
		StepIndex: idx,
	})
	rw.stateMu.Unlock()
	rw.Workflow.Add(name, do, compensate)
	rw.indexes = append(rw.indexes, []int{idx})
}

// AddGroup appends a parallel group and tracks each step independently.
func (rw *RecoverableWorkflow) AddGroup(group Group) {
	rw.stateMu.Lock()
	indexes := make([]int, 0, len(group))
	for _, step := range group {
		index := len(rw.state.Steps)
		rw.state.Steps = append(rw.state.Steps, StepState{
			Name:      step.Name,
			Status:    StatusPending,
			StepIndex: index,
		})
		indexes = append(indexes, index)
	}
	rw.stateMu.Unlock()
	rw.Workflow.AddGroup(group)
	rw.indexes = append(rw.indexes, indexes)
}

func (rw *RecoverableWorkflow) Run(ctx context.Context) error {
	if rw.store == nil {
		return errors.New("saga: recoverable workflow requires a store")
	}
	if !rw.running.CompareAndSwap(false, true) {
		return errors.New("saga: workflow is already running")
	}
	defer rw.running.Store(false)

	if err := rw.restoreState(); err != nil {
		return err
	}
	rw.stateMu.Lock()
	alreadyCompleted := rw.state.Status == StatusCompleted
	rw.stateMu.Unlock()
	if alreadyCompleted {
		return nil
	}

	completed := rw.persistedCompletedSteps()
	var completedMu sync.Mutex
	if err := rw.updateAndSave(func(state *SagaState) {
		state.Status = StatusPending
		state.Error = ""
		for index := range state.Steps {
			if state.Steps[index].Status != StatusCompleted {
				state.Steps[index].Status = StatusPending
			}
		}
	}); err != nil {
		return fmt.Errorf("saga %q persist pending state: %w", rw.id, err)
	}

	for i, item := range rw.steps {
		if rw.stepsCompleted(rw.indexes[i]) {
			continue
		}
		if ctx.Err() != nil {
			saveErr := rw.updateAndSave(func(state *SagaState) {
				state.Status = StatusFailed
				state.Error = ctx.Err().Error()
			})
			return errors.Join(rw.rollback(ctx, ctx.Err(), completed), saveErr)
		}

		var err error
		switch v := item.(type) {
		case Step:
			err = rw.runStepTracking(ctx, v, rw.indexes[i][0], &completed, &completedMu)
		case Group:
			var pending Group
			var pendingIndexes []int
			for groupIndex, stateIndex := range rw.indexes[i] {
				if rw.stepsCompleted([]int{stateIndex}) {
					continue
				}
				pending = append(pending, v[groupIndex])
				pendingIndexes = append(pendingIndexes, stateIndex)
			}
			err = rw.runGroupTracking(
				ctx,
				pending,
				pendingIndexes,
				&completed,
				&completedMu,
			)
		}

		if err != nil {
			saveErr := rw.updateAndSave(func(state *SagaState) {
				state.Status = StatusFailed
				state.Error = err.Error()
			})
			return errors.Join(rw.rollback(ctx, err, completed), saveErr)
		}
	}

	if err := rw.updateAndSave(func(state *SagaState) {
		state.Status = StatusCompleted
		state.Error = ""
	}); err != nil {
		return fmt.Errorf("saga %q persist completion: %w", rw.id, err)
	}
	return nil
}

func (rw *RecoverableWorkflow) restoreState() error {
	persisted, err := rw.store.Load(rw.id)
	if errors.Is(err, ErrSagaNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("saga %q load state: %w", rw.id, err)
	}
	if persisted == nil || persisted.ID != rw.id {
		return fmt.Errorf("saga %q loaded invalid state", rw.id)
	}

	rw.stateMu.Lock()
	defer rw.stateMu.Unlock()
	if len(persisted.Steps) != len(rw.state.Steps) {
		return fmt.Errorf(
			"saga %q persisted step count %d does not match workflow step count %d",
			rw.id,
			len(persisted.Steps),
			len(rw.state.Steps),
		)
	}
	for index := range persisted.Steps {
		if persisted.Steps[index].Name != rw.state.Steps[index].Name {
			return fmt.Errorf(
				"saga %q persisted step %d is %q, want %q",
				rw.id,
				index,
				persisted.Steps[index].Name,
				rw.state.Steps[index].Name,
			)
		}
	}
	rw.state = cloneSagaState(persisted)
	return nil
}

func (rw *RecoverableWorkflow) stepsCompleted(indexes []int) bool {
	rw.stateMu.Lock()
	defer rw.stateMu.Unlock()
	for _, index := range indexes {
		if index >= len(rw.state.Steps) || rw.state.Steps[index].Status != StatusCompleted {
			return false
		}
	}
	return true
}

func (rw *RecoverableWorkflow) persistedCompletedSteps() []Step {
	rw.stateMu.Lock()
	statuses := make([]StepStatus, len(rw.state.Steps))
	for index := range rw.state.Steps {
		statuses[index] = rw.state.Steps[index].Status
	}
	rw.stateMu.Unlock()

	var completed []Step
	for itemIndex, item := range rw.steps {
		switch typed := item.(type) {
		case Step:
			if statuses[rw.indexes[itemIndex][0]] == StatusCompleted && typed.Compensate != nil {
				completed = append(completed, typed)
			}
		case Group:
			for groupIndex, stateIndex := range rw.indexes[itemIndex] {
				step := typed[groupIndex]
				if statuses[stateIndex] == StatusCompleted && step.Compensate != nil {
					completed = append(completed, step)
				}
			}
		}
	}
	return completed
}

func (rw *RecoverableWorkflow) runStepTracking(
	ctx context.Context,
	step Step,
	stepIndex int,
	completed *[]Step,
	completedMu *sync.Mutex,
) error {
	if err := executeStep(ctx, step); err != nil {
		return err
	}

	if step.Compensate != nil {
		completedMu.Lock()
		*completed = append(*completed, step)
		completedMu.Unlock()
	}

	if err := rw.updateAndSave(func(state *SagaState) {
		if stepIndex < len(state.Steps) {
			state.Steps[stepIndex].Status = StatusCompleted
		}
	}); err != nil {
		return fmt.Errorf("saga %q persist step %q: %w", rw.id, step.Name, err)
	}
	return nil
}

func (rw *RecoverableWorkflow) runGroupTracking(
	ctx context.Context,
	group Group,
	indexes []int,
	completed *[]Step,
	completedMu *sync.Mutex,
) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(group))

	for index, step := range group {
		wg.Add(1)
		go func(s Step, stateIndex int) {
			defer wg.Done()
			if err := rw.runStepTracking(ctx, s, stateIndex, completed, completedMu); err != nil {
				errChan <- err
			}
		}(step, indexes[index])
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

func (rw *RecoverableWorkflow) rollback(ctx context.Context, triggerErr error, completed []Step) error {
	rollbackCtx := context.WithoutCancel(ctx)
	var errs []error
	errs = append(errs, triggerErr)

	for i := len(completed) - 1; i >= 0; i-- {
		step := completed[i]
		if err := rw.safeCompensate(rollbackCtx, step); err != nil {
			errs = append(errs, fmt.Errorf("rollback failed for '%s': %w", step.Name, err))
			continue
		}
		if err := rw.updateAndSave(func(state *SagaState) {
			for index := range state.Steps {
				if state.Steps[index].Name == step.Name {
					state.Steps[index].Status = StatusCompensated
					break
				}
			}
		}); err != nil {
			errs = append(errs, fmt.Errorf("persist compensation for %q: %w", step.Name, err))
		}
	}

	return joinErrors(errs)
}

func (rw *RecoverableWorkflow) updateAndSave(update func(*SagaState)) error {
	rw.stateMu.Lock()
	update(rw.state)
	state := cloneSagaState(rw.state)
	rw.stateMu.Unlock()
	return rw.store.Save(state)
}

func joinErrors(errs []error) error {
	return errors.Join(errs...)
}
