package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mirkobrombin/go-foundation/pkg/options"
)

// Job defines a scheduled task with a cron expression and handler.
type Job struct {
	Name    string
	Cron    string
	Handler func(ctx context.Context) error
}

// Scheduler runs jobs according to cron expressions.
type Scheduler struct {
	jobs    []scheduledJob
	mu      sync.RWMutex
	running bool
	cancel  context.CancelFunc
	logger  func(msg string)
	metrics func(name string, dur time.Duration, err error)
	store   *JobStore
}

type scheduledJob struct {
	job  Job
	last time.Time
}

// Option configures a Scheduler.
type Option = options.Option[Scheduler]

// New creates a new Scheduler with the given options.
func New(opts ...Option) *Scheduler {
	s := &Scheduler{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithLogger sets a logging callback for the scheduler.
func WithLogger(log func(msg string)) Option {
	return func(s *Scheduler) { s.logger = log }
}

// WithMetrics sets a metrics callback for job executions.
func WithMetrics(m func(name string, dur time.Duration, err error)) Option {
	return func(s *Scheduler) { s.metrics = m }
}

// WithStore enables persistent job state storage to the given directory.
func WithStore(dir string) Option {
	return func(s *Scheduler) {
		store, err := NewJobStore(dir)
		if err != nil {
			return
		}
		s.store = store
	}
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

func (s *Scheduler) Register(job Job) *Scheduler {
	s.mu.Lock()
	defer s.mu.Unlock()

	var last time.Time
	if s.store != nil {
		if rec, err := s.store.Load(job.Name); err == nil && !rec.LastRun.IsZero() {
			last = rec.LastRun
		}
	}

	s.jobs = append(s.jobs, scheduledJob{job: job, last: last})
	return s
}

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
			go func(job Job, idx int, t time.Time) {
				start := time.Now()
				err := job.Handler(ctx)
				dur := time.Since(start)
				s.metric(job.Name, dur, err)
				if err != nil {
					s.log(fmt.Sprintf("scheduler: job %s error: %v", job.Name, err))
				}

				if s.store != nil {
					_ = s.store.Save(&JobRecord{
						Name:        job.Name,
						Cron:        job.Cron,
						LastRun:     start,
						LastStatus:  "ok",
						LastLatency: dur.String(),
					})
				}
			}(sj.job, i, now)

			s.mu.Lock()
			s.jobs[i].last = now
			s.mu.Unlock()
		}
	}
}

func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return fmt.Errorf("scheduler: not running")
	}
	s.cancel()
	s.running = false

	if s.store != nil {
		for _, sj := range s.jobs {
			_ = s.store.Save(&JobRecord{
				Name:    sj.job.Name,
				Cron:    sj.job.Cron,
				LastRun: sj.last,
			})
		}
	}
	return nil
}

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

// JobRecord holds persisted job execution state.
type JobRecord struct {
	Name        string    `json:"name"`
	Cron        string    `json:"cron"`
	LastRun     time.Time `json:"last_run"`
	LastStatus  string    `json:"last_status,omitempty"`
	LastLatency string    `json:"last_latency,omitempty"`
}

// JobStore persists job state to disk as JSON.
type JobStore struct {
	dir string
	mu  sync.RWMutex
}

// NewJobStore creates a JobStore that writes to the given directory.
func NewJobStore(dir string) (*JobStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("scheduler store: cannot create dir %s: %w", dir, err)
	}
	return &JobStore{dir: dir}, nil
}

func (js *JobStore) Save(rec *JobRecord) error {
	js.mu.Lock()
	defer js.mu.Unlock()

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("scheduler store: marshal: %w", err)
	}

	path := filepath.Join(js.dir, rec.Name+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("scheduler store: write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("scheduler store: rename: %w", err)
	}
	return nil
}

func (js *JobStore) Load(name string) (*JobRecord, error) {
	js.mu.RLock()
	defer js.mu.RUnlock()

	path := filepath.Join(js.dir, name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("scheduler store: read %s: %w", name, err)
	}
	var rec JobRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("scheduler store: unmarshal %s: %w", name, err)
	}
	return &rec, nil
}

func (js *JobStore) List() ([]*JobRecord, error) {
	js.mu.RLock()
	defer js.mu.RUnlock()

	entries, err := os.ReadDir(js.dir)
	if err != nil {
		return nil, fmt.Errorf("scheduler store: readdir: %w", err)
	}

	var result []*JobRecord
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(js.dir, entry.Name()))
		if err != nil {
			continue
		}
		var rec JobRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			continue
		}
		result = append(result, &rec)
	}
	return result, nil
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
	t := last.Add(time.Minute)
	for i := 0; i < 525600; i++ {
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