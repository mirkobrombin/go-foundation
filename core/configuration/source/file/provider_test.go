package file

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProviderLoadFlattensJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"db":{"host":"localhost","port":5432},"debug":true}`), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	data, err := New(path).Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if data["db:host"] != "localhost" {
		t.Fatalf("db:host = %v, want localhost", data["db:host"])
	}
	if data["debug"] != true {
		t.Fatalf("debug = %v, want true", data["debug"])
	}
}

func TestProviderLoadMissingFile(t *testing.T) {
	_, err := New(filepath.Join(t.TempDir(), "missing.json")).Load(context.Background())
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func TestProviderLoadRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxFileSize + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := New(path).Load(context.Background()); err == nil {
		t.Fatal("Load() accepted an oversized file")
	}
}
