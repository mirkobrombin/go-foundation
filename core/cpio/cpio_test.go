package cpio

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestPackAndReadRoundtrip(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "bin", "hello.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := PackDir(tmp, &buf, WithMTimeUnix(0)); err != nil {
		t.Fatal(err)
	}

	rd := NewReader(bytes.NewReader(buf.Bytes()))
	seen := map[string][]byte{}
	for {
		e, err := rd.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
		seen[e.Name] = e.Data
	}

	if got := string(seen["bin/hello.txt"]); got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestUnpackToDir(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "etc"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "etc", "conf"), []byte("x=y"), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := PackDir(tmp, &buf, WithMTimeUnix(0)); err != nil {
		t.Fatal(err)
	}

	out := t.TempDir()
	if err := UnpackToDir(bytes.NewReader(buf.Bytes()), out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "etc", "conf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "x=y" {
		t.Fatalf("got %q", string(b))
	}
}

func TestReaderEnforcesConfiguredLimits(t *testing.T) {
	var archive bytes.Buffer
	writer := NewWriter(&archive)
	if err := writer.AddFile("large", 0644, []byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	reader := NewReader(bytes.NewReader(archive.Bytes()), WithReaderLimits(32, 4, 4))
	if _, err := reader.Next(); err == nil {
		t.Fatal("Reader accepted an entry larger than the configured limit")
	}
}

func TestReaderEnforcesTotalLimit(t *testing.T) {
	var archive bytes.Buffer
	writer := NewWriter(&archive)
	if err := writer.AddFile("one", 0644, []byte("1234")); err != nil {
		t.Fatal(err)
	}
	if err := writer.AddFile("two", 0644, []byte("5678")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	reader := NewReader(bytes.NewReader(archive.Bytes()), WithReaderLimits(32, 8, 6))
	if _, err := reader.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Next(); err == nil {
		t.Fatal("Reader accepted archive data beyond the total limit")
	}
}

func TestReaderEnforcesEntryCountLimit(t *testing.T) {
	var archive bytes.Buffer
	writer := NewWriter(&archive)
	if err := writer.AddFile("one", 0644, nil); err != nil {
		t.Fatal(err)
	}
	if err := writer.AddFile("two", 0644, nil); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	reader := NewReader(bytes.NewReader(archive.Bytes()), WithReaderMaxEntries(1))
	if _, err := reader.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Next(); err == nil {
		t.Fatal("Reader accepted too many empty entries")
	}
}

func TestWriterEnforcesConfiguredLimits(t *testing.T) {
	t.Run("file size", func(t *testing.T) {
		writer := NewWriter(io.Discard, WithWriterLimits(2, 10, 10))
		if err := writer.AddFile("large", 0644, []byte("123")); err == nil {
			t.Fatal("Writer accepted a file beyond its entry limit")
		}
	})

	t.Run("total size", func(t *testing.T) {
		writer := NewWriter(io.Discard, WithWriterLimits(4, 5, 10))
		if err := writer.AddFile("one", 0644, []byte("123")); err != nil {
			t.Fatal(err)
		}
		if err := writer.AddFile("two", 0644, []byte("456")); err == nil {
			t.Fatal("Writer accepted data beyond its total limit")
		}
	})

	t.Run("entry count", func(t *testing.T) {
		writer := NewWriter(io.Discard, WithWriterLimits(4, 10, 1))
		if err := writer.AddDir("one", 0755); err != nil {
			t.Fatal(err)
		}
		if err := writer.AddFile("two", 0644, nil); err == nil {
			t.Fatal("Writer accepted too many entries")
		}
	})
}

func TestUnpackRejectsSymlinkEscape(t *testing.T) {
	out := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(out, "link")); err != nil {
		t.Fatal(err)
	}

	var archive bytes.Buffer
	writer := NewWriter(&archive)
	if err := writer.AddFile("link/escaped", 0644, []byte("owned")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := UnpackToDir(bytes.NewReader(archive.Bytes()), out); err == nil {
		t.Fatal("UnpackToDir() followed a symlink outside the destination")
	}
	if _, err := os.Stat(filepath.Join(outside, "escaped")); !os.IsNotExist(err) {
		t.Fatalf("outside file exists: %v", err)
	}
}

func TestPackDirRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "secret")); err != nil {
		t.Fatal(err)
	}

	var archive bytes.Buffer
	if err := PackDir(root, &archive); err == nil {
		t.Fatal("PackDir() followed a symbolic link")
	}
}

type rejectingWriter struct{}

func (rejectingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestPackDirReturnsTrailerWriteError(t *testing.T) {
	if err := PackDir(t.TempDir(), rejectingWriter{}); err == nil {
		t.Fatal("PackDir() ignored trailer write failure")
	}
}

func TestReaderRejectsArchiveWithoutTrailer(t *testing.T) {
	var archive bytes.Buffer
	writer := NewWriter(&archive)
	if err := writer.AddFile("file", 0644, []byte("data")); err != nil {
		t.Fatal(err)
	}

	reader := NewReader(bytes.NewReader(archive.Bytes()))
	if _, err := reader.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Next(); err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("Next() error = %v, want missing trailer error", err)
	}
}

func TestReaderRejectsCRCArchiveWithoutChecksumValidation(t *testing.T) {
	var archive bytes.Buffer
	writer := NewWriter(&archive)
	if err := writer.AddFile("file", 0644, []byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data := archive.Bytes()
	copy(data[:6], "070702")

	if _, err := NewReader(bytes.NewReader(data)).Next(); err == nil {
		t.Fatal("Reader accepted an unsupported CRC archive")
	}
}
