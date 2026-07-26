package logger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mirkobrombin/go-foundation/v2/core/logger"
)

func TestConsoleSinkJSON(t *testing.T) {
	buf := &bytes.Buffer{}
	sink := logger.NewConsoleSink(buf)
	lg := logger.New(logger.WithSink(sink), logger.WithLevel(logger.DebugLevel))
	lg.Info("hello", logger.Field{Key: "k", Value: "v"})

	line, err := buf.ReadString('\n')
	if err != nil {
		t.Fatalf("ReadString() error = %v", err)
	}

	var entry logger.Entry
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &entry); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if entry.Msg != "hello" {
		t.Fatalf("entry.Msg = %q, want %q", entry.Msg, "hello")
	}
	if entry.Level != "info" {
		t.Fatalf("entry.Level = %q, want %q", entry.Level, "info")
	}
	if got, ok := entry.Fields["k"]; !ok || got != "v" {
		t.Fatalf("entry.Fields = %v, want key k=v", entry.Fields)
	}
}

type collectingSink struct {
	mu      sync.Mutex
	entries []logger.Entry
}

func (s *collectingSink) Log(entry logger.Entry) error {
	s.mu.Lock()
	s.entries = append(s.entries, entry)
	s.mu.Unlock()
	return nil
}

func TestAsyncLoggerCloseFlushesAllEntries(t *testing.T) {
	sink := &collectingSink{}
	lg := logger.New(
		logger.WithoutDefaultSink(),
		logger.WithSink(sink),
		logger.WithAsync(100),
	)
	for i := 0; i < 100; i++ {
		lg.Info("entry")
	}
	if err := lg.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := lg.Close(); err != nil {
		t.Fatal(err)
	}
	sink.mu.Lock()
	count := len(sink.entries)
	sink.mu.Unlock()
	if count != 100 {
		t.Fatalf("flushed entries = %d, want 100", count)
	}
}

type reentrantSink struct {
	once   sync.Once
	log    logger.Logger
	nested chan struct{}
}

func (s *reentrantSink) Log(logger.Entry) error {
	s.once.Do(func() {
		s.log.Info("nested")
		close(s.nested)
	})
	return nil
}

func TestAsyncLoggerAllowsReentrantLogging(t *testing.T) {
	sink := &reentrantSink{nested: make(chan struct{})}
	collector := &collectingSink{}
	log := logger.New(
		logger.WithoutDefaultSink(),
		logger.WithSink(sink),
		logger.WithSink(collector),
		logger.WithAsync(1),
	)
	sink.log = log
	log.Info("outer")

	select {
	case <-sink.nested:
	case <-time.After(time.Second):
		t.Fatal("reentrant log call deadlocked")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := log.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	collector.mu.Lock()
	count := len(collector.entries)
	collector.mu.Unlock()
	if count != 2 {
		t.Fatalf("collected entries = %d, want 2", count)
	}
}

type closingSink struct {
	log    logger.Logger
	called chan struct{}
}

func (s *closingSink) Log(logger.Entry) error {
	s.log.CloseAsync()
	close(s.called)
	return nil
}

func TestAsyncLoggerAllowsCloseAsyncFromSink(t *testing.T) {
	sink := &closingSink{called: make(chan struct{})}
	log := logger.New(
		logger.WithoutDefaultSink(),
		logger.WithSink(sink),
		logger.WithAsync(1),
	)
	sink.log = log
	log.Info("close")

	select {
	case <-sink.called:
	case <-time.After(time.Second):
		t.Fatal("sink Close deadlocked")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := log.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestDerivedLoggerDoesNotOwnParentLifecycle(t *testing.T) {
	collector := &collectingSink{}
	parent := logger.New(
		logger.WithoutDefaultSink(),
		logger.WithSink(collector),
		logger.WithAsync(2),
	)
	child := parent.With(logger.Field{Key: "child", Value: true})
	if err := child.Close(); err != nil {
		t.Fatalf("child Close: %v", err)
	}
	child.CloseAsync()
	if err := child.Shutdown(context.Background()); err != nil {
		t.Fatalf("child Shutdown: %v", err)
	}

	parent.Info("still open")
	if err := parent.Close(); err != nil {
		t.Fatalf("parent Close: %v", err)
	}
	collector.mu.Lock()
	count := len(collector.entries)
	collector.mu.Unlock()
	if count != 1 {
		t.Fatalf("collected entries = %d, want 1", count)
	}
}

func TestWithAsyncRejectsInvalidConfiguration(t *testing.T) {
	for name, options := range map[string][]logger.Option{
		"zero buffer": {logger.WithAsync(0)},
		"duplicate":   {logger.WithAsync(1), logger.WithAsync(2)},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("New() accepted invalid async configuration")
				}
			}()
			logger.New(options...)
		})
	}
}

