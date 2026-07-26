package doctor

import (
	"bytes"
	"strings"
	"testing"
)

func TestCheckSourceRoutesAndHealth(t *testing.T) {
	report := CheckSource(Source{Routes: []Route{
		{Method: "GET", Path: "/health/live"},
		{Method: "GET", Path: "/health/ready"},
	}})

	if report.Failures() != 0 {
		t.Fatalf("Failures() = %d, want 0", report.Failures())
	}
	if report.Warnings() != 0 {
		t.Fatalf("Warnings() = %d, want 0", report.Warnings())
	}
}

func TestCheckSourceFailsWithoutRoutes(t *testing.T) {
	report := CheckSource(Source{})

	if report.Failures() != 1 {
		t.Fatalf("Failures() = %d, want 1", report.Failures())
	}
}

func TestRunWithEnvPrintsText(t *testing.T) {
	var buf bytes.Buffer

	err := RunWithEnv(Source{Routes: []Route{{Method: "GET", Path: "/ping"}}}, "print", &buf)
	if err != nil {
		t.Fatalf("RunWithEnv() error = %v", err)
	}
	if !strings.Contains(buf.String(), "foundation doctor") {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestRunWithEnvFailsOnFailures(t *testing.T) {
	var buf bytes.Buffer

	err := RunWithEnv(Source{}, "fail", &buf)
	if err == nil {
		t.Fatal("RunWithEnv() error = nil, want error")
	}
}

func TestRunWithEnvWritesJSON(t *testing.T) {
	var buf bytes.Buffer

	err := RunWithEnv(Source{Routes: []Route{{Method: "GET", Path: "/ping"}}}, "json", &buf)
	if err != nil {
		t.Fatalf("RunWithEnv() error = %v", err)
	}
	if !strings.Contains(buf.String(), `"checks"`) {
		t.Fatalf("output = %q", buf.String())
	}
}
