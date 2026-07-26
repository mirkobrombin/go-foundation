package saga

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// Step defines a saga step with a do and compensate action.
type Step struct {
	Name       string
	Do         func(ctx context.Context) error
	Compensate func(ctx context.Context) error
}

// Group is a collection of steps executed in parallel.
type Group []Step

// Workflow orchestrates a saga with rollback support.
type Workflow struct {
	steps   []any
	running atomic.Bool
}

// New creates an empty Workflow.
func New() *Workflow {
	return &Workflow{}
}

// Add appends a step to the workflow.
func (w *Workflow) Add(name string, do, compensate func(ctx context.Context) error) {
	w.steps = append(w.steps, Step{
		Name:       name,
		Do:         do,
		Compensate: compensate,
	})
}

// AddGroup appends a parallel step group to the workflow.
func (w *Workflow) AddGroup(g Group) {
	w.steps = append(w.steps, g)
}

// Run executes all steps in order, rolling back on failure.
func (w *Workflow) Run(ctx context.Context) error {
	if !w.running.CompareAndSwap(false, true) {
		return errors.New("saga: workflow is already running")
	}
	defer w.running.Store(false)

	var completed []Step
	var completedMu sync.Mutex
	for _, item := range w.steps {
		if ctx.Err() != nil {
			return w.rollback(ctx, ctx.Err(), completed)
		}

		var err error
		switch v := item.(type) {
		case Step:
			err = w.runStep(ctx, v, &completed, &completedMu)
		case Group:
			err = w.runGroup(ctx, v, &completed, &completedMu)
		}

		if err != nil {
			return w.rollback(ctx, err, completed)
		}
	}
	return nil
}

func (w *Workflow) runStep(
	ctx context.Context,
	step Step,
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
	return nil
}

func executeStep(ctx context.Context, step Step) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in step '%s': %v", step.Name, r)
		}
	}()

	if err := step.Do(ctx); err != nil {
		return fmt.Errorf("step '%s' failed: %w", step.Name, err)
	}
	return nil
}

func (w *Workflow) runGroup(
	ctx context.Context,
	group Group,
	completed *[]Step,
	completedMu *sync.Mutex,
) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(group))

	for _, step := range group {
		wg.Add(1)
		go func(s Step) {
			defer wg.Done()
			if err := w.runStep(ctx, s, completed, completedMu); err != nil {
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
		return errors.Join(errs...)
	}
	return nil
}

func (w *Workflow) rollback(ctx context.Context, triggerErr error, completed []Step) error {
	rollbackCtx := context.WithoutCancel(ctx)
	var errs []error
	errs = append(errs, triggerErr)

	for i := len(completed) - 1; i >= 0; i-- {
		step := completed[i]
		if err := w.safeCompensate(rollbackCtx, step); err != nil {
			errs = append(errs, fmt.Errorf("rollback failed for '%s': %w", step.Name, err))
		}
	}

	return errors.Join(errs...)
}

func (w *Workflow) safeCompensate(ctx context.Context, step Step) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic during compensation: %v", r)
		}
	}()
	return step.Compensate(ctx)
}
