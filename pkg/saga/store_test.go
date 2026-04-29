package saga

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

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

// --- M5: MemoryStore, Idempotency, DLQ tests ---

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

func TestSagaStore_Interface(t *testing.T) {
	var _ SagaStore = NewMemoryStore()
	var _ SagaStore = &FileStore{}
	dir := t.TempDir()
	fs, _ := NewStore(dir)
	var _ SagaStore = fs
}