package logger_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mirkobrombin/go-foundation/pkg/logger"
)

func TestCLEFSinkInformationOmitsLevel(t *testing.T) {
	buf := &bytes.Buffer{}
	sink := logger.NewCLEFSink(buf)

	err := sink.Log(logger.Entry{
		Level:  "info",
		Time:   time.Now().UTC(),
		Msg:    "service started",
		Fields: map[string]interface{}{"port": 8080},
	})
	if err != nil {
		t.Fatalf("Log() error = %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &m); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if _, ok := m["@l"]; ok {
		t.Fatalf("@l should be omitted for Information level, got %v", m["@l"])
	}
	if m["@m"] != "service started" {
		t.Fatalf("@m = %v, want %q", m["@m"], "service started")
	}
	if m["port"] == nil {
		t.Fatalf("structured field port missing from root level")
	}
}

func TestCLEFSinkLevelMapping(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"debug", "Debug"},
		{"warn", "Warning"},
		{"error", "Error"},
	}

	for _, tc := range cases {
		buf := &bytes.Buffer{}
		sink := logger.NewCLEFSink(buf)

		_ = sink.Log(logger.Entry{Level: tc.in, Time: time.Now().UTC(), Msg: "msg"})

		var m map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &m); err != nil {
			t.Fatalf("level=%q: json.Unmarshal() error = %v", tc.in, err)
		}
		if m["@l"] != tc.want {
			t.Fatalf("level=%q: @l = %v, want %q", tc.in, m["@l"], tc.want)
		}
	}
}

func TestCLEFSinkFieldsFlattened(t *testing.T) {
	buf := &bytes.Buffer{}
	sink := logger.NewCLEFSink(buf)

	_ = sink.Log(logger.Entry{
		Level: "info",
		Time:  time.Now().UTC(),
		Msg:   "request handled",
		Fields: map[string]interface{}{
			"request_id": "abc-123",
			"status":     200,
		},
	})

	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &m); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Fields must be at root, not nested inside a "fields" key.
	if _, nested := m["fields"]; nested {
		t.Fatalf("fields should be flat at root, not nested under 'fields'")
	}
	if m["request_id"] != "abc-123" {
		t.Fatalf("request_id = %v, want %q", m["request_id"], "abc-123")
	}
}

func TestCLEFSinkTimestampFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	sink := logger.NewCLEFSink(buf)
	ts := time.Date(2026, 3, 13, 9, 0, 0, 0, time.UTC)

	_ = sink.Log(logger.Entry{Level: "info", Time: ts, Msg: "ts check"})

	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &m); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	got, ok := m["@t"].(string)
	if !ok {
		t.Fatalf("@t is not a string: %v", m["@t"])
	}
	if !strings.HasPrefix(got, "2026-03-13T09:00:00") {
		t.Fatalf("@t = %q, want RFC3339Nano starting with 2026-03-13T09:00:00", got)
	}
}

func TestCLEFSinkViaLogger(t *testing.T) {
	buf := &bytes.Buffer{}
	// Default logger has ConsoleSink; replace with CLEFSink only.
	lg := logger.New(logger.WithSink(logger.NewCLEFSink(buf)))
	lg.Warn("disk space low", logger.Field{Key: "free_gb", Value: 2})

	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &m); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if m["@l"] != "Warning" {
		t.Fatalf("@l = %v, want Warning", m["@l"])
	}
	if m["@m"] != "disk space low" {
		t.Fatalf("@m = %v, want 'disk space low'", m["@m"])
	}
	if m["free_gb"] == nil {
		t.Fatalf("free_gb missing from CLEF output")
	}
}
