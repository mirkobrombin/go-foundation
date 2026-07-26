package saga

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type failingSagaStore struct {
	state   *SagaState
	saveErr error
}

func (s *failingSagaStore) Save(*SagaState) error {
	return s.saveErr
}

func (s *failingSagaStore) Load(string) (*SagaState, error) {
	if s.state == nil {
		return nil, ErrSagaNotFound
	}
	return cloneSagaState(s.state), nil
}

func (s *failingSagaStore) Delete(string) error {
	return nil
}

func (s *failingSagaStore) ListIncomplete() ([]*SagaState, error) {
	return nil, nil
}

func TestStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	state := &SagaState{
		ID:     "test-1",
		Status: StatusPending,
		Steps: []StepState{
			{Name: "reserve", Status: StatusCompleted, StepIndex: 0},
			{Name: "charge", Status: StatusPending, StepIndex: 1},
		},
	}
	if err := store.Save(state); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load("test-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.ID != "test-1" {
		t.Errorf("ID = %q, want %q", loaded.ID, "test-1")
	}
	if len(loaded.Steps) != 2 {
		t.Errorf("len(Steps) = %d, want 2", len(loaded.Steps))
	}
	if loaded.Steps[0].Status != StatusCompleted {
		t.Errorf("step[0] status = %q, want %q", loaded.Steps[0].Status, StatusCompleted)
	}
}

