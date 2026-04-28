package logger_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mirkobrombin/go-foundation/pkg/logger"
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