type blockingSink struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingSink) Log(logger.Entry) error {
	s.once.Do(func() {
		close(s.started)
		<-s.release
	})
	return nil
}

func TestAsyncLoggerRemainsBoundedWhileSinkIsBlocked(t *testing.T) {
	sink := &blockingSink{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	log := logger.New(
		logger.WithoutDefaultSink(),
		logger.WithSink(sink),
		logger.WithAsync(1),
	)
	log.Info("first")
	select {
	case <-sink.started:
	case <-time.After(time.Second):
		t.Fatal("sink did not start")
	}

	done := make(chan struct{})
	go func() {
		for index := 0; index < 1000; index++ {
			log.Info("queued")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("logging blocked while the async queue was full")
	}

	close(sink.release)
	if err := log.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestLevelFiltering(t *testing.T) {
	buf := &bytes.Buffer{}
	sink := logger.NewConsoleSink(buf)
	lg := logger.New(logger.WithSink(sink), logger.WithLevel(logger.WarnLevel))
	lg.Info("should be filtered")
	if buf.Len() != 0 {
		t.Fatalf("buffer length = %d, want 0", buf.Len())
	}

	lg.Error("should appear")
	if buf.Len() == 0 {
		t.Fatalf("buffer length = 0, want > 0")
	}
}

func TestWithBindsContextFields(t *testing.T) {
	buf := &bytes.Buffer{}
	sink := logger.NewConsoleSink(buf)
	lg := logger.New(logger.WithSink(sink), logger.WithFields(logger.Field{Key: "service", Value: "api"}))

	requestLogger := lg.With(logger.Field{Key: "request_id", Value: "abc"})
	requestLogger.Info("serving")

	line, err := buf.ReadString('\n')
	if err != nil {
		t.Fatalf("ReadString() error = %v", err)
	}

	var entry logger.Entry
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &entry); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if entry.Fields["service"] != "api" {
		t.Fatalf("service field = %v, want api", entry.Fields["service"])
	}
	if entry.Fields["request_id"] != "abc" {
		t.Fatalf("request_id field = %v, want abc", entry.Fields["request_id"])
	}
}

func TestDerivedLoggerSharesRuntimeConfiguration(t *testing.T) {
	parentSink := &collectingSink{}
	lateSink := &collectingSink{}
	parent := logger.New(
		logger.WithoutDefaultSink(),
		logger.WithSink(parentSink),
		logger.WithLevel(logger.ErrorLevel),
	)
	child := parent.With(logger.Field{Key: "child", Value: true})
	parent.RegisterSink(lateSink)
	parent.SetLevel(logger.InfoLevel)

	child.Info("shared")

	for name, sink := range map[string]*collectingSink{
		"parent": parentSink,
		"late":   lateSink,
	} {
		sink.mu.Lock()
		count := len(sink.entries)
		sink.mu.Unlock()
		if count != 1 {
			t.Fatalf("%s sink entries = %d, want 1", name, count)
		}
	}
}

type panicSink struct{}

func (*panicSink) Log(logger.Entry) error {
	panic("sink failed")
}

func TestAsyncLoggerSurvivesSinkPanic(t *testing.T) {
	sink := &collectingSink{}
	log := logger.New(
		logger.WithoutDefaultSink(),
		logger.WithSink(&panicSink{}),
		logger.WithSink(sink),
		logger.WithAsync(2),
	)
	log.Info("first")
	log.Info("second")
	if err := log.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	sink.mu.Lock()
	count := len(sink.entries)
	sink.mu.Unlock()
	if count != 2 {
		t.Fatalf("entries after sink panic = %d, want 2", count)
	}
}