func TestStore_Delete(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	state := &SagaState{ID: "del-me", Status: StatusPending}
	store.Save(state)

	if err := store.Delete("del-me"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := store.Load("del-me")
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestStore_ListIncomplete(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	store.Save(&SagaState{ID: "s1", Status: StatusPending})
	store.Save(&SagaState{ID: "s2", Status: StatusCompleted})
	store.Save(&SagaState{ID: "s3", Status: StatusFailed})

	list, err := store.ListIncomplete()
	if err != nil {
		t.Fatalf("ListIncomplete: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("got %d incomplete, want 2", len(list))
	}
}

func TestStoreListIncompleteReportsCorruptFiles(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(&SagaState{ID: "valid", Status: StatusPending}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}

	states, err := store.ListIncomplete()
	if err == nil {
		t.Fatal("ListIncomplete() ignored a corrupt state file")
	}
	if len(states) != 1 || states[0].ID != "valid" {
		t.Fatalf("ListIncomplete() states = %v, want valid state", states)
	}
}

func TestStoreRejectsTraversalAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(&SagaState{ID: "../../escape"}); err == nil {
		t.Fatal("Save() accepted path traversal")
	}
	if _, err := store.Load("../../escape"); err == nil {
		t.Fatal("Load() accepted path traversal")
	}
	if err := store.Delete("../../escape"); err == nil {
		t.Fatal("Delete() accepted path traversal")
	}

	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte(`{"id":"outside"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linked.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("linked"); err == nil {
		t.Fatal("Load() followed a symbolic link")
	}
	list, err := store.ListIncomplete()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("ListIncomplete() returned symlink state: %v", list)
	}
}

func TestStoreUsesPrivateModesAndSizeLimit(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(&SagaState{ID: "private"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "private.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("file mode = %o, want 600", info.Mode().Perm())
	}
	if err := store.Save(&SagaState{
		ID:    "large",
		Error: string(make([]byte, maxSagaStateSize+1)),
	}); err == nil {
		t.Fatal("Save() accepted an oversized state")
	}
}

func TestRecoverableWorkflow_AllStepsSucceed(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	var executed []string
	rw := NewRecoverable("order-1", store)
	rw.Add("reserve",
		func(ctx context.Context) error { executed = append(executed, "reserve"); return nil },
		func(ctx context.Context) error { executed = append(executed, "undo-reserve"); return nil },
	)
	rw.Add("charge",
		func(ctx context.Context) error { executed = append(executed, "charge"); return nil },
		func(ctx context.Context) error { executed = append(executed, "undo-charge"); return nil },
	)

	err := rw.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(executed) != 2 {
		t.Errorf("executed %v, want 2 steps", executed)
	}

	state, _ := store.Load("order-1")
	if state.Status != StatusCompleted {
		t.Errorf("status = %q, want %q", state.Status, StatusCompleted)
	}
}

func TestRecoverableWorkflow_StepFails_Persists(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	rw := NewRecoverable("order-2", store)
	rw.Add("reserve",
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error { return nil },
	)
	rw.Add("charge",
		func(ctx context.Context) error { return errors.New("card declined") },
		func(ctx context.Context) error { return nil },
	)

	err := rw.Run(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}

	state, _ := store.Load("order-2")
	if state.Status != StatusFailed {
		t.Errorf("status = %q, want %q", state.Status, StatusFailed)
	}
	if state.Steps[0].Status != StatusCompensated {
		t.Errorf("step[0] = %q, want %q", state.Steps[0].Status, StatusCompensated)
	}
}

func TestRecoverableWorkflow_CrashRecovery(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	rw := NewRecoverable("order-3", store)
	rw.Add("reserve",
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error { return nil },
	)
	rw.Add("charge",
		func(ctx context.Context) error { return errors.New("crash") },
		func(ctx context.Context) error { return nil },
	)

	rw.Run(context.Background())

	incomplete, err := store.ListIncomplete()
	if err != nil {
		t.Fatalf("ListIncomplete: %v", err)
	}
	if len(incomplete) != 1 {
		t.Fatalf("got %d incomplete sagas, want 1", len(incomplete))
	}
	if incomplete[0].ID != "order-3" {
		t.Errorf("ID = %q, want %q", incomplete[0].ID, "order-3")
	}

	_ = filepath.Join(dir, "order-3.json")
	data, err := os.ReadFile(filepath.Join(dir, "order-3.json"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	t.Logf("persisted state: %s", string(data))
}

func TestRecoverableWorkflowSkipsPersistedCompletedSteps(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Save(&SagaState{
		ID:     "recovery",
		Status: StatusPending,
		Steps: []StepState{
			{Name: "completed", Status: StatusCompleted, StepIndex: 0},
			{Name: "pending", Status: StatusPending, StepIndex: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	var completedCalls atomic.Int32
	var pendingCalls atomic.Int32
	workflow := NewRecoverable("recovery", store)
	workflow.Add("completed", func(context.Context) error {
		completedCalls.Add(1)
		return nil
	}, nil)
	workflow.Add("pending", func(context.Context) error {
		pendingCalls.Add(1)
		return nil
	}, nil)

	if err := workflow.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if completedCalls.Load() != 0 {
		t.Fatal("Run() repeated a persisted completed step")
	}
	if pendingCalls.Load() != 1 {
		t.Fatalf("pending step calls = %d, want 1", pendingCalls.Load())
	}
	state, err := store.Load("recovery")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != StatusCompleted {
		t.Fatalf("recovered saga status = %s, want completed", state.Status)
	}
}

func TestRecoverableWorkflowCompensatesPersistedCompletedSteps(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Save(&SagaState{
		ID:     "rollback-recovery",
		Status: StatusPending,
		Steps: []StepState{
			{Name: "completed", Status: StatusCompleted, StepIndex: 0},
			{Name: "failure", Status: StatusPending, StepIndex: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	var compensationCalls atomic.Int32
	workflow := NewRecoverable("rollback-recovery", store)
	workflow.Add("completed", func(context.Context) error {
		t.Fatal("persisted completed step was executed again")
		return nil
	}, func(context.Context) error {
		compensationCalls.Add(1)
		return nil
	})
	workflow.Add("failure", func(context.Context) error {
		return errors.New("failed")
	}, nil)

	if err := workflow.Run(context.Background()); err == nil {
		t.Fatal("Run() succeeded")
	}
	if compensationCalls.Load() != 1 {
		t.Fatalf("compensation calls = %d, want 1", compensationCalls.Load())
	}
	state, err := store.Load("rollback-recovery")
	if err != nil {
		t.Fatal(err)
	}
	if state.Steps[0].Status != StatusCompensated {
		t.Fatalf("persisted step status = %s, want compensated", state.Steps[0].Status)
	}
}

func TestMemoryStore_CRUD(t *testing.T) {
	store := NewMemoryStore()

	state := &SagaState{ID: "m1", Status: StatusPending}
	if err := store.Save(state); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load("m1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.ID != "m1" {
		t.Errorf("ID = %q, want m1", loaded.ID)
	}

	store.Save(&SagaState{ID: "m2", Status: StatusCompleted})
	incomplete, _ := store.ListIncomplete()
	if len(incomplete) != 1 {
		t.Errorf("incomplete = %d, want 1", len(incomplete))
	}

	store.Delete("m1")
	_, err = store.Load("m1")
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestIdempotencyRecorder(t *testing.T) {
	rec := NewIdempotencyRecorder()

	state1 := &SagaState{ID: "s1", Status: StatusCompleted}
	if err := rec.Record("key-1", state1); err != nil {
		t.Fatalf("Record: %v", err)
	}

	err := rec.Record("key-1", &SagaState{ID: "s2"})
	if err == nil {
		t.Fatal("expected idempotency error")
	}
	var idErr *IdempotencyError
	if !errors.As(err, &idErr) {
		t.Errorf("error type = %T, want *IdempotencyError", err)
	}

	got, ok := rec.Get("key-1")
	if !ok {
		t.Fatal("expected key found")
	}
	if got.ID != "s1" {
		t.Errorf("got ID = %q, want s1", got.ID)
	}

	_, ok = rec.Get("missing")
	if ok {
		t.Error("expected missing key not found")
	}
}

func TestDeadLetterQueue(t *testing.T) {
	dlq := NewDeadLetterQueue()

	state := &SagaState{ID: "dead-1", Status: StatusFailed, RetryCount: 3}
	dlq.Enqueue(state, "max retries exceeded")

	if dlq.Len() != 1 {
		t.Errorf("Len = %d, want 1", dlq.Len())
	}

	entries := dlq.List()
	if len(entries) != 1 {
		t.Fatalf("List len = %d, want 1", len(entries))
	}
	if entries[0].State.ID != "dead-1" {
		t.Errorf("entry ID = %q, want dead-1", entries[0].State.ID)
	}
	if entries[0].Reason != "max retries exceeded" {
		t.Errorf("reason = %q, want max retries exceeded", entries[0].Reason)
	}

	dlq.Remove("dead-1")
	if dlq.Len() != 0 {
		t.Errorf("Len after remove = %d, want 0", dlq.Len())
	}
}

func TestProcessWithDLQ_ExceedsRetries(t *testing.T) {
	store := NewMemoryStore()
	dlq := NewDeadLetterQueue()

	state := &SagaState{ID: "dlq-1", Status: StatusFailed, RetryCount: 3, MaxRetries: 3}
	store.Save(state)

	processor := ProcessWithDLQ(store, dlq, 3)
	run := processor("dlq-1", func(ctx context.Context) error {
		return errors.New("still failing")
	})

	err := run(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}

	updated, _ := store.Load("dlq-1")
	if updated.Status != StatusDead {
		t.Errorf("status = %q, want %q", updated.Status, StatusDead)
	}
	if dlq.Len() != 1 {
		t.Errorf("dlq len = %d, want 1", dlq.Len())
	}
}

func TestMemoryStoresCloneAndEnforceCapacity(t *testing.T) {
	store := NewMemoryStore(1)
	state := &SagaState{
		ID:    "one",
		Steps: []StepState{{Name: "step", Status: StatusPending}},
	}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	state.Steps[0].Status = StatusCompleted
	loaded, err := store.Load("one")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Steps[0].Status != StatusPending {
		t.Fatal("MemoryStore retained the caller's slice")
	}
	loaded.Steps[0].Status = StatusCompleted
	again, _ := store.Load("one")
	if again.Steps[0].Status != StatusPending {
		t.Fatal("MemoryStore exposed its internal slice")
	}
	if err := store.Save(&SagaState{ID: "two"}); err == nil {
		t.Fatal("MemoryStore exceeded its capacity")
	}

	recorder := NewIdempotencyRecorder(1)
	if err := recorder.Record("one", state); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Record("two", &SagaState{ID: "two"}); err == nil {
		t.Fatal("IdempotencyRecorder exceeded its capacity")
	}

	queue := NewDeadLetterQueue(1)
	if err := queue.Enqueue(state, "failed"); err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(&SagaState{ID: "two"}, "failed"); err == nil {
		t.Fatal("DeadLetterQueue exceeded its capacity")
	}
}

func TestProcessWithDLQSurfacesSaveFailure(t *testing.T) {
	saveErr := errors.New("save failed")
	store := &failingSagaStore{
		state:   &SagaState{ID: "one", Status: StatusPending},
		saveErr: saveErr,
	}
	run := ProcessWithDLQ(store, NewDeadLetterQueue(), 3)(
		"one",
		func(context.Context) error { return nil },
	)
	if err := run(context.Background()); !errors.Is(err, saveErr) {
		t.Fatalf("ProcessWithDLQ() error = %v, want save error", err)
	}
}

func TestRecoverableWorkflowSurfacesSaveFailure(t *testing.T) {
	saveErr := errors.New("save failed")
	workflow := NewRecoverable("one", &failingSagaStore{saveErr: saveErr})
	if err := workflow.Run(context.Background()); !errors.Is(err, saveErr) {
		t.Fatalf("Run() error = %v, want save error", err)
	}
}

func TestProcessWithDLQ_RetrySucceeds(t *testing.T) {
	store := NewMemoryStore()
	dlq := NewDeadLetterQueue()

	state := &SagaState{ID: "retry-1", Status: StatusFailed, RetryCount: 1, MaxRetries: 3}
	store.Save(state)

	processor := ProcessWithDLQ(store, dlq, 3)
	run := processor("retry-1", func(ctx context.Context) error {
		return nil
	})

	if err := run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, _ := store.Load("retry-1")
	if updated.Status != StatusCompleted {
		t.Errorf("status = %q, want %q", updated.Status, StatusCompleted)
	}
	if updated.RetryCount != 2 {
		t.Errorf("retry count = %d, want 2", updated.RetryCount)
	}
	if dlq.Len() != 0 {
		t.Errorf("dlq len = %d, want 0", dlq.Len())
	}
}

func TestProcessWithDLQSerializesSameSaga(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Save(&SagaState{ID: "shared", Status: StatusFailed}); err != nil {
		t.Fatal(err)
	}
	var active atomic.Int32
	var maximum atomic.Int32
	runFunc := func(context.Context) error {
		current := active.Add(1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		active.Add(-1)
		return nil
	}
	processor := ProcessWithDLQ(store, NewDeadLetterQueue(), 10)
	run := processor("shared", runFunc)

	var wait sync.WaitGroup
	wait.Add(2)
	for range 2 {
		go func() {
			defer wait.Done()
			_ = run(context.Background())
		}()
	}
	wait.Wait()

	if maximum.Load() != 1 {
		t.Fatalf("concurrent saga executions = %d, want 1", maximum.Load())
	}
	state, err := store.Load("shared")
	if err != nil {
		t.Fatal(err)
	}
	if state.RetryCount != 1 {
		t.Fatalf("retry count = %d, want 1", state.RetryCount)
	}
}

func TestRecoverableWorkflowRecoversStepPanic(t *testing.T) {
	store := NewMemoryStore()
	workflow := NewRecoverable("panic", store)
	workflow.Add("panic", func(context.Context) error {
		panic("step failed")
	}, nil)

	if err := workflow.Run(context.Background()); err == nil {
		t.Fatal("Run() accepted a step panic")
	}
	state, err := store.Load("panic")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != StatusFailed {
		t.Fatalf("state status = %s, want failed", state.Status)
	}
}

func TestSagaStore_Interface(t *testing.T) {
	var _ SagaStore = NewMemoryStore()
	var _ SagaStore = &FileStore{}
	dir := t.TempDir()
	fs, _ := NewStore(dir)
	var _ SagaStore = fs
}
