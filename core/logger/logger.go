package logger

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

// Level represents log severity.
type Level int

// Log levels in ascending order of severity.
const (
	// DebugLevel logs detailed diagnostic information.
	DebugLevel Level = iota
	// InfoLevel logs general operational messages.
	InfoLevel
	// WarnLevel logs potential problems.
	WarnLevel
	// ErrorLevel logs error conditions.
	ErrorLevel
	// FatalLevel logs severe errors that should terminate the process.
	FatalLevel
)

// String returns the textual representation of the log level.
func (l Level) String() string {
	switch l {
	case DebugLevel:
		return "debug"
	case InfoLevel:
		return "info"
	case WarnLevel:
		return "warn"
	case ErrorLevel:
		return "error"
	case FatalLevel:
		return "fatal"
	default:
		return "unknown"
	}
}

// Field is a single structured key/value pair.
type Field struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

// Entry is the log payload passed to sinks.
type Entry struct {
	Level   string                 `json:"level"`
	Time    time.Time              `json:"time"`
	Msg     string                 `json:"msg"`
	Fields  map[string]interface{} `json:"fields,omitempty"`
	TraceID string                 `json:"trace_id,omitempty"`
	SpanID  string                 `json:"span_id,omitempty"`
}

// Sink receives log entries for processing.
// A sink that initiates logger shutdown must use Logger.CloseAsync.
type Sink interface {
	Log(e Entry) error
}

// Logger is the public logging contract.
type Logger interface {
	io.Closer
	With(fields ...Field) Logger
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	RegisterSink(s Sink)
	SetLevel(l Level)
	CloseAsync()
	Shutdown(ctx context.Context) error
}

// Option configures the concrete logger on creation.
type Option func(*stdLogger)

type stdLogger struct {
	mu     sync.RWMutex
	sinks  []Sink
	level  Level
	shared *loggerState
	fields map[string]interface{}
	ctx    context.Context
	async  *asyncState
	owner  bool

	asyncConfigured bool
	asyncBuffer     int
}

type loggerState struct {
	mu    sync.RWMutex
	sinks []Sink
	level Level
}

type asyncItem struct {
	entry Entry
	sinks []Sink
}

type asyncState struct {
	mu        sync.Mutex
	ch        chan asyncItem
	done      chan struct{}
	workerWG  sync.WaitGroup
	submitWG  sync.WaitGroup
	closeOnce sync.Once
	closed    bool
}

// New constructs a logger with optional options.
//
// Example:
//
//	log := logger.New(
//		logger.WithLevel(logger.DebugLevel),
//		logger.WithoutDefaultSink(),
//		logger.WithSink(mySink),
//	)
//	log.Info("started", logger.Field{Key: "version", Value: "1.0"})
func New(opts ...Option) Logger {
	l := &stdLogger{
		level:  InfoLevel,
		fields: map[string]interface{}{},
		sinks:  []Sink{NewConsoleSink(nil)},
		owner:  true,
	}
	for _, o := range opts {
		o(l)
	}
	l.shared = &loggerState{
		sinks: append([]Sink(nil), l.sinks...),
		level: l.level,
	}
	if l.asyncConfigured {
		state := &asyncState{
			ch:   make(chan asyncItem, l.asyncBuffer),
			done: make(chan struct{}),
		}
		state.workerWG.Add(1)
		l.async = state
		go state.process()
	}
	return l
}

// WithLevel sets the minimum level for emitted logs.
func WithLevel(level Level) Option {
	return func(l *stdLogger) { l.level = level }
}

// WithSink adds an initial sink.
func WithSink(s Sink) Option {
	return func(l *stdLogger) { l.sinks = append(l.sinks, s) }
}

// WithoutDefaultSink removes the default ConsoleSink added by New.
// Use before WithSink to create a logger with only custom sinks:
//
//	logger.New(logger.WithoutDefaultSink(), logger.WithSink(clefSink))
func WithoutDefaultSink() Option {
	return func(l *stdLogger) { l.sinks = nil }
}

// WithFields binds fields to the logger returned from New.
func WithFields(fields ...Field) Option {
	return func(l *stdLogger) {
		for _, f := range fields {
			l.fields[f.Key] = f.Value
		}
	}
}

// WithAsync enables asynchronous logging with a bounded queue.
// Entries are dropped when the queue is full.
func WithAsync(bufSize int) Option {
	return func(l *stdLogger) {
		if bufSize <= 0 {
			panic("logger: async buffer size must be positive")
		}
		if l.asyncConfigured {
			panic("logger: async logging is already configured")
		}
		l.asyncConfigured = true
		l.asyncBuffer = bufSize
	}
}

func (s *asyncState) process() {
	defer s.workerWG.Done()
	for item := range s.ch {
		for _, sink := range item.sinks {
			_ = safeSinkLog(sink, item.entry)
		}
	}
}

func safeSinkLog(sink Sink, entry Entry) (err error) {
	if sink == nil {
		return nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errors.New("logger: sink panic")
		}
	}()
	return sink.Log(entry)
}

