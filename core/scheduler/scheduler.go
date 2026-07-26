package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mirkobrombin/go-foundation/v2/core/options"
)

// Job defines a scheduled task with a cron expression and handler.
type Job struct {
	Name    string
	Cron    string
	Handler func(ctx context.Context) error
}

// Scheduler runs jobs according to cron expressions.
type Scheduler struct {
	jobs     []scheduledJob
	mu       sync.RWMutex
	running  bool
	cancel   context.CancelFunc
	logger   func(msg string)
	metrics  func(name string, dur time.Duration, err error)
	store    *JobStore
	storeErr error
	runErr   error
	runCtx   context.Context
	done     chan struct{}
	failure  chan error
	taskMu   sync.Mutex
	tasks    sync.WaitGroup
	stopping bool
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

// SetLogger replaces the scheduler logging callback.
func (s *Scheduler) SetLogger(log func(msg string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logger = log
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
			s.storeErr = err
			return
		}
		s.store = store
	}
}

func (s *Scheduler) log(msg string) {
	s.mu.RLock()
	logger := s.logger
	s.mu.RUnlock()
	if logger != nil {
		func() {
			defer func() {
				_ = recover()
			}()
			logger(msg)
		}()
	}
}

func (s *Scheduler) metric(name string, dur time.Duration, err error) {
	s.mu.RLock()
	metrics := s.metrics
	s.mu.RUnlock()
	if metrics != nil {
		func() {
			defer func() {
				_ = recover()
			}()
			metrics(name, dur, err)
		}()
	}
}

func (s *Scheduler) Register(job Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.storeErr != nil {
		return s.storeErr
	}
	if job.Name == "" {
		return errors.New("scheduler: job name cannot be empty")
	}
	if job.Handler == nil {
		return fmt.Errorf("scheduler: job %q handler cannot be nil", job.Name)
	}
	if _, err := parseCron(job.Cron); err != nil {
		return err
	}
	for _, existing := range s.jobs {
		if existing.job.Name == job.Name {
			return fmt.Errorf("scheduler: job %q is already registered", job.Name)
		}
	}

	var last time.Time
	if s.store != nil {
		rec, err := s.store.Load(job.Name)
		switch {
		case err == nil && !rec.LastRun.IsZero():
			last = rec.LastRun
		case err != nil && !errors.Is(err, os.ErrNotExist):
			return fmt.Errorf("scheduler: load job %q state: %w", job.Name, err)
		}
	}

	s.jobs = append(s.jobs, scheduledJob{job: job, last: last})
	return nil
}

func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.storeErr != nil {
		s.mu.Unlock()
		return s.storeErr
	}
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("scheduler: already running")
	}
	s.taskMu.Lock()
	if s.stopping && s.cancel != nil {
		s.taskMu.Unlock()
		s.mu.Unlock()
		return fmt.Errorf("scheduler: shutdown is still in progress")
	}
	s.stopping = false
	s.taskMu.Unlock()
	s.running = true
	s.runErr = nil
	ctx, s.cancel = context.WithCancel(ctx)
	s.runCtx = ctx
	s.done = make(chan struct{})
	s.failure = make(chan error, 1)
	done := s.done
	s.mu.Unlock()
	s.log("scheduler: started")

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		defer close(done)

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
			launched := s.launchTask(func() {
				start := time.Now()
				err := callJob(ctx, sj.job.Handler)
				dur := time.Since(start)
				s.metric(sj.job.Name, dur, err)
				if err != nil {
					s.log(fmt.Sprintf("scheduler: job %s error: %v", sj.job.Name, err))
				}

				if s.store != nil {
					status := "ok"
					if err != nil {
						status = "error"
					}
					if saveErr := s.store.Save(&JobRecord{
						Name:        sj.job.Name,
						Cron:        sj.job.Cron,
						LastRun:     start,
						LastStatus:  status,
						LastLatency: dur.String(),
					}); saveErr != nil {
						s.fail(fmt.Errorf("scheduler: persist job %s state: %w", sj.job.Name, saveErr))
					}
				}
			})

			if launched {
				s.mu.Lock()
				s.jobs[i].last = now
				s.mu.Unlock()
			}
		}
	}
}

