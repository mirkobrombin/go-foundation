package worker

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// Task is a work function executed by the pool.
type Task func(context.Context) error

// Future holds the result of an asynchronous task.
type Future struct {
	ch chan Result
}

// Result holds the outcome of a completed Task.
type Result struct {
	Err error
}

func (f *Future) Wait() error {
	r := <-f.ch
	return r.Err
}

// Pool is a fixed-size worker pool for concurrent task execution.
type Pool struct {
	wg     sync.WaitGroup
	tasks  chan work
	done   chan struct{}
	cancel context.CancelFunc
	ctx    context.Context
	once   sync.Once
	closed atomic.Bool
}

type work struct {
	task     Task
	accepted chan bool
}

// NewPool creates a fixed-size worker pool of n goroutines.
//
// Example:
//
//	pool := worker.NewPool(4)
//	defer pool.Shutdown()
//	pool.Submit(func(ctx context.Context) error {
//		return doWork(ctx)
//	})
func NewPool(n int) *Pool {
	if n <= 0 {
		n = 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &Pool{
		tasks:  make(chan work),
		done:   make(chan struct{}),
		cancel: cancel,
		ctx:    ctx,
	}
	for i := 0; i < n; i++ {
		p.wg.Add(1)
		go p.worker()
	}
	return p
}

// worker pulls tasks from the tasks channel until done is closed.
func (p *Pool) worker() {
	defer p.wg.Done()
	for {
		select {
		case item := <-p.tasks:
			if p.closed.Load() {
				item.accepted <- false
				continue
			}
			item.accepted <- true
			_ = runTask(p.ctx, item.task)
		case <-p.done:
			return
		}
	}
}

func runTask(ctx context.Context, task Task) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("worker: task panic: %v", recovered)
		}
	}()
	return task(ctx)
}

// Submit enqueues a task for execution. Returns false if the pool is shut down
// or shutting down; the task is not executed in that case.
func (p *Pool) Submit(task Task) bool {
	if task == nil || p.closed.Load() {
		return false
	}
	item := work{
		task:     task,
		accepted: make(chan bool, 1),
	}
	select {
	case p.tasks <- item:
		return <-item.accepted
	case <-p.done:
		return false
	}
}

// Shutdown stops accepting new tasks and waits for in-progress work to finish.
// The task channel is unbuffered, so no tasks can be pending pickup when done
// is closed; any Submit in progress will observe done and return false.
func (p *Pool) Shutdown() {
	p.once.Do(func() {
		p.closed.Store(true)
		p.cancel()
		close(p.done)
		p.wg.Wait()
	})
}
