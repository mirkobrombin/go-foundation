package logger

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"
)

// Level represents log severity.
type Level int

// Log levels in ascending order of severity.
const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
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
	Level     string                 `json:"level"`
	Time      time.Time              `json:"time"`
	Msg       string                 `json:"msg"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
	TraceID   string                 `json:"trace_id,omitempty"`
	SpanID    string                 `json:"span_id,omitempty"`
}

type Sink interface {
	Log(e Entry) error
}

// Logger is the public logging contract.
type Logger interface {
	With(fields ...Field) Logger
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	RegisterSink(s Sink)
	SetLevel(l Level)
}

// Option configures the concrete logger on creation.
type Option func(*stdLogger)

type stdLogger struct {
	mu     sync.RWMutex
	sinks  []Sink
	level  Level
	fields map[string]interface{}
	ctx    context.Context
	async  bool
	ch     chan Entry
	wg     sync.WaitGroup
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
	}
	for _, o := range opts {
		o(l)
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

// WithAsync enables asynchronous logging with a buffered channel
// of the specified size.
func WithAsync(bufSize int) Option {
	return func(l *stdLogger) {
		l.async = true
		l.ch = make(chan Entry, bufSize)
		l.wg.Add(1)
		go l.processAsync()
	}
}

func (l *stdLogger) processAsync() {
	defer l.wg.Done()
	for e := range l.ch {
		l.mu.RLock()
		sinks := l.sinks
		l.mu.RUnlock()
		for _, sink := range sinks {
			_ = sink.Log(e)
		}
	}
}

// WithContext binds a context to the logger, allowing sinks
// to extract trace/span IDs for distributed tracing.
func WithContext(ctx context.Context) Option {
	return func(l *stdLogger) { l.ctx = ctx }
}

// RegisterSink adds a sink at runtime.
func RegisterSink(l Logger, s Sink) {
	if sl, ok := l.(*stdLogger); ok {
		sl.mu.Lock()
		defer sl.mu.Unlock()
		sl.sinks = append(sl.sinks, s)
	}
}

func (l *stdLogger) RegisterSink(s Sink) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sinks = append(l.sinks, s)
}

// SetLevel changes the log level at runtime.
func (l *stdLogger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// With returns a derived logger that shares sinks but has extra bound fields.
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
		sinks:  append([]Sink(nil), l.sinks...),
		level:  l.level,
		fields: nextFields,
		ctx:    l.ctx,
		async:  l.async,
		ch:     l.ch,
	}
	return child
}

// shouldLog reports whether the given level meets the logger's minimum threshold.
func (l *stdLogger) shouldLog(level Level) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return level >= l.level
}

// log constructs an Entry and dispatches it to all registered sinks.
func (l *stdLogger) log(level Level, msg string, fields ...Field) {
	if !l.shouldLog(level) {
		return
	}

	entry := Entry{
		Level: level.String(),
		Time:  time.Now().UTC(),
		Msg:   msg,
		Fields: map[string]interface{}{},
	}

	l.mu.RLock()
	for k, v := range l.fields {
		entry.Fields[k] = v
	}
	sinks := append([]Sink(nil), l.sinks...)
	ctx := l.ctx
	l.mu.RUnlock()

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

	if l.async {
		select {
		case l.ch <- entry:
		default:
		}
		return
	}

	for _, sink := range sinks {
		_ = sink.Log(entry)
	}
}

// Debug logs at debug level.
func (l *stdLogger) Debug(msg string, fields ...Field) { l.log(DebugLevel, msg, fields...) }
// Info logs at info level.
func (l *stdLogger) Info(msg string, fields ...Field)  { l.log(InfoLevel, msg, fields...) }
// Warn logs at warn level.
func (l *stdLogger) Warn(msg string, fields ...Field)  { l.log(WarnLevel, msg, fields...) }
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