func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	cancel := s.cancel
	done := s.done
	if cancel == nil {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	s.taskMu.Lock()
	s.stopping = true
	s.taskMu.Unlock()
	cancel()

	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	tasksDone := make(chan struct{})
	go func() {
		s.tasks.Wait()
		close(tasksDone)
	}()
	select {
	case <-tasksDone:
	case <-ctx.Done():
		return ctx.Err()
	}

	s.mu.Lock()
	s.cancel = nil
	s.runCtx = nil
	s.done = nil
	s.mu.Unlock()
	return nil
}

func (s *Scheduler) Enqueue(fn func(ctx context.Context) error) {
	if fn == nil {
		s.fail(errors.New("scheduler: enqueue handler cannot be nil"))
		return
	}
	s.log("scheduler: enqueued fire-and-forget job")
	ctx := s.taskContext()
	s.launchTask(func() {
		start := time.Now()
		err := callJob(ctx, fn)
		dur := time.Since(start)
		s.metric("enqueue", dur, err)
		if err != nil {
			s.log(fmt.Sprintf("scheduler: enqueue error: %v", err))
		}
	})
}

func (s *Scheduler) ScheduleAfter(d time.Duration, fn func(ctx context.Context) error) {
	if fn == nil {
		s.fail(errors.New("scheduler: schedule-after handler cannot be nil"))
		return
	}
	s.log(fmt.Sprintf("scheduler: scheduled job after %v", d))
	ctx := s.taskContext()
	s.launchTask(func() {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return
		}
		start := time.Now()
		err := callJob(ctx, fn)
		dur := time.Since(start)
		s.metric("schedule-after", dur, err)
		if err != nil {
			s.log(fmt.Sprintf("scheduler: schedule-after error: %v", err))
		}
	})
}

func (s *Scheduler) taskContext() context.Context {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.runCtx != nil {
		return s.runCtx
	}
	return context.Background()
}

func (s *Scheduler) launchTask(task func()) bool {
	s.taskMu.Lock()
	if s.stopping {
		s.taskMu.Unlock()
		return false
	}
	s.tasks.Add(1)
	s.taskMu.Unlock()
	go func() {
		defer s.tasks.Done()
		defer func() {
			if recovered := recover(); recovered != nil {
				s.fail(fmt.Errorf("scheduler: async task panic: %v", recovered))
			}
		}()
		task()
	}()
	return true
}

func callJob(ctx context.Context, handler func(context.Context) error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("scheduler: job panic: %v", recovered)
		}
	}()
	return handler(ctx)
}

func (s *Scheduler) fail(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	s.runErr = errors.Join(s.runErr, err)
	cancel := s.cancel
	failure := s.failure
	s.mu.Unlock()
	if failure != nil {
		select {
		case failure <- err:
		default:
		}
	}
	s.log(err.Error())
	if cancel != nil {
		cancel()
	}
}

// Err returns runtime failures that stopped the scheduler.
func (s *Scheduler) Err() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.runErr
}

// Completion reports a runtime failure that stops the scheduler.
func (s *Scheduler) Completion() <-chan error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.failure
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

const maxJobRecordSize = 1 << 20

// NewJobStore creates a JobStore that writes to the given directory.
func NewJobStore(dir string) (*JobStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("scheduler store: cannot create dir %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, fmt.Errorf("scheduler store: cannot secure dir %s: %w", dir, err)
	}
	return &JobStore{dir: dir}, nil
}

