package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mirkobrombin/go-foundation/pkg/options"
)

// Job defines a scheduled job with a cron expression and handler.
type Job struct {
	Name    string
	Cron    string
	Handler func(ctx context.Context) error
}

// Scheduler runs registered jobs on cron schedules.
type Scheduler struct {
	jobs    []scheduledJob
	mu      sync.RWMutex
	running bool
	cancel  context.CancelFunc
	logger  func(msg string)
	metrics func(name string, dur time.Duration, err error)
}

type scheduledJob struct {
	job  Job
	last time.Time
}

// Option configures a Scheduler.
type Option = options.Option[Scheduler]

// New creates a Scheduler with the given options.
func New(opts ...Option) *Scheduler {
	s := &Scheduler{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithLogger sets the logger callback.
func WithLogger(log func(msg string)) Option {
	return func(s *Scheduler) { s.logger = log }
}

// WithMetrics sets the metrics callback.
func WithMetrics(m func(name string, dur time.Duration, err error)) Option {
	return func(s *Scheduler) { s.metrics = m }
}

func (s *Scheduler) log(msg string) {
	if s.logger != nil {
		s.logger(msg)
	}
}

func (s *Scheduler) metric(name string, dur time.Duration, err error) {
	if s.metrics != nil {
		s.metrics(name, dur, err)
	}
}

// Register adds a job to the scheduler.
func (s *Scheduler) Register(job Job) *Scheduler {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, scheduledJob{job: job})
	return s
}

// Start begins the scheduler loop.
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("scheduler: already running")
	}
	s.running = true
	ctx, s.cancel = context.WithCancel(ctx)
	s.mu.Unlock()

	s.log("scheduler: started")

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				s.mu.Lock()
				s.running = false
				s.mu.Unlock()
				s.log("scheduler: stopped")
				return
			case now := <-ticker.C:
				s.runDue(ctx, now)
			}
		}
	}()

	return nil
}

func (s *Scheduler) runDue(ctx context.Context, now time.Time) {
	s.mu.RLock()
	jobs := make([]scheduledJob, len(s.jobs))
	copy(jobs, s.jobs)
	s.mu.RUnlock()

	for i, sj := range jobs {
		if isDue(sj.job.Cron, now, sj.last) {
			go func(job Job, idx int) {
				start := time.Now()
				err := job.Handler(ctx)
				dur := time.Since(start)
				s.metric(job.Name, dur, err)
				if err != nil {
					s.log(fmt.Sprintf("scheduler: job %s error: %v", job.Name, err))
				}
			}(sj.job, i)

			s.mu.Lock()
			s.jobs[i].last = now
			s.mu.Unlock()
		}
	}
}

// Stop cancels the scheduler loop.
func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return fmt.Errorf("scheduler: not running")
	}
	s.cancel()
	s.running = false
	return nil
}

func isDue(cronExpr string, now, last time.Time) bool {
	fields, err := parseCron(cronExpr)
	if err != nil {
		return false
	}

	if last.IsZero() {
		return true
	}

	next := nextRun(fields, last)
	return !next.After(now)
}

type cronFields struct {
	minute, hour, dom, month, dow int
	wildMin, wildHour, wildDom, wildMonth, wildDow bool
}

func parseCron(expr string) (cronFields, error) {
	parts := splitFields(expr)
	if len(parts) != 5 {
		return cronFields{}, fmt.Errorf("scheduler: invalid cron expression: %s", expr)
	}

	f := cronFields{}
	f.minute, f.wildMin = parseField(parts[0], 0, 59)
	f.hour, f.wildHour = parseField(parts[1], 0, 23)
	f.dom, f.wildDom = parseField(parts[2], 1, 31)
	f.month, f.wildMonth = parseField(parts[3], 1, 12)
	f.dow, f.wildDow = parseField(parts[4], 0, 6)
	return f, nil
}

func splitFields(expr string) []string {
	var fields []string
	current := ""
	for _, ch := range expr {
		if ch == ' ' || ch == '\t' {
			if current != "" {
				fields = append(fields, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		fields = append(fields, current)
	}
	return fields
}

func parseField(s string, min, max int) (int, bool) {
	if s == "*" {
		return 0, true
	}
	var v int
	fmt.Sscanf(s, "%d", &v)
	return v, false
}

func nextRun(f cronFields, last time.Time) time.Time {
	// Simple approximation: add 1 minute and find the next match
	t := last.Add(time.Minute)
	for i := 0; i < 525600; i++ { // max 1 year of minutes
		if (f.wildMin || t.Minute() == f.minute) &&
			(f.wildHour || t.Hour() == f.hour) &&
			(f.wildDom || t.Day() == f.dom) &&
			(f.wildMonth || int(t.Month()) == f.month) &&
			(f.wildDow || int(t.Weekday()) == f.dow) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return t
}

// Enqueue runs a fire-and-forget function in a goroutine.
func (s *Scheduler) Enqueue(fn func(ctx context.Context) error) {
	s.log("scheduler: enqueued fire-and-forget job")
	go func() {
		start := time.Now()
		err := fn(context.Background())
		dur := time.Since(start)
		s.metric("enqueue", dur, err)
		if err != nil {
			s.log(fmt.Sprintf("scheduler: enqueue error: %v", err))
		}
	}()
}

// ScheduleAfter runs fn after the given duration.
func (s *Scheduler) ScheduleAfter(d time.Duration, fn func(ctx context.Context) error) {
	s.log(fmt.Sprintf("scheduler: scheduled job after %v", d))
	go func() {
		time.Sleep(d)
		start := time.Now()
		err := fn(context.Background())
		dur := time.Since(start)
		s.metric("schedule-after", dur, err)
		if err != nil {
			s.log(fmt.Sprintf("scheduler: schedule-after error: %v", err))
		}
	}()
}