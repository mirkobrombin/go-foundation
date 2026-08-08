package dir

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFlattensEachFileUnderItsName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.example.json", `{"domain":"a.example","port":8080,"ssl":{"enabled":true}}`)
	writeFile(t, dir, "b.example.json", `{"domain":"b.example","port":9090}`)

	p := New(dir, "*.json")
	got, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]any{
		"a.example:domain":      "a.example",
		"a.example:port":        float64(8080),
		"a.example:ssl:enabled": true,
		"b.example:domain":      "b.example",
		"b.example:port":        float64(9090),
	}
	if len(got) != len(want) {
		t.Fatalf("got %d keys, want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %q: got %v, want %v", k, got[k], v)
		}
	}
}

func TestLoadHonorsPatternAndExclude(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "alpha.json", `{"a":1}`)
	writeFile(t, dir, "conf.global.json", `{"b":2}`)
	writeFile(t, dir, "notes.txt", `{"c":3}`)
	sub := filepath.Join(dir, "nested")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, sub, "inner.json", `{"d":4}`)

	p := New(dir, "*.json", "conf.global.json")
	got, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["alpha:a"]; !ok {
		t.Error("alpha.json not loaded")
	}
	if _, ok := got["conf.global:b"]; ok {
		t.Error("excluded file was loaded")
	}
	if _, ok := got["notes:c"]; ok {
		t.Error("non-matching pattern was loaded")
	}
	if _, ok := got["inner:d"]; ok {
		t.Error("subdirectory file was loaded")
	}
}

func TestLoadReportsBadJSONWithFileName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "broken.json", `{invalid`)

	p := New(dir, "*.json")
	_, err := p.Load(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "broken.json") {
		t.Errorf("error %q does not name the file", got)
	}
}

func TestLoadMissingDirectory(t *testing.T) {
	p := New(filepath.Join(t.TempDir(), "nope"), "*.json")
	if _, err := p.Load(context.Background()); err == nil {
		t.Fatal("expected error for missing directory")
	}
}

func TestLoadCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := New(t.TempDir(), "*.json")
	if _, err := p.Load(ctx); err == nil {
		t.Fatal("expected context error")
	}
}
