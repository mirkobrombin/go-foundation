package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestJobStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJobStore(dir)
	if err != nil {
		t.Fatalf("NewJobStore: %v", err)
	}

	rec := &JobRecord{
		Name:        "cleanup",
		Cron:        "0 3 * * *",
		LastRun:     time.Date(2026, 1, 15, 3, 0, 0, 0, time.UTC),
		LastStatus:  "ok",
		LastLatency: "12ms",
	}
	if err := store.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load("cleanup")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Name != "cleanup" {
		t.Errorf("Name = %q, want %q", loaded.Name, "cleanup")
	}
	if loaded.Cron != "0 3 * * *" {
		t.Errorf("Cron = %q, want %q", loaded.Cron, "0 3 * * *")
	}
	if !loaded.LastRun.Equal(rec.LastRun) {
		t.Errorf("LastRun = %v, want %v", loaded.LastRun, rec.LastRun)
	}
}

func TestJobStore_List(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewJobStore(dir)

	store.Save(&JobRecord{Name: "job1", Cron: "* * * * *"})
	store.Save(&JobRecord{Name: "job2", Cron: "0 * * * *"})

	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("got %d jobs, want 2", len(list))
	}
}

func TestScheduler_WithStore(t *testing.T) {
	dir := t.TempDir()
	s := New(WithStore(dir))
	var count int64
	s.Register(Job{
		Name: "tick",
		Cron: "* * * * *",
		Handler: func(ctx context.Context) error {
			atomic.AddInt64(&count, 1)
			return nil
		},
	})
	s.runDue(context.Background(), time.Now())
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt64(&count) == 0 {
		t.Error("expected job to run at least once")
	}
}