func (js *JobStore) Save(rec *JobRecord) error {
	js.mu.Lock()
	defer js.mu.Unlock()
	if rec == nil {
		return errors.New("scheduler store: record cannot be nil")
	}
	if err := validateJobName(rec.Name); err != nil {
		return err
	}

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("scheduler store: marshal: %w", err)
	}
	if len(data) > maxJobRecordSize {
		return errors.New("scheduler store: record exceeds size limit")
	}

	path := filepath.Join(js.dir, rec.Name+".json")
	tmp, err := os.CreateTemp(js.dir, ".job-*.tmp")
	if err != nil {
		return fmt.Errorf("scheduler store: create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return fmt.Errorf("scheduler store: secure temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("scheduler store: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("scheduler store: close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("scheduler store: rename: %w", err)
	}
	return nil
}

func (js *JobStore) Load(name string) (*JobRecord, error) {
	js.mu.RLock()
	defer js.mu.RUnlock()

	if err := validateJobName(name); err != nil {
		return nil, err
	}
	data, err := readJobFile(js.dir, name+".json")
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
		if filepath.Ext(entry.Name()) != ".json" || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		data, err := readJobFile(js.dir, entry.Name())
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

func validateJobName(name string) error {
	if name == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("scheduler store: invalid job name %q", name)
	}
	for _, character := range name {
		if character == 0 {
			return fmt.Errorf("scheduler store: invalid job name %q", name)
		}
	}
	return nil
}

func readJobFile(dir, name string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("scheduler store: refusing to read symbolic link")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxJobRecordSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxJobRecordSize {
		return nil, errors.New("scheduler store: record exceeds size limit")
	}
	return data, nil
}

func isDue(cronExpr string, now, last time.Time) bool {
	fields, err := parseCron(cronExpr)
	if err != nil {
		return false
	}
	if last.IsZero() {
		return cronMatches(fields, now)
	}
	next, ok := nextRun(fields, last)
	return ok && !next.After(now)
}

type cronFields struct {
	minute, hour, dom, month, dow                  int
	wildMin, wildHour, wildDom, wildMonth, wildDow bool
}

func parseCron(expr string) (cronFields, error) {
	parts := splitFields(expr)
	if len(parts) != 5 {
		return cronFields{}, fmt.Errorf("scheduler: invalid cron expression: %s", expr)
	}
	f := cronFields{}
	var err error
	if f.minute, f.wildMin, err = parseField(parts[0], 0, 59); err != nil {
		return cronFields{}, err
	}
	if f.hour, f.wildHour, err = parseField(parts[1], 0, 23); err != nil {
		return cronFields{}, err
	}
	if f.dom, f.wildDom, err = parseField(parts[2], 1, 31); err != nil {
		return cronFields{}, err
	}
	if f.month, f.wildMonth, err = parseField(parts[3], 1, 12); err != nil {
		return cronFields{}, err
	}
	if f.dow, f.wildDow, err = parseField(parts[4], 0, 6); err != nil {
		return cronFields{}, err
	}
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

func parseField(s string, min, max int) (int, bool, error) {
	if s == "*" {
		return 0, true, nil
	}
	value, err := strconv.Atoi(s)
	if err != nil || value < min || value > max {
		return 0, false, fmt.Errorf("scheduler: invalid cron field %q", s)
	}
	return value, false, nil
}

func nextRun(fields cronFields, last time.Time) (time.Time, bool) {
	next := last.Truncate(time.Minute).Add(time.Minute)
	const fiveYearsInMinutes = 5 * 366 * 24 * 60
	for i := 0; i < fiveYearsInMinutes; i++ {
		if cronMatches(fields, next) {
			return next, true
		}
		next = next.Add(time.Minute)
	}
	return time.Time{}, false
}

func cronMatches(fields cronFields, value time.Time) bool {
	return (fields.wildMin || value.Minute() == fields.minute) &&
		(fields.wildHour || value.Hour() == fields.hour) &&
		(fields.wildDom || value.Day() == fields.dom) &&
		(fields.wildMonth || int(value.Month()) == fields.month) &&
		(fields.wildDow || int(value.Weekday()) == fields.dow)
}
