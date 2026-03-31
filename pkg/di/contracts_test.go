package di

import (
	"testing"

	"github.com/mirkobrombin/go-foundation/pkg/contracts"
)

type Worker interface {
	Work() string
}

type GoodWorker struct {
	contracts.Implements[Worker]
}

func (g *GoodWorker) Work() string {
	return "working hard"
}

type LazyWorker struct {
	contracts.Implements[Worker]
}

func (l *LazyWorker) Work() string {
	return "working smart"
}

type BrokenWorker struct {
	contracts.Implements[Worker]
}

// BrokenWorker does NOT implement Work()

func TestContainer_ProvideWithContracts(t *testing.T) {
	c := New()

	t.Run("Valid implementation", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Provide should not panic for valid worker: %v", r)
			}
		}()
		c.Provide("good", &GoodWorker{})
	})

	t.Run("Invalid implementation panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Provide should panic for broken worker")
			}
		}()
		c.Provide("broken", &BrokenWorker{})
	})
}

func TestResolveAll(t *testing.T) {
	c := New()
	c.Provide("good", &GoodWorker{})
	c.Provide("lazy", &LazyWorker{})
	c.Provide("other", "not a worker")

	workers := ResolveAll[Worker](c)
	if len(workers) != 2 {
		t.Fatalf("expected 2 workers, got %d", len(workers))
	}

	foundGood := false
	foundLazy := false
	for _, w := range workers {
		switch w.Work() {
		case "working hard":
			foundGood = true
		case "working smart":
			foundLazy = true
		}
	}

	if !foundGood || !foundLazy {
		t.Error("ResolveAll did not find all expected workers")
	}
}
