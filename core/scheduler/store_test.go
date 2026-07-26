package scheduler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestJobStoreRejectsTraversalAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJobStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(&JobRecord{Name: "../../escape"}); err == nil {
		t.Fatal("Save() accepted path traversal")
	}
	if _, err := store.Load("../../escape"); err == nil {
		t.Fatal("Load() accepted path traversal")
	}

	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte(`{"name":"outside"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linked.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("linked"); err == nil {
		t.Fatal("Load() followed a symbolic link")
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("List() returned symlink record: %v", list)
	}
}

func TestJobStoreUsesPrivateModeAndSizeLimit(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJobStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(&JobRecord{Name: "private"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "private.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("file mode = %o, want 600", info.Mode().Perm())
	}
	if err := store.Save(&JobRecord{
		Name: "large",
		Cron: string(make([]byte, maxJobRecordSize+1)),
	}); err == nil {
		t.Fatal("Save() accepted an oversized record")
	}
}

func TestScheduler_WithStore(t *testing.T) {
	dir := t.TempDir()
	s := New(WithStore(dir))
	var count int64
	if err := s.Register(Job{
		Name: "tick",
		Cron: "* * * * *",
		Handler: func(ctx context.Context) error {
			atomic.AddInt64(&count, 1)
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	s.runDue(context.Background(), time.Now())
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt64(&count) == 0 {
		t.Error("expected job to run at least once")
	}
}

func TestSchedulerRegisterSurfacesCorruptState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "job.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	scheduler := New(WithStore(dir))
	err := scheduler.Register(Job{
		Name:    "job",
		Cron:    "* * * * *",
		Handler: func(context.Context) error { return nil },
	})
	if err == nil {
		t.Fatal("Register() ignored corrupt persisted state")
	}
}

func TestSchedulerPersistsHandlerErrorStatus(t *testing.T) {
	dir := t.TempDir()
	scheduler := New(WithStore(dir))
	if err := scheduler.Register(Job{
		Name:    "failing",
		Cron:    "* * * * *",
		Handler: func(context.Context) error { return errors.New("failed") },
	}); err != nil {
		t.Fatal(err)
	}
	scheduler.runDue(context.Background(), time.Now())

	deadline := time.Now().Add(time.Second)
	for {
		record, err := scheduler.store.Load("failing")
		if err == nil {
			if record.LastStatus != "error" {
				t.Fatalf("LastStatus = %q, want error", record.LastStatus)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job state was not persisted: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSchedulerLogsPersistenceFailure(t *testing.T) {
	dir := t.TempDir()
	messages := make(chan string, 10)
	scheduler := New(WithStore(dir), WithLogger(func(message string) {
		messages <- message
	}))
	if err := scheduler.Register(Job{
		Name:    "job",
		Cron:    "* * * * *",
		Handler: func(context.Context) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	scheduler.runDue(context.Background(), time.Now())
	timeout := time.After(time.Second)
	for {
		select {
		case message := <-messages:
			if strings.Contains(message, "persist job job state") {
				if scheduler.Err() == nil {
					t.Fatal("persistence failure was not exposed by Err()")
				}
				return
			}
		case <-timeout:
			t.Fatal("persistence failure was not logged")
		}
	}
}
