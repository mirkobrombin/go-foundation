package saga

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestWorkflow_Run_AllStepsSucceed(t *testing.T) {
	var executed []string
	wf := New()
	wf.Add("step1",
		func(ctx context.Context) error { executed = append(executed, "step1"); return nil },
		func(ctx context.Context) error { executed = append(executed, "undo1"); return nil },
	)
	wf.Add("step2",
		func(ctx context.Context) error { executed = append(executed, "step2"); return nil },
		func(ctx context.Context) error { executed = append(executed, "undo2"); return nil },
	)

	err := wf.Run(context.Background())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(executed) != 2 {
		t.Fatalf("expected 2 steps executed, got %v", executed)
	}
	if executed[0] != "step1" || executed[1] != "step2" {
		t.Errorf("unexpected order: %v", executed)
	}
}

func TestWorkflow_Run_StepFails_Compensates(t *testing.T) {
	var executed []string
	wf := New()
	wf.Add("step1",
		func(ctx context.Context) error { executed = append(executed, "step1"); return nil },
		func(ctx context.Context) error { executed = append(executed, "undo1"); return nil },
	)
	wf.Add("step2",
		func(ctx context.Context) error { executed = append(executed, "step2"); return errors.New("fail") },
		func(ctx context.Context) error { executed = append(executed, "undo2"); return nil },
	)
	wf.Add("step3",
		func(ctx context.Context) error { executed = append(executed, "step3"); return nil },
		func(ctx context.Context) error { executed = append(executed, "undo3"); return nil },
	)

	err := wf.Run(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	expect := []string{"step1", "step2", "undo1"}
	if len(executed) != len(expect) {
		t.Fatalf("expected %v, got %v", expect, executed)
	}
	for i, v := range expect {
		if executed[i] != v {
			t.Errorf("executed[%d] = %q, want %q", i, executed[i], v)
		}
	}
}

func TestWorkflow_Run_ContextCancelled(t *testing.T) {
	var executed []string
	ctx, cancel := context.WithCancel(context.Background())

	wf := New()
	wf.Add("step1",
		func(ctx context.Context) error { executed = append(executed, "step1"); return nil },
		func(ctx context.Context) error { executed = append(executed, "undo1"); return nil },
	)
	wf.Add("step2",
		func(ctx context.Context) error {
			cancel()
			return ctx.Err()
		},
		func(ctx context.Context) error { return nil },
	)

	err := wf.Run(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if len(executed) != 2 {
		t.Fatalf("expected step1 and step2 executed, got %v", executed)
	}
}

func TestWorkflow_Run_PanicInStep(t *testing.T) {
	var executed []string
	wf := New()
	wf.Add("step1",
		func(ctx context.Context) error { executed = append(executed, "step1"); return nil },
		func(ctx context.Context) error { executed = append(executed, "undo1"); return nil },
	)
	wf.Add("panic-step",
		func(ctx context.Context) error {
			panic("something went wrong")
		},
		func(ctx context.Context) error { return nil },
	)

	err := wf.Run(context.Background())
	if err == nil {
		t.Fatal("expected error from panic")
	}
	expect := []string{"step1", "undo1"}
	if len(executed) != len(expect) {
		t.Fatalf("expected %v, got %v", expect, executed)
	}
	for i, v := range expect {
		if executed[i] != v {
			t.Errorf("executed[%d] = %q, want %q", i, executed[i], v)
		}
	}
}

func TestWorkflow_Run_GroupParallel(t *testing.T) {
	var mu sync.Mutex
	var executed []string
	wf := New()

	group := Group{
		{Name: "g1", Do: func(ctx context.Context) error { mu.Lock(); executed = append(executed, "g1"); mu.Unlock(); return nil },
			Compensate: func(ctx context.Context) error { return nil }},
		{Name: "g2", Do: func(ctx context.Context) error { mu.Lock(); executed = append(executed, "g2"); mu.Unlock(); return nil },
			Compensate: func(ctx context.Context) error { return nil }},
	}
	wf.AddGroup(group)

	err := wf.Run(context.Background())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(executed) != 2 {
		t.Errorf("expected 2 group steps, got %v", executed)
	}
}

func TestWorkflow_Run_CompensatePanicSafe(t *testing.T) {
	wf := New()
	wf.Add("step1",
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error { panic("compensate panic") },
	)
	wf.Add("step2",
		func(ctx context.Context) error { return errors.New("fail") },
		nil,
	)

	err := wf.Run(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWorkflow_Run_NoCompensateOnSuccess(t *testing.T) {
	var compensated bool
	wf := New()
	wf.Add("step1",
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error { compensated = true; return nil },
	)

	err := wf.Run(context.Background())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if compensated {
		t.Error("expected no compensation on success")
	}
}

func TestWorkflowDoesNotReuseCompensationsAcrossRuns(t *testing.T) {
	workflow := New()
	var fail bool
	var compensations int
	workflow.Add(
		"first",
		func(context.Context) error { return nil },
		func(context.Context) error {
			compensations++
			return nil
		},
	)
	workflow.Add(
		"second",
		func(context.Context) error {
			if fail {
				return errors.New("failed")
			}
			return nil
		},
		nil,
	)

	if err := workflow.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	fail = true
	if err := workflow.Run(context.Background()); err == nil {
		t.Fatal("second Run() succeeded")
	}
	if compensations != 1 {
		t.Fatalf("compensations = %d, want 1 for the failing run", compensations)
	}
}

func TestWorkflowCompensationCanReenterRun(t *testing.T) {
	workflow := New()
	reentered := make(chan error, 1)
	workflow.Add(
		"first",
		func(context.Context) error { return nil },
		func(context.Context) error {
			reentered <- workflow.Run(context.Background())
			return nil
		},
	)
	workflow.Add(
		"failure",
		func(context.Context) error { return errors.New("failed") },
		nil,
	)

	done := make(chan error, 1)
	go func() {
		done <- workflow.Run(context.Background())
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run() succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("compensation deadlocked while reentering the workflow")
	}
	if err := <-reentered; err == nil {
		t.Fatal("reentrant Run() was accepted")
	}
}

func TestWorkflow_Compensate_WithRetry(t *testing.T) {
	attempts := 0
	do := WithRetry(RetryPolicy{MaxAttempts: 3, Delay: 10 * time.Millisecond, Multiplier: 1.0},
		func(ctx context.Context) error {
			attempts++
			return nil
		},
	)

	err := do(context.Background())
	if err != nil {
		t.Fatalf("WithRetry failed: %v", err)
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt on success, got %d", attempts)
	}
}

func TestWorkflow_Compensate_WithRetryExhausted(t *testing.T) {
	attempts := 0
	do := WithRetry(RetryPolicy{MaxAttempts: 3, Delay: 10 * time.Millisecond, Multiplier: 1.0},
		func(ctx context.Context) error {
			attempts++
			return errors.New("always fail")
		},
	)

	err := do(context.Background())
	if err == nil {
		t.Fatal("expected error after retry exhausted")
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}