// WithContext binds a context to the logger, allowing sinks
// to extract trace/span IDs for distributed tracing.
func WithContext(ctx context.Context) Option {
	return func(l *stdLogger) { l.ctx = ctx }
}

// RegisterSink adds a sink at runtime.
func RegisterSink(l Logger, s Sink) {
	if sl, ok := l.(*stdLogger); ok {
		sl.RegisterSink(s)
	}
}

func (l *stdLogger) RegisterSink(s Sink) {
	l.shared.mu.Lock()
	defer l.shared.mu.Unlock()
	l.shared.sinks = append(l.shared.sinks, s)
}

// SetLevel changes the log level at runtime.
func (l *stdLogger) SetLevel(level Level) {
	l.shared.mu.Lock()
	defer l.shared.mu.Unlock()
	l.shared.level = level
}

// With returns a non-owning derived logger that shares sinks but has extra bound fields.
// Closing a derived logger does not close its parent.
func (l *stdLogger) With(fields ...Field) Logger {
	l.mu.RLock()
	defer l.mu.RUnlock()

	nextFields := make(map[string]interface{}, len(l.fields)+len(fields))
	for k, v := range l.fields {
		nextFields[k] = v
	}
	for _, f := range fields {
		nextFields[f.Key] = f.Value
	}

	child := &stdLogger{
		fields: nextFields,
		ctx:    l.ctx,
		async:  l.async,
		shared: l.shared,
		owner:  false,
	}
	return child
}

// shouldLog reports whether the given level meets the logger's minimum threshold.
func (l *stdLogger) shouldLog(level Level) bool {
	l.shared.mu.RLock()
	defer l.shared.mu.RUnlock()
	return level >= l.shared.level
}

// log constructs an Entry and dispatches it to all registered sinks.
func (l *stdLogger) log(level Level, msg string, fields ...Field) {
	if !l.shouldLog(level) {
		return
	}

	entry := Entry{
		Level:  level.String(),
		Time:   time.Now().UTC(),
		Msg:    msg,
		Fields: map[string]interface{}{},
	}

	l.mu.RLock()
	for k, v := range l.fields {
		entry.Fields[k] = v
	}
	ctx := l.ctx
	l.mu.RUnlock()
	l.shared.mu.RLock()
	sinks := append([]Sink(nil), l.shared.sinks...)
	l.shared.mu.RUnlock()

	if ctx != nil {
		if tid, ok := ctx.Value("trace_id").(string); ok {
			entry.TraceID = tid
		}
		if sid, ok := ctx.Value("span_id").(string); ok {
			entry.SpanID = sid
		}
	}

	for _, f := range fields {
		entry.Fields[f.Key] = f.Value
	}

	if l.async != nil {
		l.async.submit(asyncItem{entry: entry, sinks: sinks})
		return
	}

	for _, sink := range sinks {
		_ = safeSinkLog(sink, entry)
	}
}

func (s *asyncState) submit(item asyncItem) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.submitWG.Add(1)
	s.mu.Unlock()

	send := func() {
		defer s.submitWG.Done()
		select {
		case s.ch <- item:
		default:
		}
	}
	send()
}

// Close flushes pending asynchronous entries and stops the logger worker.
func (l *stdLogger) Close() error {
	if l.async == nil || !l.owner {
		return nil
	}
	l.async.startClose()
	<-l.async.done
	return nil
}

// CloseAsync starts logger shutdown without waiting for sink callbacks.
// It is safe to call from a Sink.
func (l *stdLogger) CloseAsync() {
	if l.async == nil || !l.owner {
		return
	}
	l.async.startClose()
}

func (s *asyncState) startClose() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()

		go func() {
			s.submitWG.Wait()
			close(s.ch)
			s.workerWG.Wait()
			close(s.done)
		}()
	})
}

// Shutdown flushes pending asynchronous entries or returns when ctx is canceled.
// Sink callbacks must use CloseAsync instead.
func (l *stdLogger) Shutdown(ctx context.Context) error {
	if l.async == nil || !l.owner {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	l.async.startClose()
	select {
	case <-l.async.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Debug logs at debug level.
func (l *stdLogger) Debug(msg string, fields ...Field) { l.log(DebugLevel, msg, fields...) }

// Info logs at info level.
func (l *stdLogger) Info(msg string, fields ...Field) { l.log(InfoLevel, msg, fields...) }

// Warn logs at warn level.
func (l *stdLogger) Warn(msg string, fields ...Field) { l.log(WarnLevel, msg, fields...) }

// Error logs at error level.
func (l *stdLogger) Error(msg string, fields ...Field) { l.log(ErrorLevel, msg, fields...) }

// ConsoleSink writes entries as compact JSON lines to an io.Writer.
type ConsoleSink struct {
	w io.Writer
}

// NewConsoleSink constructs a ConsoleSink.
func NewConsoleSink(w io.Writer) *ConsoleSink {
	if w == nil {
		w = os.Stdout
	}
	return &ConsoleSink{w: w}
}

// Log writes a structured entry to the underlying writer.
func (c *ConsoleSink) Log(e Entry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = c.w.Write(append(b, '\n'))
	return err
}
